package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RTwoStudio/orbit/internal/domain"
	"github.com/RTwoStudio/orbit/internal/fsutil"
	"github.com/RTwoStudio/orbit/internal/logx"
)

// CacheLoad loads the verified snapshot of a domain from its global cache. The
// cache holds manifest.json + stubs only (agents/commands deploy straight to
// the opencode dir and are never cached).
func CacheLoad(d domain.Domain) (*Fetched, error) {
	dir := cacheDir(d)
	f := CacheFetcher{Dir: dir}
	raw, err := f.Fetch("manifest.json")
	if err != nil {
		return nil, fmt.Errorf("cache empty — run: orbit %s update (or install)", d.Name)
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("cache integrity: %w", err)
	}
	fc := &Fetched{Manifest: manifest, Content: map[string][]byte{"manifest.json": raw}}
	for _, e := range manifest.Files.Stubs {
		data, err := f.Fetch(e.Path)
		if err != nil {
			return nil, fmt.Errorf("cache incomplete (%s) — run: orbit %s update", e.Path, d.Name)
		}
		if got := SHA256Hex(data); got != e.SHA256 {
			return nil, fmt.Errorf("cache integrity: sha256 mismatch for %s — run: orbit %s update", e.Path, d.Name)
		}
		fc.Content[e.Path] = data
	}
	return fc, nil
}

// CacheFetcher fetches from a domain's cache dir directly (paths relative to
// the domain root == cache dir contents).
type CacheFetcher struct{ Dir string }

func (c CacheFetcher) Fetch(relPath string) ([]byte, error) {
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return nil, fmt.Errorf("refusing path outside cache: %s", relPath)
	}
	return os.ReadFile(filepath.Join(c.Dir, clean))
}

// CacheSave atomically writes stubs + manifest.json into the domain cache dir.
func CacheSave(fc *Fetched, d domain.Domain) error {
	dir := cacheDir(d)
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

// CacheManifestHash returns the sha256 of the domain's cached manifest.json
// ("" if absent).
func CacheManifestHash(d domain.Domain) string {
	data, err := os.ReadFile(filepath.Join(cacheDir(d), "manifest.json"))
	if err != nil {
		return ""
	}
	return SHA256Hex(data)
}

// cacheDir is the canonical per-domain global cache. It is the single source
// of truth shared with config.CacheDir and deploy's ledger locations.
func cacheDir(d domain.Domain) string {
	return d.CacheDir()
}

// RemoteFetcher fetches one domain via the git binary (shallow clone),
// falling back to a GitHub/GitLab tarball download. The repo is cloned once
// per process and split into per-base subtrees, so two domains sharing a URL
// share a single clone.
type RemoteFetcher struct {
	URL      string // https git URL
	Ref      string
	TokenEnv string // name of env var holding the token (never the token itself)
	Domain   domain.Domain
}

// token returns the token value from the configured env var name.
func (r RemoteFetcher) token() string {
	if r.TokenEnv == "" {
		return ""
	}
	return os.Getenv(r.TokenEnv)
}

// remoteRepos caches the per-domain subtrees of a cloned repo, keyed by
// URL|ref. A single clone serves every domain; each entry maps
// base → (repo-relative path → bytes).
var (
	remoteMu    sync.Mutex
	remoteRepos = map[string]map[string]map[string][]byte{}
)

func (r RemoteFetcher) Fetch(relPath string) ([]byte, error) {
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return nil, fmt.Errorf("refusing path outside %s/: %s", r.Domain.Base, relPath)
	}
	repo, err := r.repo()
	if err != nil {
		return nil, err
	}
	baseTree, ok := repo[r.Domain.Base]
	if !ok {
		return nil, fmt.Errorf("%w: no %s/ folder in registry", ErrDomainAbsent, r.Domain.Base)
	}
	data, ok := baseTree[clean]
	if !ok {
		return nil, fmt.Errorf("file not in registry: %s", relPath)
	}
	return data, nil
}

// repo returns the clone-once, per-base tree for this URL|ref.
func (r RemoteFetcher) repo() (map[string]map[string][]byte, error) {
	key := r.URL + "\x00" + r.Ref
	remoteMu.Lock()
	if tree, ok := remoteRepos[key]; ok {
		remoteMu.Unlock()
		return tree, nil
	}
	remoteMu.Unlock()

	tree, err := r.fetchAll()
	if err != nil {
		return nil, err
	}
	remoteMu.Lock()
	remoteRepos[key] = tree
	remoteMu.Unlock()
	return tree, nil
}

func (r RemoteFetcher) fetchAll() (map[string]map[string][]byte, error) {
	if _, err := exec.LookPath("git"); err == nil {
		if tree, err := r.fetchGit(); err == nil {
			return tree, nil
		} else {
			logx.Debug("git fetch failed, falling back to tarball: %v", err)
		}
	}
	return r.fetchTarball()
}

// splitPath normalizes a repo-relative path into its components.
func splitPath(rel string) []string {
	return strings.Split(strings.TrimPrefix(filepath.ToSlash(rel), "./"), "/")
}

// knownBasePath finds the first known domain base component and returns the
// domain-relative path after it. Scanning rather than assuming a fixed offset
// tolerates a tarball's arbitrary wrapper prefix (e.g. <repo>-<ref>/).
func knownBasePath(parts []string) (base, inner string, ok bool) {
	for i, p := range parts {
		if p == "" || !domain.IsKnownBase(p) {
			continue
		}
		inner = strings.Join(parts[i+1:], "/")
		if inner == "" {
			return "", "", false
		}
		return p, inner, true
	}
	return "", "", false
}

func (r RemoteFetcher) fetchGit() (map[string]map[string][]byte, error) {
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
	tree := map[string]map[string][]byte{}
	err = filepath.Walk(repo, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if fi.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(repo, path)
		base, inner, ok := knownBasePath(splitPath(rel))
		if !ok {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if tree[base] == nil {
			tree[base] = map[string][]byte{}
		}
		tree[base][inner] = data
		return nil
	})
	return tree, err
}

func (r RemoteFetcher) fetchTarball() (map[string]map[string][]byte, error) {
	apiURL := strings.TrimSuffix(r.URL, ".git")
	switch {
	case strings.HasPrefix(apiURL, "https://github.com/"):
		// https://github.com/<org>/<repo> → API tarball endpoint.
		apiURL = strings.Replace(apiURL, "https://github.com/", "https://api.github.com/repos/", 1)
		apiURL = apiURL + "/tarball/" + r.Ref
	case strings.HasPrefix(apiURL, "https://gitlab.com/"):
		// https://gitlab.com/<path> → API v4 repository archive.
		// The project path is the segment(s) after the host, URL-encoded;
		// ref is a query parameter.
		rest := strings.TrimPrefix(apiURL, "https://gitlab.com/")
		apiURL = fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/repository/archive.tar.gz?sha=%s",
			url.PathEscape(strings.Trim(rest, "/")), url.QueryEscape(r.Ref))
	default:
		return nil, fmt.Errorf("registry tarball fallback supports github.com and gitlab.com URLs only — use a git URL with git installed: %s", r.URL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	if tok := r.token(); tok != "" {
		if strings.Contains(apiURL, "gitlab.com/api/v4") {
			req.Header.Set("PRIVATE-TOKEN", tok)
		} else {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
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

// extractTarGz extracts every known-domain subtree of the tarball.
func extractTarGz(r io.Reader) (map[string]map[string][]byte, error) {
	tree := map[string]map[string][]byte{}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base, inner, ok := knownBasePath(splitPath(hdr.Name))
		if !ok {
			continue
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, tr); err != nil {
			return nil, err
		}
		if tree[base] == nil {
			tree[base] = map[string][]byte{}
		}
		tree[base][inner] = buf.Bytes()
	}
	if len(tree) == 0 {
		return nil, fmt.Errorf("tarball contains no registry domain folders")
	}
	return tree, nil
}
