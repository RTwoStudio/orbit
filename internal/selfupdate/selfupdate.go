// Package selfupdate replaces the running orbit binary with the latest GitHub
// release. It uses the POSIX rename(2) swap: download → verify → extract to a
// temp file on the SAME filesystem as the current binary → atomic rename over
// it. A running executable can be renamed over on Linux/macOS because the old
// inode stays alive until the process exits; the next invocation is the new
// binary. Windows is refused (a running .exe cannot be replaced in place).
package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RTwoStudio/orbit/internal/exit"
)

// Config holds the release source and the binary to replace.
type Config struct {
	Repo         string // "owner/repo"
	APIBase      string // default https://api.github.com
	DownloadBase string // default https://github.com
	BinPath      string // absolute path of the binary to replace
	Current      string // current compiled version ("0.2.0")
	Token        string // optional GitHub token (private repo / rate limits)
}

// DefaultConfig resolves source + binary path from env, mirroring install.sh.
func DefaultConfig(current string) (*Config, error) {
	bin, err := BinaryPath()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Repo:         envOr("ORBIT_GH_REPO", "RTwoStudio/orbit"),
		APIBase:      envOr("ORBIT_GH_API", "https://api.github.com"),
		DownloadBase: envOr("ORBIT_GH_DL", "https://github.com"),
		BinPath:      bin,
		Current:      current,
		Token:        os.Getenv("ORBIT_GITHUB_TOKEN"),
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// BinaryPath returns the absolute, symlink-resolved path of the running
// binary. ORBIT_BIN_DIR overrides the directory (used by install.sh and tests).
func BinaryPath() (string, error) {
	if dir := os.Getenv("ORBIT_BIN_DIR"); dir != "" {
		return filepath.Join(dir, "orbit"), nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot resolve the running binary path")
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil // not a symlink or unresolvable — use the raw path
	}
	return resolved, nil
}

// Info is the result of a check/lookup.
type Info struct {
	Current  string `json:"current"`
	Latest   string `json:"latest"`
	UpToDate bool   `json:"up_to_date"`
	Newer    bool   `json:"current_is_newer"`
	Asset    string `json:"asset"`
	URL      string `json:"download_url"`
	BinPath  string `json:"binary_path"`
}

type release struct {
	TagName string `json:"tag_name"`
}

// Lookup resolves the latest release and computes the comparison.
func (c *Config) Lookup(client *http.Client) (*Info, error) {
	tag, err := c.latestTag(client)
	if err != nil {
		return nil, err
	}
	latest := strings.TrimPrefix(tag, "v")
	asset, err := AssetName(tag)
	if err != nil {
		return nil, err
	}
	info := &Info{
		Current: c.Current,
		Latest:  latest,
		Asset:   asset,
		URL:     fmt.Sprintf("%s/%s/releases/download/%s/%s", c.DownloadBase, c.Repo, tag, asset),
		BinPath: c.BinPath,
	}
	switch cmp := compareSemver(latest, c.Current); {
	case cmp == 0:
		info.UpToDate = true
	case cmp < 0:
		info.Newer = true
	}
	return info, nil
}

func (c *Config) latestTag(client *http.Client) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.APIBase, c.Repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", exit.New(exit.RegistryUnreachable, "invalid release API URL: "+url)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", exit.New(exit.RegistryUnreachable,
			fmt.Sprintf("cannot reach GitHub for %s: %v", c.Repo, err),
			"check your network")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", exit.New(exit.RegistryUnreachable,
			fmt.Sprintf("GitHub returned HTTP %d for %s", resp.StatusCode, url),
			"the repo may be private — set ORBIT_GITHUB_TOKEN, or the project has no releases yet")
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return "", exit.New(exit.RegistryUnreachable, "cannot parse the release response: "+err.Error())
	}
	if rel.TagName == "" {
		return "", exit.New(exit.RegistryUnreachable, "the latest release has no tag")
	}
	return rel.TagName, nil
}

// AssetName is the release asset for the current OS/arch (matches release.yml).
func AssetName(tag string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", exit.New(exit.StateConflict,
			"self-update is only supported on linux for now — reinstall with the installer script")
	}
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		return "", exit.New(exit.StateConflict,
			fmt.Sprintf("unsupported architecture %q (supported: amd64, arm64)", runtime.GOARCH))
	}
	return fmt.Sprintf("orbit_%s_%s_%s.tar.gz", tag, "linux", arch), nil
}

// Apply downloads, verifies, extracts, and atomically swaps the binary.
// Returns nil on a no-op (already current without --force). force reinstalls
// even when current and permits replacing a newer local build.
func (c *Config) Apply(client *http.Client, info *Info, force bool) error {
	if runtime.GOOS == "windows" {
		return exit.New(exit.StateConflict,
			"self-update cannot replace a running .exe on Windows",
			"reinstall using the installer script instead")
	}
	if info.UpToDate && !force {
		return nil
	}
	if info.Newer && !force {
		return exit.New(exit.StateConflict,
			fmt.Sprintf("installed %s is newer than latest release %s — refusing to downgrade", info.Current, info.Latest),
			"pass --force to replace it anyway")
	}

	dir := filepath.Dir(c.BinPath)
	if err := writableDir(dir); err != nil {
		return exit.New(exit.StateConflict,
			fmt.Sprintf("%s is not writable", dir),
			"reinstall to a writable dir, or re-run with sudo/ORBIT_BIN_DIR set")
	}

	tmp, err := downloadAsset(client, info.URL)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	if err := verifyChecksum(client, c, info, tmp); err != nil {
		return err
	}

	bin, err := extractBinary(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(bin)

	// Same-filesystem temp: rename(2) fails across filesystems (EXDEV).
	staged, err := os.CreateTemp(dir, ".orbit-update-*")
	if err != nil {
		return exit.Wrap(exit.IOError, err, "cannot create a staging file in "+dir)
	}
	stagedName := staged.Name()
	defer os.Remove(stagedName) // no-op after a successful swap

	src, err := os.Open(bin)
	if err != nil {
		staged.Close()
		return exit.Wrap(exit.IOError, err, "cannot open the downloaded binary")
	}
	defer src.Close()
	if _, err := io.Copy(staged, src); err != nil {
		staged.Close()
		return exit.Wrap(exit.IOError, err, "cannot stage the new binary")
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		return exit.Wrap(exit.IOError, err, "cannot flush the staged binary")
	}
	if err := staged.Close(); err != nil {
		return exit.Wrap(exit.IOError, err, "cannot close the staged binary")
	}
	if err := os.Chmod(stagedName, 0o755); err != nil {
		return exit.Wrap(exit.IOError, err, "cannot chmod the staged binary")
	}
	// The atomic swap. Legal on a running executable: the old inode survives
	// until this process exits.
	if err := os.Rename(stagedName, c.BinPath); err != nil {
		return exit.Wrap(exit.IOError, err, "cannot replace "+c.BinPath)
	}
	return nil
}

func writableDir(dir string) error {
	tmp, err := os.CreateTemp(dir, ".orbit-w-*")
	if err != nil {
		return err
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return nil
}

func downloadAsset(client *http.Client, url string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", exit.New(exit.RegistryUnreachable, fmt.Sprintf("download failed: %v", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", exit.New(exit.RegistryUnreachable,
			fmt.Sprintf("download failed: HTTP %d from %s", resp.StatusCode, url))
	}
	f, err := os.CreateTemp("", "orbit-asset-*.tar.gz")
	if err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot create a temp file")
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(f.Name())
		return "", exit.Wrap(exit.IOError, err, "cannot write the downloaded asset")
	}
	return f.Name(), nil
}

// verifyChecksum fetches checksums.txt and compares the already-downloaded
// asset against the listed entry. A missing checksums file (or a missing
// entry) is tolerated, matching install.sh; a present mismatch is fatal.
func verifyChecksum(client *http.Client, c *Config, info *Info, assetPath string) error {
	url := strings.TrimSuffix(info.URL, "/"+info.Asset) + "/checksums.txt"
	resp, err := client.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil
	}
	want := ""
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == info.Asset {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return nil
	}
	have, err := sha256Of(assetPath)
	if err != nil {
		return exit.Wrap(exit.IOError, err, "cannot hash the downloaded asset")
	}
	if !strings.EqualFold(have, want) {
		return exit.New(exit.StateConflict,
			fmt.Sprintf("checksum mismatch for %s", info.Asset),
			fmt.Sprintf("want %s, got %s — refusing to install a corrupted download", want, have))
	}
	return nil
}

// extractBinary pulls the orbit binary out of a tar.gz and returns its path.
func extractBinary(tarPath string) (string, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot open the asset")
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot read the asset archive")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", exit.Wrap(exit.IOError, err, "cannot read the asset archive")
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == "orbit" {
			out, err := os.CreateTemp("", "orbit-bin-*")
			if err != nil {
				return "", exit.Wrap(exit.IOError, err, "cannot create a temp file")
			}
			defer out.Close()
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, io.LimitReader(tr, 1<<30)); err != nil {
				os.Remove(out.Name())
				return "", exit.Wrap(exit.IOError, err, "cannot extract the binary")
			}
			if _, err := out.Write(buf.Bytes()); err != nil {
				os.Remove(out.Name())
				return "", exit.Wrap(exit.IOError, err, "cannot write the binary")
			}
			return out.Name(), nil
		}
	}
	return "", exit.New(exit.NotFound, "the release archive contains no 'orbit' binary")
}

// sha256Of is used by tests and the checksum path.
func sha256Of(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var _ = sha256Of

// compareSemver returns -1, 0, +1 for a vs b (numeric, dotted).
func compareSemver(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x := atoiSafe(as, i)
		y := atoiSafe(bs, i)
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func atoiSafe(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n := 0
	for _, r := range parts[i] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// DefaultClient is the HTTP client used by self-update (10-minute timeout for
// large assets).
func DefaultClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute}
}
