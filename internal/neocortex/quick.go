package neocortex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
	"github.com/RTwoStudio/orbit/internal/logx"
	"github.com/RTwoStudio/orbit/internal/registry"
)

// NewQuickResult summarizes what quick new created.
type NewQuickResult struct {
	Number int
	Dir    string
	Path   string
}

// NewQuick scaffolds the light lane: issues/<NNNN>-issue/00-quick.md only (§5.4
// analogue). All-or-nothing: the dir is built under staging, then moved into
// place, then ACTIVE is written.
func NewQuick(title string, src IssueSource) (*NewQuickResult, error) {
	if strings.TrimSpace(title) == "" {
		return nil, exit.New(exit.PreflightFailed, "title must not be empty")
	}
	if len(title) > 120 {
		return nil, exit.New(exit.PreflightFailed, fmt.Sprintf("title is %d chars (max 120)", len(title)))
	}

	detail, source, err := resolveDetail(src)
	if err != nil {
		return nil, err
	}

	cache, err := registry.CacheLoad()
	if err != nil {
		return nil, exit.New(exit.RegistryUnreachable,
			"registry cache is empty or corrupted — run: orbit neocortex update", err.Error())
	}
	stub := stubContent(cache, "00-quick.stub.md")
	if stub == "" {
		return nil, exit.New(exit.RegistryUnreachable,
			"registry has no 00-quick.stub.md — run: orbit neocortex update")
	}

	n, err := NextIssueNumber()
	if err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot scan issues dir")
	}
	if fsutil.IsDir(IssueDir(n)) || fsutil.IsDir(QuickDir(n)) {
		return nil, exit.New(exit.StateConflict, fmt.Sprintf("issue %d already exists — refusing to overwrite", n))
	}

	rendered, err := Render(stub, map[string]string{
		"ISSUE_ID":         fmt.Sprintf("%d", n),
		"ISSUE_TITLE":      title,
		"DATE":             time.Now().UTC().Format(time.RFC3339),
		"SOURCE":           source,
		"REGISTRY_VERSION": cache.Manifest.Version,
		"DETAIL":           detail,
	})
	if err != nil {
		return nil, exit.Wrap(exit.General, err, "cannot render quick stub")
	}

	// Robustness: the quick stub carries {{DETAIL}} inside its Intent section;
	// if a registry version doesn't, still guarantee the fetched text lands in
	// Intent rather than being silently dropped.
	if strings.TrimSpace(detail) != "" && !strings.Contains(string(rendered), strings.TrimSpace(detail)) {
		rendered = injectIntoSection(rendered, "Intent", detail)
	}

	staging := QuickDir(n) + ".tmp-staging"
	os.RemoveAll(staging)
	defer os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot create "+staging)
	}
	if err := os.WriteFile(filepath.Join(staging, "00-quick.md"), rendered, 0o644); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot write quick file")
	}
	if err := os.Rename(staging, QuickDir(n)); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot move staging dir into place")
	}
	if err := WriteActive(n); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot write ACTIVE")
	}
	logx.Info("quick created issue=%d title=%q source=%s", n, title, source)
	return &NewQuickResult{Number: n, Dir: QuickDir(n), Path: QuickPath(n)}, nil
}

// QuickStatusOf reads a quick run's status.
func QuickStatusOf(n int) (QuickStatus, error) {
	doc, err := ParseDoc(QuickPath(n))
	if err != nil {
		if os.IsNotExist(err) {
			return "", exit.New(exit.NotFound, fmt.Sprintf("quick file missing: %s", QuickPath(n)))
		}
		return "", exit.New(exit.PreflightFailed, err.Error())
	}
	return quickStatusFromFile(doc.Get("Status")), nil
}

// SetQuickStatus applies a validated quick transition, appending a CLI log
// line. Close is refused while ## Result holds no real content.
func SetQuickStatus(n int, to QuickStatus) (QuickStatus, error) {
	path := QuickPath(n)
	if !fsutil.Exists(path) {
		return "", exit.New(exit.NotFound, fmt.Sprintf("quick file missing: %s", path))
	}
	doc, err := ParseDoc(path)
	if err != nil {
		return "", exit.New(exit.PreflightFailed, err.Error())
	}
	from := quickStatusFromFile(doc.Get("Status"))
	if err := CheckQuickTransition(from, to); err != nil {
		return "", err
	}
	if to == QuickClose {
		if err := checkQuickResult(path, doc.Body); err != nil {
			return "", err
		}
	}
	doc.Set("Status", to.FileValue())
	if err := doc.Save(); err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot rewrite "+path)
	}
	if err := appendCLILog(path, from.FileValue(), to.FileValue()); err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot append CLI log")
	}
	logx.Info("quick status | issue=%d | %s → %s", n, from.FileValue(), to.FileValue())
	return from, nil
}

// checkQuickResult requires the ## Result section to carry real content
// (HTML comments do not count).
func checkQuickResult(path string, body []byte) error {
	if StripHTMLComments(Section(body, "Result")) == "" {
		return exit.New(exit.PreflightFailed,
			fmt.Sprintf("%s: ## Result is empty — record what was done before Close", path),
			"fill Result, then retry the close")
	}
	return nil
}

// PromoteQuick converts a quick run into a default-lane issue IN PLACE:
// 00-concept.md is rendered from the quick Intent (with a promotion note in
// Detail), the standard tree is created, and 00-quick.md is removed. No
// folder move, no restart.
func PromoteQuick(n int) (string, error) {
	quickPath := QuickPath(n)
	qdoc, err := ParseDoc(quickPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", exit.New(exit.NotFound, fmt.Sprintf("quick file missing: %s", quickPath))
		}
		return "", exit.New(exit.PreflightFailed, err.Error())
	}
	if fsutil.Exists(ConceptPath(n)) {
		return "", exit.New(exit.StateConflict,
			fmt.Sprintf("%s already exists — issue %d is already in the default lane", ConceptPath(n), n))
	}

	cache, err := registry.CacheLoad()
	if err != nil {
		return "", exit.New(exit.RegistryUnreachable,
			"registry cache is empty or corrupted — run: orbit neocortex update", err.Error())
	}
	conceptStub := stubContent(cache, "00-concept.stub.md")
	planStub := stubContent(cache, "01-plan.stub.md")
	if conceptStub == "" || planStub == "" {
		return "", exit.New(exit.RegistryUnreachable,
			"registry is missing concept/plan stubs — run: orbit neocortex update")
	}

	title := quickTitle(qdoc)
	intent := strings.TrimSpace(StripHTMLComments(Section(qdoc.Body, "Intent")))
	detail := fmt.Sprintf("<!-- Promoted from quick run issue %d. The original Intent follows. -->\n\n%s", n, intent)

	now := time.Now().UTC().Format(time.RFC3339)
	vals := map[string]string{
		"ISSUE_ID":         fmt.Sprintf("%d", n),
		"ISSUE_TITLE":      title,
		"DATE":             now,
		"SOURCE":           "promoted:quick",
		"REGISTRY_VERSION": cache.Manifest.Version,
	}
	conceptVals := map[string]string{}
	for k, v := range vals {
		conceptVals[k] = v
	}
	conceptVals["DETAIL"] = detail

	concept, err := Render(conceptStub, conceptVals)
	if err != nil {
		return "", exit.Wrap(exit.General, err, "cannot render concept stub")
	}
	plan, err := Render(planStub, vals)
	if err != nil {
		return "", exit.Wrap(exit.General, err, "cannot render plan stub")
	}

	// Rename the folder <NNNN>-quick → <NNNN>-issue so the name stays truthful.
	oldDir := QuickDir(n)
	newDir := IssueDir(n)
	if fsutil.IsDir(oldDir) {
		if fsutil.Exists(newDir) {
			return "", exit.New(exit.StateConflict,
				fmt.Sprintf("%s already exists — cannot promote in place", newDir))
		}
		if err := os.Rename(oldDir, newDir); err != nil {
			return "", exit.Wrap(exit.IOError, err, "cannot rename "+oldDir+" → "+newDir)
		}
	}
	if err := os.Remove(filepath.Join(newDir, "00-quick.md")); err != nil && !os.IsNotExist(err) {
		return "", exit.Wrap(exit.IOError, err, "cannot remove quick file")
	}

	for _, d := range []string{TasksDir(n), AddendaDir(n), NotesDir(n)} {
		if err := fsutil.EnsureDir(d); err != nil {
			return "", exit.Wrap(exit.IOError, err, "cannot create "+d)
		}
	}
	if err := fsutil.AtomicWrite(ConceptPath(n), concept, 0o644); err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot write concept")
	}
	if err := fsutil.AtomicWrite(PlanPath(n), plan, 0o644); err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot write plan")
	}
	logx.Info("quick promoted issue=%d → default lane", n)
	return ConceptPath(n), nil
}

// quickTitle derives the plain title from a quick file's H1 ("# Quick: X").
func quickTitle(doc *Doc) string {
	if h1 := firstH1(doc.Body); h1 != "" {
		return strings.TrimSpace(strings.TrimPrefix(h1, "Quick: "))
	}
	return fmt.Sprintf("Issue %s", doc.Get("Issue-ID"))
}

// injectIntoSection inserts text at the end of a named "## " section, before
// the next heading (or EOF). Used to guarantee verbatim intake lands in Intent
// even if a stub lacks the {{DETAIL}} token.
func injectIntoSection(body []byte, name, text string) []byte {
	lines := strings.Split(string(body), "\n")
	start := -1
	end := len(lines)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if start < 0 {
			if t == "## "+name {
				start = i + 1
			}
			continue
		}
		if strings.HasPrefix(t, "## ") || strings.HasPrefix(t, "# ") {
			end = i
			break
		}
	}
	if start < 0 {
		return append(append(body, []byte("\n\n## "+name+"\n\n")...), []byte(text+"\n")...)
	}
	block := strings.Split(text, "\n")
	out := make([]string, 0, len(lines)+len(block)+2)
	out = append(out, lines[:end]...)
	out = append(out, "", strings.TrimSuffix(text, "\n"))
	out = append(out, lines[end:]...)
	return []byte(strings.Join(out, "\n"))
}
