package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RTwoStudio/orbit-cli/internal/fsutil"
	"github.com/RTwoStudio/orbit-cli/internal/logx"
)

// CacheLoad loads the verified snapshot from the global cache. The cache
// holds manifest.json + stubs only (agents/commands deploy straight to the
// opencode dir and are never cached).
func CacheLoad() (*Fetched, error) {
	dir := cacheDir()
	f := CacheFetcher{Dir: dir}
	raw, err := f.Fetch("manifest.json")
	if err != nil {
		return nil, fmt.Errorf("cache empty — run: orbit neocortex update (or install)")
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("cache integrity: %w", err)
	}
	fc := &Fetched{Manifest: manifest, Content: map[string][]byte{"manifest.json": raw}}
	for _, e := range manifest.Files.Stubs {
		data, err := f.Fetch(e.Path)
		if err != nil {
			return nil, fmt.Errorf("cache incomplete (%s) — run: orbit neocortex update", e.Path)
		}
		if got := SHA256Hex(data); got != e.SHA256 {
			return nil, fmt.Errorf("cache integrity: sha256 mismatch for %s — run: orbit neocortex update", e.Path)
		}
		fc.Content[e.Path] = data
	}
	return fc, nil
}

// CacheFetcher fetches from the global cache dir directly (paths relative
// to neocortex/ == cache dir contents).
type CacheFetcher struct{ Dir string }

func (c CacheFetcher) Fetch(relPath string) ([]byte, error) {
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return nil, fmt.Errorf("refusing path outside cache: %s", relPath)
	}
	return os.ReadFile(filepath.Join(c.Dir, clean))
}

// CacheSave atomically writes stubs + manifest.json into the cache dir.
func CacheSave(fc *Fetched) error {
	dir := cacheDir()
	if err := fsutil.EnsureDir(dir); err != nil {
		return err
	}
	for _, e := range fc.Manifest.Files.Stubs {
		if err := fsutil.AtomicWrite(filepath.Join(dir, e.Path), fc.Content[e.Path], 0o644); err != nil {
			return err
		}
	}
	return fsutil.AtomicWrite(filepath.Join(dir, "manifest.json"), fc.Content["manifest.json"], 0o644)
}

// CacheManifestHash returns the sha256 of the cached manifest.json ("" if absent).
func CacheManifestHash() string {
	data, err := os.ReadFile(filepath.Join(cacheDir(), "manifest.json"))
	if err != nil {
		return ""
	}
	return SHA256Hex(data)
}

func cacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "orbit", "neocortex", "cache")
}

// RemoteFetcher fetches via the git binary (shallow clone), falling back
// to a GitHub tarball download.
type RemoteFetcher struct {
	URL      string // https git URL
	Ref      string
	TokenEnv string // name of env var holding the token (never the token itself)
}

// token returns the token value from the configured env var name.
func (r RemoteFetcher) token() string {
	if r.TokenEnv == "" {
		return ""
	}
	return os.Getenv(r.TokenEnv)
}

func (r RemoteFetcher) Fetch(relPath string) ([]byte, error) {
	// Fetch the whole tree once per process; lazy cache.
	if cachedErr != nil {
		return nil, cachedErr
	}
	if fetchedTree == nil {
		fc, err := r.fetchAll()
		if err != nil {
			cachedErr = err
			return nil, err
		}
		fetchedTree = fc
	}
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return nil, fmt.Errorf("refusing path outside neocortex/: %s", relPath)
	}
	data, ok := fetchedTree[clean]
	if !ok {
		return nil, fmt.Errorf("file not in registry: %s", relPath)
	}
	return data, nil
}

var (
	fetchedTree map[string][]byte
	cachedErr   error
)

func (r RemoteFetcher) fetchAll() (map[string][]byte, error) {
	if _, err := exec.LookPath("git"); err == nil {
		if tree, err := r.fetchGit(); err == nil {
			return tree, nil
		} else {
			logx.Debug("git fetch failed, falling back to tarball: %v", err)
		}
	}
	return r.fetchTarball()
}

func (r RemoteFetcher) fetchGit() (map[string][]byte, error) {
	tmp, err := os.MkdirTemp("", "orbit-registry-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	repo := filepath.Join(tmp, "repo")
	url := r.URL
	if tok := r.token(); tok != "" && strings.HasPrefix(url, "https://") {
		// Token never logged; embedded only in the transient command env.
		url = strings.Replace(url, "https://", "https://x-access-token:"+tok+"@", 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", r.Ref, "--single-branch", url, repo)
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/true")
	out, err := c.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git clone %s (ref %s) failed: %s", r.URL, r.Ref, strings.TrimSpace(string(out)))
	}
	neoRoot := filepath.Join(repo, "neocortex")
	tree := map[string][]byte{}
	err = filepath.Walk(neoRoot, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(neoRoot, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tree[filepath.ToSlash(rel)] = data
		return nil
	})
	return tree, err
}

func (r RemoteFetcher) fetchTarball() (map[string][]byte, error) {
	// https://github.com/<org>/<repo> → API tarball endpoint.
	apiURL := strings.TrimSuffix(r.URL, ".git")
	apiURL = strings.Replace(apiURL, "https://github.com/", "https://api.github.com/repos/", 1)
	apiURL = apiURL + "/tarball/" + r.Ref
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	if tok := r.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry unreachable at %s (ref %s): %w", r.URL, r.Ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry unreachable at %s (ref %s): HTTP %d — check url/ref/token", r.URL, r.Ref, resp.StatusCode)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	return extractTarGz(gz)
}

// extractTarGz extracts only the neocortex/ subtree of the tarball.
func extractTarGz(r io.Reader) (map[string][]byte, error) {
	tree := map[string][]byte{}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := filepath.ToSlash(hdr.Name)
		// Tarballs from GitHub wrap in <repo>-<ref>/.
		if i := strings.Index(name, "neocortex/"); i >= 0 && hdr.Typeflag == tar.TypeReg {
			rel := name[i+len("neocortex/"):]
			if rel == "" || strings.HasSuffix(rel, "/") {
				continue
			}
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, tr); err != nil {
				return nil, err
			}
			tree[rel] = buf.Bytes()
		}
	}
	if len(tree) == 0 {
		return nil, fmt.Errorf("tarball contains no neocortex/ folder")
	}
	return tree, nil
}
