package neocortex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
	"github.com/RTwoStudio/orbit/internal/logx"
	"github.com/RTwoStudio/orbit/internal/registry"
)

// IssueSource selects the Detail injection mode for issue new.
type IssueSource struct {
	Mode      string // interactive | file | remote
	FilePath  string
	RemoteURL string
	TokenEnv  string
	Title_    string // informational; title is passed separately
}

// NewIssueResult summarizes what issue new created.
type NewIssueResult struct {
	Number int
	Dir    string
	Tree   []string
}

var (
	githubIssueRe = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/(?:issues|pull)/(\d+)`)
	gitlabIssueRe = regexp.MustCompile(`^https://(?:www\.)?gitlab\.com/(.+?)/(?:-/?)?(?:issues|merge_requests)/(\d+)`)
	gitlabBareRe  = regexp.MustCompile(`^gitlab\.com/(.+?)/(?:-/?)?(?:issues|merge_requests)/(\d+)`)
)

// remoteRef describes how to fetch a remote issue/PR across providers.
type remoteRef struct {
	provider string // "github" | "gitlab"
	api      string // fully-formed API URL
	prefix   string // header line for the injected content
}

// parseRemoteRef maps a GitHub or GitLab issue/PR reference to its API call.
// Accepts full URLs (https://github.com/o/r/issues/1, https://gitlab.com/g/p/-/issues/1)
// and a GitLab shorthand (gitlab.com/g/p/issues/1). An unrecognized reference
// is fetched as-is (raw GET) — useful for arbitrary content URLs.
func parseRemoteRef(rawurl string) remoteRef {
	if m := githubIssueRe.FindStringSubmatch(rawurl); m != nil {
		return remoteRef{
			provider: "github",
			api:      fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%s", m[1], m[2], m[3]),
			prefix:   fmt.Sprintf("# %s/%s#%s\n\n", m[1], m[2], m[3]),
		}
	}
	if m := gitlabIssueRe.FindStringSubmatch(rawurl); m != nil {
		return gitlabRef(m[1], m[2])
	}
	if m := gitlabBareRe.FindStringSubmatch(rawurl); m != nil {
		return gitlabRef(m[1], m[2])
	}
	return remoteRef{provider: "raw", api: rawurl}
}

// gitlabRef builds the GitLab API v4 URL. Project paths are URL-encoded
// (group/subgroup/project → group%2Fsubgroup%2Fproject).
func gitlabRef(projectPath, iid string) remoteRef {
	base := strings.TrimRight(envOr("ORBIT_GITLAB_URL", "https://gitlab.com"), "/")
	scheme := "https://"
	host := base
	if i := strings.Index(base, "://"); i >= 0 {
		scheme = base[:i+3]
		host = base[i+3:]
	}
	enc := url.PathEscape(strings.Trim(projectPath, "/"))
	return remoteRef{
		provider: "gitlab",
		api:      fmt.Sprintf("%s%s/api/v4/projects/%s/issues/%s", scheme, host, enc, iid),
		prefix:   fmt.Sprintf("# %s#%s\n\n", strings.Trim(projectPath, "/"), iid),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ProviderOf classifies a remote reference by provider, for token selection.
func ProviderOf(rawurl string) string {
	return parseRemoteRef(rawurl).provider
}

// fetchRemote GETs content for a remote source (GitHub/GitLab issue URL →
// API, or a raw URL), 15s timeout. Failure is registry_unreachable; nothing
// is written.
func fetchRemote(rawurl, tokenEnv string) (string, error) {
	ref := parseRemoteRef(rawurl)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref.api, nil)
	if err != nil {
		return "", exit.New(exit.RegistryUnreachable, "invalid remote URL "+rawurl)
	}
	switch ref.provider {
	case "github":
		req.Header.Set("Accept", "application/vnd.github+json")
	case "gitlab":
		req.Header.Set("Accept", "application/json")
	}
	if tokenEnv != "" {
		if tok := os.Getenv(tokenEnv); tok != "" {
			switch ref.provider {
			case "gitlab":
				// GitLab uses PRIVATE-TOKEN; a "Bearer" scheme also works on
				// recent versions, so send both-safe: PRIVATE-TOKEN.
				req.Header.Set("PRIVATE-TOKEN", tok)
			default:
				req.Header.Set("Authorization", "Bearer "+tok)
			}
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", exit.New(exit.RegistryUnreachable, fmt.Sprintf("remote fetch failed: %v — nothing written", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", exit.New(exit.RegistryUnreachable,
			fmt.Sprintf("remote fetch failed: HTTP %d from %s — nothing written", resp.StatusCode, ref.api))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", exit.New(exit.RegistryUnreachable, fmt.Sprintf("remote read failed: %v — nothing written", err))
	}
	switch ref.provider {
	case "github":
		var payload struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if json.Unmarshal(body, &payload) == nil && (payload.Title != "" || payload.Body != "") {
			return ref.prefix + payload.Title + "\n\n" + payload.Body, nil
		}
	case "gitlab":
		var payload struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		}
		if json.Unmarshal(body, &payload) == nil && (payload.Title != "" || payload.Description != "") {
			return ref.prefix + payload.Title + "\n\n" + payload.Description, nil
		}
	}
	return ref.prefix + string(body), nil
}

// NewIssue scaffolds the full issue tree (§5.4). All-or-nothing: the whole
// tree is built under a staging dir, then moved into place.
func NewIssue(title string, src IssueSource) (*NewIssueResult, error) {
	if strings.TrimSpace(title) == "" {
		return nil, exit.New(exit.PreflightFailed, "title must not be empty")
	}
	if len(title) > 120 {
		return nil, exit.New(exit.PreflightFailed, fmt.Sprintf("title is %d chars (max 120)", len(title)))
	}

	cache, err := registry.CacheLoad()
	if err != nil {
		return nil, exit.New(exit.RegistryUnreachable,
			"registry cache is empty or corrupted — run: orbit neocortex update",
			err.Error())
	}
	deployedVersion := cache.Manifest.Version

	// Resolve Detail content per source mode.
	detail, source, err := resolveDetail(src)
	if err != nil {
		return nil, err
	}

	n, err := NextIssueNumber()
	if err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot scan issues dir")
	}
	// Defensive collision check (either lane's folder for n).
	if fsutil.IsDir(IssueDir(n)) || fsutil.IsDir(QuickDir(n)) {
		return nil, exit.New(exit.StateConflict, fmt.Sprintf("issue %d already exists — refusing to overwrite", n))
	}

	now := time.Now().UTC().Format(time.RFC3339)
	issueVals := map[string]string{
		"ISSUE_ID":         fmt.Sprintf("%d", n),
		"ISSUE_TITLE":      title,
		"DATE":             now,
		"SOURCE":           source,
		"REGISTRY_VERSION": deployedVersion,
		"DETAIL":           detail,
	}
	concept, err := Render(stubContent(cache, "00-concept.stub.md"), issueVals)
	if err != nil {
		return nil, exit.Wrap(exit.General, err, "cannot render concept stub")
	}
	planVals := map[string]string{
		"ISSUE_ID":         issueVals["ISSUE_ID"],
		"ISSUE_TITLE":      issueVals["ISSUE_TITLE"],
		"DATE":             now,
		"REGISTRY_VERSION": deployedVersion,
	}
	plan, err := Render(stubContent(cache, "01-plan.stub.md"), planVals)
	if err != nil {
		return nil, exit.Wrap(exit.General, err, "cannot render plan stub")
	}

	// Build in a staging dir, then rename into place (all-or-nothing).
	staging := IssueDir(n) + ".tmp-staging"
	os.RemoveAll(staging)
	defer os.RemoveAll(staging)
	dirs := []string{staging, filepath.Join(staging, "tasks"), filepath.Join(staging, "addenda"), filepath.Join(staging, "notes")}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, exit.Wrap(exit.IOError, err, "cannot create "+d)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, "00-concept.md"), concept, 0o644); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot write concept")
	}
	if err := os.WriteFile(filepath.Join(staging, "01-plan.md"), plan, 0o644); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot write plan")
	}
	if err := os.Rename(staging, IssueDir(n)); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot move staging tree into place")
	}
	if err := WriteActive(n); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot write ACTIVE")
	}
	logx.Info("issue created issue=%d title=%q source=%s", n, title, source)

	return &NewIssueResult{
		Number: n,
		Dir:    IssueDir(n),
		Tree: []string{
			fmt.Sprintf("issues/%s/00-concept.md", IssueDirName(n, LaneIssue)),
			fmt.Sprintf("issues/%s/01-plan.md", IssueDirName(n, LaneIssue)),
			fmt.Sprintf("issues/%s/tasks/", IssueDirName(n, LaneIssue)),
			fmt.Sprintf("issues/%s/addenda/", IssueDirName(n, LaneIssue)),
			fmt.Sprintf("issues/%s/notes/", IssueDirName(n, LaneIssue)),
		},
	}, nil
}

// stubContent returns the cached raw content of a registry stub by base name
// ("" when the registry has no such stub).
func stubContent(cache *registry.Fetched, name string) string {
	for _, e := range cache.Manifest.Files.Stubs {
		if filepath.Base(e.Path) == name {
			return string(cache.Content[e.Path])
		}
	}
	return ""
}

// resolveDetail resolves the Detail/Intent injection for an intake source.
// interactive → an agent-instruction placeholder; file/remote → verbatim
// content, with the provenance string recorded in the SOURCE frontmatter.
func resolveDetail(src IssueSource) (detail, source string, err error) {
	switch src.Mode {
	case "interactive":
		return "<!-- Agent: transcribe the Orchestrator's answers here verbatim,\nno embellishment. -->", "interactive", nil
	case "file":
		data, rerr := os.ReadFile(src.FilePath)
		if rerr != nil {
			return "", "", exit.Wrap(exit.NotFound, rerr, "cannot read source file "+src.FilePath)
		}
		return string(data), "file:" + src.FilePath, nil
	case "remote":
		data, rerr := fetchRemote(src.RemoteURL, src.TokenEnv)
		if rerr != nil {
			return "", "", rerr
		}
		return data, "remote:" + src.RemoteURL, nil
	}
	return "", "", exit.New(exit.Usage,
		"exactly one source mode is required (interactive | file | remote)")
}

// LockIssue performs issue lock (§5.5) with ordered preflights.
func LockIssue(n int) (string, error) {
	path := ConceptPath(n)
	doc, err := ParseDoc(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", exit.New(exit.NotFound, fmt.Sprintf("concept file missing: %s", path))
		}
		return "", exit.New(exit.PreflightFailed, err.Error())
	}
	// 1. Status must be Draft.
	if doc.Get("Status") == string(ConceptLocked) {
		return "", exit.New(exit.StateConflict, fmt.Sprintf("%s is already Locked — lock is one-way", path))
	}
	if doc.Get("Status") != string(ConceptDraft) {
		return "", exit.New(exit.PreflightFailed,
			fmt.Sprintf("%s has Status %q (must be %q)", path, doc.Get("Status"), ConceptDraft))
	}
	// 2–3. Body guards.
	if err := GuardBody(path, doc.Body); err != nil {
		return "", err
	}
	// Actions.
	hash := LockHash(doc.Body)
	doc.Set("Status", string(ConceptLocked))
	doc.Set("Locked-At", time.Now().UTC().Format(time.RFC3339))
	doc.Set("Lock-Hash", hash)
	if err := doc.Save(); err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot rewrite "+path)
	}
	logx.Info("issue lock | issue=%d | locked hash=%s", n, hash[:19]+"…")
	return hash, nil
}

// IssueInfo is one row of issue list / status.
type IssueInfo struct {
	Number          int            `json:"id"`
	Title           string         `json:"title"`
	Lane            string         `json:"lane"` // "full" | "quick"
	Quick           string         `json:"quick_status,omitempty"`
	Concept         string         `json:"concept_status,omitempty"`
	Plan            string         `json:"plan_status,omitempty"`
	Addenda         []string       `json:"addenda_counts,omitempty"`
	ConceptAddenda  int            `json:"addenda_draft,omitempty"`
	ApprovedAddenda int            `json:"addenda_approved,omitempty"`
	AppliedAddenda  int            `json:"addenda_applied,omitempty"`
	TaskCounts      map[string]int `json:"task_counts,omitempty"`
	Active          bool           `json:"active"`
}

// ListIssuesInfo collects rows for issue list.
func ListIssuesInfo() ([]IssueInfo, error) {
	nums, err := ListIssues()
	if err != nil {
		return nil, err
	}
	active := -1
	if n, err := ReadActive(); err == nil {
		active = n
	}
	var out []IssueInfo
	for _, n := range nums {
		info := IssueInfo{Number: n, TaskCounts: map[string]int{}}
		info.Active = n == active
		// The folder suffix is authoritative; Class: quick must agree with it.
		if IssueLane(n) == LaneQuick {
			info.Lane = "quick"
			qdoc, qerr := ParseDoc(QuickPath(n))
			if qerr != nil {
				info.Quick = "unreadable"
				out = append(out, info)
				continue
			}
			info.Quick = qdoc.Get("Status")
			if qdoc.Get("Class") != string(LaneQuick) {
				info.Quick += " (Class mismatch)"
			}
			if h1 := firstH1(qdoc.Body); h1 != "" {
				info.Title = strings.TrimSpace(strings.TrimPrefix(h1, "Quick: "))
			}
			out = append(out, info)
			continue
		}
		info.Lane = "full"
		if doc, err := ParseDoc(ConceptPath(n)); err == nil {
			info.Concept = doc.Get("Status")
			if h1 := firstH1(doc.Body); h1 != "" {
				info.Title = strings.TrimPrefix(h1, "Concept: ")
			}
		}
		if doc, err := ParseDoc(PlanPath(n)); err == nil {
			info.Plan = doc.Get("Status")
		}
		// Addenda counts.
		if entries, err := os.ReadDir(AddendaDir(n)); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				if doc, err := ParseDoc(filepath.Join(AddendaDir(n), e.Name())); err == nil {
					switch doc.Get("Status") {
					case "Draft":
						info.ConceptAddenda++
					case "Approved":
						info.ApprovedAddenda++
					case "Applied":
						info.AppliedAddenda++
					}
				}
			}
		}
		info.Addenda = []string{
			fmt.Sprintf("%d draft", info.ConceptAddenda),
			fmt.Sprintf("%d approved", info.ApprovedAddenda),
			fmt.Sprintf("%d applied", info.AppliedAddenda),
		}
		// Task counts.
		if entries, err := os.ReadDir(TasksDir(n)); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				if doc, err := ParseDoc(filepath.Join(TasksDir(n), e.Name())); err == nil {
					info.TaskCounts[doc.Get("status")]++
				} else {
					info.TaskCounts["unreadable"]++
				}
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// SwitchIssue validates the target issue and writes ACTIVE.
func SwitchIssue(n int) error {
	if IssueLane(n) == "" {
		return exit.New(exit.NotFound, fmt.Sprintf("issue %d does not exist under %s", n, IssuesDir()),
			"list issues with: orbit neocortex issue list")
	}
	if err := WriteActive(n); err != nil {
		return exit.Wrap(exit.IOError, err, "cannot write ACTIVE")
	}
	return nil
}

// firstH1 returns the text of the first "# " line.
func firstH1(body []byte) string {
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return ""
}
