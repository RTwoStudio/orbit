package neocortex

import (
	"fmt"
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

var taskIDRe = regexp.MustCompile(`^T[0-9]+$`)

// NewTask scaffolds tasks/<ID>.md JIT (§5.10).
func NewTask(issue int, id, name string) (path string, warnings []string, err error) {
	// Preflight: plan Locked + concept hash verified.
	concept, err := ParseDoc(ConceptPath(issue))
	if err != nil {
		return "", nil, exit.New(exit.NotFound, "concept missing: "+ConceptPath(issue))
	}
	if err := VerifyHash(concept, concept.Get("Lock-Hash")); err != nil {
		return "", nil, err
	}
	plan, err := ParseDoc(PlanPath(issue))
	if err != nil {
		return "", nil, exit.New(exit.NotFound, "plan missing: "+PlanPath(issue))
	}
	if plan.Get("Status") != string(PlanLocked) {
		return "", nil, exit.New(exit.PreflightFailed, "plan is not Locked — tasks are JIT-created after plan lock")
	}
	dag, err := EffectiveDAG(plan)
	if err != nil {
		return "", nil, err
	}
	if !taskIDRe.MatchString(id) {
		return "", nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("task ID %q is invalid — syntax: T<number> (e.g. T1)", id))
	}
	row := dag.Get(id)
	if row == nil {
		return "", nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("task %s does not exist in the plan's effective Task DAG — new tasks enter via 'orbit neocortex addenda new'", id))
	}
	tp := TaskPath(issue, id)
	if fsutil.Exists(tp) {
		doc, perr := ParseDoc(tp)
		if perr == nil {
			placeholders := len(agentPlaceholderRe.FindAll(doc.Body, -1))
			return "", nil, exit.New(exit.StateConflict,
				fmt.Sprintf("%s already exists (status: %s, %d agent placeholders remaining) — resume it instead of recreating",
					tp, doc.Get("status"), placeholders))
		}
		return "", nil, exit.New(exit.StateConflict, fmt.Sprintf("%s already exists", tp))
	}

	// Resolve frontmatter fields from the DAG row.
	dependsOn := row.DependsOn
	blocks := dag.BlocksOf(id)
	origin, amendmentNNs := taskOrigin(plan, id)

	// Dep warnings (never block).
	for _, dep := range dependsOn {
		depPath := TaskPath(issue, dep)
		if !fsutil.Exists(depPath) {
			warnings = append(warnings,
				fmt.Sprintf("dependency %s has NO task file yet — %s will block until it is created", dep, id))
			continue
		}
		if ddoc, perr := ParseDoc(depPath); perr == nil && ddoc.Get("status") != string(StatusClose) {
			warnings = append(warnings,
				fmt.Sprintf("WARNING: dependency %s is not Close (status: %s) — %s starts blocked",
					dep, ddoc.Get("status"), id))
		}
	}

	cache, err := registry.CacheLoad()
	if err != nil {
		return "", nil, exit.New(exit.RegistryUnreachable, "registry cache is empty or corrupted — run: orbit neocortex update")
	}
	var stub string
	for _, e := range cache.Manifest.Files.Stubs {
		if filepath.Base(e.Path) == "task.stub.md" {
			stub = string(cache.Content[e.Path])
		}
	}
	rendered, err := Render(stub, map[string]string{
		"TASK_ID":          id,
		"TASK_NAME":        name,
		"ISSUE_ID":         fmt.Sprintf("%d", issue),
		"DEPENDS_ON":       strings.Join(dependsOn, ", "),
		"ORIGIN":           origin,
		"DATE":             time.Now().UTC().Format(time.RFC3339),
		"REGISTRY_VERSION": cache.Manifest.Version,
		"BLOCKED_BY":       depsString(dependsOn),
		"BLOCKS":           depsString(blocks),
	})
	if err != nil {
		return "", nil, exit.Wrap(exit.General, err, "cannot render task stub")
	}
	// amendments pre-list.
	doc, err := ParseDocBytes(tp, rendered)
	if err != nil {
		return "", nil, exit.Wrap(exit.General, err, "cannot parse rendered task")
	}
	for _, nn := range amendmentNNs {
		doc.AppendToList("amendments", fmt.Sprintf("%02d", nn))
	}
	if err := doc.Save(); err != nil {
		return "", nil, exit.Wrap(exit.IOError, err, "cannot write "+tp)
	}
	logx.Info("task created issue=%d task=%s origin=%s deps=%v", issue, id, origin, dependsOn)
	return tp, warnings, nil
}

// taskOrigin derives origin + pre-listed amendment NNs from the plan's
// Amendments history (carried through MODIFYs).
func taskOrigin(plan *Doc, id string) (string, []int) {
	amendments := ParseAmendments(plan.Body)
	var nns []int
	origin := "plan"
	originSet := false
	for _, a := range amendments {
		if amendmentMentions(a.Summary, id) {
			if !originSet && strings.Contains(a.Summary, "ADD "+id) {
				origin = fmt.Sprintf("addenda-%02d", a.NN)
				originSet = true
				continue
			}
			nns = append(nns, a.NN)
		}
	}
	if origin != "plan" && len(nns) > 0 {
		// Keep subsequent MODIFYs as amendments (already collected).
	}
	return origin, nns
}

func amendmentMentions(summary, id string) bool {
	for _, part := range strings.Split(summary, ", ") {
		fields := strings.Fields(part)
		if len(fields) == 2 && fields[1] == id {
			return true
		}
	}
	return false
}

// SetTaskStatus transitions a task's status with full preflights (§5.10).
func SetTaskStatus(issue int, id, input string) (from, to TaskStatus, err error) {
	to, err = ParseTaskStatus(input)
	if err != nil {
		return "", "", err
	}
	tp := TaskPath(issue, id)
	if !fsutil.Exists(tp) {
		return "", "", exit.New(exit.NotFound,
			fmt.Sprintf("task file %s does not exist — tasks are JIT: 'orbit neocortex task new %s \"Name\"'", tp, id))
	}
	doc, err := ParseDoc(tp)
	if err != nil {
		return "", "", exit.New(exit.PreflightFailed, err.Error())
	}
	fromFile := doc.Get("status")
	from = statusFromFile(fromFile)
	if err := CheckTransition(from, to); err != nil {
		return "", "", err
	}
	// Preflight for → Revise.
	if to == StatusRevise {
		if err := checkReviseReadiness(tp, doc.Body); err != nil {
			return "", "", err
		}
	}
	doc.Set("status", to.FileValue())
	if err := doc.Save(); err != nil {
		return "", "", exit.Wrap(exit.IOError, err, "cannot rewrite "+tp)
	}
	if err := appendCLILog(tp, from.FileValue(), to.FileValue()); err != nil {
		return "", "", exit.Wrap(exit.IOError, err, "cannot append CLI log")
	}
	logx.Info("task status | task=%s | %s → %s", id, from.FileValue(), to.FileValue())
	return from, to, nil
}

// statusFromFile maps YAML status strings to canonical.
func statusFromFile(s string) TaskStatus {
	switch s {
	case "In Progress":
		return StatusInProgress
	case "Revise":
		return StatusRevise
	case "Rework":
		return StatusRework
	case "Close":
		return StatusClose
	}
	return StatusOpen
}

// checkReviseReadiness: Completion Notes non-empty AND all `- [ ]` under
// Verification ticked.
func checkReviseReadiness(path string, body []byte) error {
	notes := StripHTMLComments(BulletFormSection(body, "Completion Notes"))
	if notes == "" {
		return exit.New(exit.PreflightFailed,
			fmt.Sprintf("%s: Completion Notes is empty — fill them before moving to Revise", path))
	}
	verif := BulletFormSection(body, "Verification")
	if verif != nil {
		for _, line := range strings.Split(string(verif), "\n") {
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "- [ ]") {
				return exit.New(exit.PreflightFailed,
					fmt.Sprintf("%s: unticked Verification item:\n  %s", path, t),
					"tick every verification checkbox before moving to Revise")
			}
		}
	}
	return nil
}

// appendCLILog appends "<RFC3339>  <from> → <to>" to the trailing CLI log
// comment block (creating it if absent).
func appendCLILog(path, from, to string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%s  %s → %s", time.Now().UTC().Format(time.RFC3339), from, to)
	content := string(data)
	marker := "<!-- CLI log"
	if idx := strings.LastIndex(content, marker); idx >= 0 {
		end := strings.Index(content[idx:], "-->")
		if end >= 0 {
			insertAt := idx + end
			return fsutil.AtomicWrite(path, []byte(content[:insertAt]+line+"\n"+content[insertAt:]), 0o644)
		}
	}
	block := fmt.Sprintf("\n<!-- CLI log\n%s\n-->\n", line)
	return fsutil.AppendToFile(path, []byte(block))
}

// TaskInfo is one row of task list / next.
type TaskInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	DependsOn []string `json:"dependsOn"`
	Origin    string   `json:"origin"`
	DepStatus []string `json:"dep_status"`
	Runnable  bool     `json:"runnable,omitempty"`
}

// ListTasks returns rows for task list.
func ListTasks(issue int) ([]TaskInfo, error) {
	plan, err := ParseDoc(PlanPath(issue))
	if err != nil {
		return nil, exit.New(exit.NotFound, "plan missing: "+PlanPath(issue))
	}
	dag, err := EffectiveDAG(plan)
	if err != nil {
		return nil, err
	}
	var out []TaskInfo
	for _, t := range dag.Tasks {
		info := TaskInfo{ID: t.ID, Name: t.Name, DependsOn: t.DependsOn}
		info.Origin, _ = taskOrigin(plan, t.ID)
		tp := TaskPath(issue, t.ID)
		if fsutil.Exists(tp) {
			if doc, perr := ParseDoc(tp); perr == nil {
				info.Status = doc.Get("status")
			}
		}
		if info.Status == "" {
			info.Status = "not created"
		}
		for _, dep := range t.DependsOn {
			depPath := TaskPath(issue, dep)
			if !fsutil.Exists(depPath) {
				info.DepStatus = append(info.DepStatus, dep+":not created")
				continue
			}
			if ddoc, perr := ParseDoc(depPath); perr == nil {
				info.DepStatus = append(info.DepStatus, dep+":"+ddoc.Get("status"))
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// NextTask returns the first Open task (DAG order) whose deps are all
// Close. None → nil, nil (exit 0 friendly).
func NextTask(issue int) (*TaskInfo, error) {
	rows, err := ListTasks(issue)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		r := rows[i]
		if r.Status != string(StatusOpen) {
			continue
		}
		allClosed := true
		for _, ds := range r.DepStatus {
			if !strings.HasSuffix(ds, ":"+string(StatusClose)) {
				allClosed = false
				break
			}
		}
		if allClosed {
			r.Runnable = true
			return &r, nil
		}
	}
	return nil, nil
}

// ShowTask reads a task file.
func ShowTask(issue int, id string) ([]byte, error) {
	tp := TaskPath(issue, id)
	if !fsutil.Exists(tp) {
		return nil, exit.New(exit.NotFound, "task file missing: "+tp)
	}
	return os.ReadFile(tp)
}
