package neocortex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

var githubIssueRe = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/(?:issues|pull)/(\d+)`)

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
	stubOf := func(name string) string {
		for _, e := range cache.Manifest.Files.Stubs {
			if filepath.Base(e.Path) == name {
				return string(cache.Content[e.Path])
			}
		}
		return ""
	}
	deployedVersion := cache.Manifest.Version

	// Resolve Detail content per source mode.
	var detail, source string
	switch src.Mode {
	case "interactive":
		detail = "<!-- Agent: transcribe the Orchestrator's answers here verbatim,\nno embellishment. -->"
		source = "interactive"
	case "file":
		data, err := os.ReadFile(src.FilePath)
		if err != nil {
			return nil, exit.Wrap(exit.NotFound, err, "cannot read source file "+src.FilePath)
		}
		detail = string(data)
		source = "file:" + src.FilePath
	case "remote":
		data, err := fetchRemote(src.RemoteURL, src.TokenEnv)
		if err != nil {
			return nil, err
		}
		detail = data
		source = "remote:" + src.RemoteURL
	default:
		return nil, exit.New(exit.Usage, "exactly one source flag is required (--interactive | --from-remote | --from-file)")
	}

	n, err := NextIssueNumber()
	if err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot scan issues dir")
	}
	// Defensive collision check.
	if fsutil.IsDir(IssueDir(n)) {
		return nil, exit.New(exit.StateConflict, fmt.Sprintf("issue-%d already exists — refusing to overwrite", n))
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
	concept, err := Render(stubOf("00-concept.stub.md"), issueVals)
	if err != nil {
		return nil, exit.Wrap(exit.General, err, "cannot render concept stub")
	}
	planVals := map[string]string{
		"ISSUE_ID":         issueVals["ISSUE_ID"],
		"ISSUE_TITLE":      issueVals["ISSUE_TITLE"],
		"DATE":             now,
		"REGISTRY_VERSION": deployedVersion,
	}
	plan, err := Render(stubOf("01-plan.stub.md"), planVals)
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
			fmt.Sprintf("issues/issue-%d/00-concept.md", n),
			fmt.Sprintf("issues/issue-%d/01-plan.md", n),
			fmt.Sprintf("issues/issue-%d/tasks/", n),
			fmt.Sprintf("issues/issue-%d/addenda/", n),
			fmt.Sprintf("issues/issue-%d/notes/", n),
		},
	}, nil
}

// fetchRemote GETs content for a remote source (GitHub issue URL → API),
// 15s timeout. Failure is registry_unreachable; nothing is written.
func fetchRemote(rawurl, tokenEnv string) (string, error) {
	api := rawurl
	titlePrefix := ""
	if m := githubIssueRe.FindStringSubmatch(rawurl); m != nil {
		api = fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%s", m[1], m[2], m[3])
		titlePrefix = "# " + m[1] + "/" + m[2] + "#" + m[3] + "\n\n"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return "", exit.New(exit.RegistryUnreachable, "invalid remote URL "+rawurl)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tokenEnv != "" {
		if tok := os.Getenv(tokenEnv); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", exit.New(exit.RegistryUnreachable, fmt.Sprintf("remote fetch failed: %v — nothing written", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", exit.New(exit.RegistryUnreachable,
			fmt.Sprintf("remote fetch failed: HTTP %d from %s — nothing written", resp.StatusCode, api))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", exit.New(exit.RegistryUnreachable, fmt.Sprintf("remote read failed: %v — nothing written", err))
	}
	if strings.Contains(api, "api.github.com") {
		var payload struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if json.Unmarshal(body, &payload) == nil {
			out := titlePrefix + payload.Title + "\n\n" + payload.Body
			return out, nil
		}
	}
	return titlePrefix + string(body), nil
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

// IssueInfo is one row of issue list.
type IssueInfo struct {
	Number          int            `json:"id"`
	Title           string         `json:"title"`
	Concept         string         `json:"concept_status"`
	Plan            string         `json:"plan_status"`
	Addenda         []string       `json:"addenda_counts"`
	ConceptAddenda  int            `json:"addenda_draft"`
	ApprovedAddenda int            `json:"addenda_approved"`
	AppliedAddenda  int            `json:"addenda_applied"`
	TaskCounts      map[string]int `json:"task_counts"`
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
	if !fsutil.IsDir(IssueDir(n)) {
		return exit.New(exit.NotFound, fmt.Sprintf("issue-%d does not exist under %s", n, IssuesDir()),
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
