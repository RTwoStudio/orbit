package neocortex

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/RTwoStudio/orbit-cli/internal/exit"
	"github.com/RTwoStudio/orbit-cli/internal/fsutil"
	"github.com/RTwoStudio/orbit-cli/internal/logx"
	"github.com/RTwoStudio/orbit-cli/internal/registry"
)

func regexpAmendment() *regexp.Regexp {
	return regexp.MustCompile(`^- addenda-(\d+): "(.*?)" — applied (.*?) — (.*)$`)
}

// AddendaDelta is one parsed Plan Changes line.
type AddendaDelta struct {
	Verb    string // ADD | REMOVE | MODIFY
	ID      string
	Name    string
	Deps    []string
	Context string
	Line    string
}

var (
	addLineRe    = regexp.MustCompile(`^- ADD \[(T[0-9]+)\] (.+?) \(Blocked by: (None|T[0-9]+(?:, T[0-9]+)*)\)(?: — (.*))?$`)
	removeLineRe = regexp.MustCompile(`^- REMOVE \[(T[0-9]+)\](?: — (.*))?$`)
	modifyLineRe = regexp.MustCompile(`^- MODIFY \[(T[0-9]+)\] (.+?) \(Blocked by: (None|T[0-9]+(?:, T[0-9]+)*)\)(?: — (.*))?$`)
)

// ParseAddendaDeltas extracts ADD/REMOVE/MODIFY lines from the Plan
// Changes section. Lines inside HTML comment blocks are ignored; a line
// that starts with a delta verb but fails to parse is an error (quoted).
func ParseAddendaDeltas(body []byte) ([]AddendaDelta, error) {
	sec := Section(body, "Plan Changes")
	if sec == nil {
		return nil, nil
	}
	var out []AddendaDelta
	inComment := false
	for _, raw := range strings.Split(string(sec), "\n") {
		line := strings.TrimRight(raw, "\r")
		t := strings.TrimSpace(line)
		if inComment {
			if strings.Contains(line, "-->") {
				inComment = false
			}
			continue
		}
		if strings.HasPrefix(t, "<!--") {
			if !strings.Contains(line, "-->") {
				inComment = true
			}
			continue
		}
		if t == "" {
			continue
		}
		if m := addLineRe.FindStringSubmatch(t); m != nil {
			out = append(out, AddendaDelta{Verb: "ADD", ID: m[1], Name: m[2], Deps: splitDeps(m[3]), Context: m[4], Line: t})
			continue
		}
		if m := modifyLineRe.FindStringSubmatch(t); m != nil {
			out = append(out, AddendaDelta{Verb: "MODIFY", ID: m[1], Name: m[2], Deps: splitDeps(m[3]), Context: m[4], Line: t})
			continue
		}
		if m := removeLineRe.FindStringSubmatch(t); m != nil {
			out = append(out, AddendaDelta{Verb: "REMOVE", ID: m[1], Line: t})
			continue
		}
		if strings.HasPrefix(t, "- ADD ") || strings.HasPrefix(t, "- REMOVE ") || strings.HasPrefix(t, "- MODIFY ") {
			return nil, fmt.Errorf("cannot parse delta line: %q", t)
		}
	}
	return out, nil
}

func splitDeps(s string) []string {
	if s == "None" {
		return nil
	}
	return strings.Split(s, ", ")
}

func depsString(deps []string) string {
	if len(deps) == 0 {
		return "None"
	}
	return strings.Join(deps, ", ")
}

// NewAddenda scaffolds NN-<slug>.md in the issue's addenda dir (§5.9).
func NewAddenda(issue int, title string) (string, error) {
	// Preflight: plan Locked + concept hash verified.
	concept, err := ParseDoc(ConceptPath(issue))
	if err != nil {
		return "", exit.New(exit.NotFound, "concept missing: "+ConceptPath(issue))
	}
	if err := VerifyHash(concept, concept.Get("Lock-Hash")); err != nil {
		return "", err
	}
	plan, err := ParseDoc(PlanPath(issue))
	if err != nil {
		return "", exit.New(exit.NotFound, "plan missing: "+PlanPath(issue))
	}
	if plan.Get("Status") != string(PlanLocked) {
		return "", exit.New(exit.PreflightFailed,
			fmt.Sprintf("plan is not Locked (Status: %s) — lock the plan before addenda", plan.Get("Status")))
	}

	// Next NN = max+1 over addenda files.
	entries, _ := os.ReadDir(AddendaDir(issue))
	max := 0
	for _, e := range entries {
		var nn int
		if _, err := fmt.Sscanf(e.Name(), "%d-", &nn); err == nil && nn > max {
			max = nn
		}
	}
	nn := max + 1

	cache, err := registry.CacheLoad()
	if err != nil {
		return "", exit.New(exit.RegistryUnreachable, "registry cache is empty or corrupted — run: orbit neocortex update")
	}
	var stub string
	for _, e := range cache.Manifest.Files.Stubs {
		if filepath.Base(e.Path) == "addenda.stub.md" {
			stub = string(cache.Content[e.Path])
		}
	}
	rendered, err := Render(stub, map[string]string{
		"ADDENDA_NUM":      fmt.Sprintf("%d", nn),
		"ADDENDA_TITLE":    title,
		"ISSUE_ID":         fmt.Sprintf("%d", issue),
		"DATE":             time.Now().UTC().Format(time.RFC3339),
		"REGISTRY_VERSION": cache.Manifest.Version,
	})
	if err != nil {
		return "", exit.Wrap(exit.General, err, "cannot render addenda stub")
	}

	fname := fmt.Sprintf("%s-%s.md", AddendaFilePrefix(nn), SanitizeSlug(title))
	path := filepath.Join(AddendaDir(issue), fname)
	if err := fsutil.AtomicWrite(path, rendered, 0o644); err != nil {
		return "", exit.Wrap(exit.IOError, err, "cannot write "+path)
	}
	logx.Info("addenda created issue=%d nn=%d path=%s", issue, nn, fname)
	return path, nil
}

// ApproveAddenda approves Draft addenda NN (one-way).
func ApproveAddenda(issue, nn int) error {
	path, err := FindAddendaFile(issue, nn)
	if err != nil {
		return exit.New(exit.NotFound, fmt.Sprintf("addenda %02d not found in issue-%d", nn, issue))
	}
	doc, err := ParseDoc(path)
	if err != nil {
		return exit.New(exit.PreflightFailed, err.Error())
	}
	switch doc.Get("Status") {
	case "Approved", "Applied":
		return exit.New(exit.StateConflict, fmt.Sprintf("%s is already %s — approval is one-way", path, doc.Get("Status")))
	case "Draft":
	default:
		return exit.New(exit.PreflightFailed, fmt.Sprintf("%s has unexpected Status %q", path, doc.Get("Status")))
	}
	if agentPlaceholderRe.Match(doc.Body) {
		return exit.New(exit.PreflightFailed,
			fmt.Sprintf("%s still contains '<!-- Agent:' placeholders — resolve them before approval", path))
	}
	deltas, err := ParseAddendaDeltas(doc.Body)
	if err != nil {
		return exit.New(exit.PreflightFailed, err.Error())
	}
	if len(deltas) == 0 {
		return exit.New(exit.PreflightFailed,
			fmt.Sprintf("%s: Plan Changes section has no ADD/REMOVE/MODIFY lines", path))
	}
	doc.Set("Status", "Approved")
	doc.Set("Approved-At", time.Now().UTC().Format(time.RFC3339))
	if err := doc.Save(); err != nil {
		return exit.Wrap(exit.IOError, err, "cannot rewrite "+path)
	}
	logx.Info("addenda approved issue=%d nn=%d deltas=%d", issue, nn, len(deltas))
	return nil
}

// AddendaInfo is one row of addenda list.
type AddendaInfo struct {
	NN         int    `json:"nn"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Created    string `json:"created"`
	ApprovedAt string `json:"approved_at,omitempty"`
	AppliedAt  string `json:"applied_at,omitempty"`
	File       string `json:"file"`
}

// ListAddenda returns all addenda rows for an issue, sorted by NN.
func ListAddenda(issue int) ([]AddendaInfo, error) {
	dir := AddendaDir(issue)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, exit.Wrap(exit.IOError, err, "cannot read addenda dir")
	}
	var out []AddendaInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		doc, err := ParseDoc(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var nn int
		fmt.Sscanf(e.Name(), "%d-", &nn)
		info := AddendaInfo{
			NN:         nn,
			Status:     doc.Get("Status"),
			Created:    doc.Get("Created"),
			ApprovedAt: doc.Get("Approved-At"),
			AppliedAt:  doc.Get("Applied-At"),
			File:       filepath.Join(dir, e.Name()),
		}
		if h1 := firstH1(doc.Body); h1 != "" {
			info.Title = strings.TrimSpace(strings.TrimPrefix(h1, fmt.Sprintf("Addenda %d:", nn)))
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NN < out[j].NN })
	return out, nil
}

// AddendaApplyResult reports the apply diff.
type AddendaApplyResult struct {
	Before  []string
	After   []string
	Summary string
}

// ApplyAddenda applies Approved addenda NN to the locked plan (§5.9):
// validates the delta, simulates the resulting DAG, then rewrites the
// plan, appends the Amendments line, re-chains the hash, stamps task
// files, and marks the addenda Applied — all-or-nothing at the validation
// level, sequential atomic writes at the file level.
func ApplyAddenda(issue, nn int) (*AddendaApplyResult, error) {
	// Locate + hash-verify concept and plan first.
	concept, err := ParseDoc(ConceptPath(issue))
	if err != nil {
		return nil, exit.New(exit.NotFound, "concept missing: "+ConceptPath(issue))
	}
	if err := VerifyHash(concept, concept.Get("Lock-Hash")); err != nil {
		return nil, err
	}
	plan, err := ParseDoc(PlanPath(issue))
	if err != nil {
		return nil, exit.New(exit.NotFound, "plan missing: "+PlanPath(issue))
	}
	if err := VerifyHash(plan, plan.Get("Lock-Hash")); err != nil {
		return nil, err
	}

	addendaPath, err := FindAddendaFile(issue, nn)
	if err != nil {
		return nil, exit.New(exit.NotFound, fmt.Sprintf("addenda %02d not found in issue-%d", nn, issue))
	}
	addenda, err := ParseDoc(addendaPath)
	if err != nil {
		return nil, exit.New(exit.PreflightFailed, err.Error())
	}
	if addenda.Get("Status") != "Approved" {
		return nil, exit.New(exit.StateConflict,
			fmt.Sprintf("%s has Status %q — apply requires Approved (run: orbit neocortex addenda approve %d)",
				addendaPath, addenda.Get("Status"), nn))
	}
	deltas, err := ParseAddendaDeltas(addenda.Body)
	if err != nil {
		return nil, exit.New(exit.PreflightFailed, err.Error())
	}
	if len(deltas) == 0 {
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("%s: no ADD/REMOVE/MODIFY lines in Plan Changes", addendaPath))
	}

	// Validate deltas against the current effective DAG.
	dagSec := Section(plan.Body, "Task DAG")
	dag, err := ParseDAG(dagSec)
	if err != nil {
		return nil, err
	}
	before := dagLineStrings(dag)
	pos := map[string]int{}
	for i, t := range dag.Tasks {
		pos[t.ID] = i
	}
	for _, d := range deltas {
		switch d.Verb {
		case "ADD":
			if _, ok := pos[d.ID]; ok {
				return nil, exit.New(exit.PreflightFailed,
					fmt.Sprintf("ADD %s refused: already exists in the effective DAG (line: %s)", d.ID, d.Line))
			}
		case "REMOVE", "MODIFY":
			if _, ok := pos[d.ID]; !ok {
				return nil, exit.New(exit.PreflightFailed,
					fmt.Sprintf("%s %s refused: ID does not exist in the effective DAG (line: %s)", d.Verb, d.ID, d.Line))
			}
		}
	}

	// Simulate the resulting DAG.
	sim := &DAG{}
	for _, t := range dag.Tasks {
		clone := *t
		sim.Tasks = append(sim.Tasks, &clone)
	}
	for _, d := range deltas {
		switch d.Verb {
		case "ADD", "MODIFY":
			nt := &Task{ID: d.ID, Name: d.Name, DependsOn: d.Deps, Context: d.Context}
			nt.Line = fmt.Sprintf("- [%s] %s (Blocked by: %s)", d.ID, d.Name, depsString(d.Deps))
			if d.Context != "" {
				nt.Line += " — " + d.Context
			}
			if d.Verb == "MODIFY" {
				*sim.Get(d.ID) = *nt
			} else {
				sim.Tasks = append(sim.Tasks, nt)
			}
		case "REMOVE":
			var kept []*Task
			for _, t := range sim.Tasks {
				if t.ID != d.ID {
					kept = append(kept, t)
				}
			}
			sim.Tasks = kept
		}
	}
	if err := sim.Validate(); err != nil {
		return nil, err
	}
	after := dagLineStrings(sim)

	// Rewrite the Task DAG section lines in place (preserve comments).
	newBody := rewriteDAGSection(plan.Body, sim)
	plan.Body = newBody

	// Append the Amendments line.
	summaryParts := make([]string, 0, len(deltas))
	for _, d := range deltas {
		verb := map[string]string{"ADD": "ADD", "REMOVE": "REM", "MODIFY": "MOD"}[d.Verb]
		summaryParts = append(summaryParts, fmt.Sprintf("%s %s", verb, d.ID))
	}
	summary := strings.Join(summaryParts, ", ")
	amend := fmt.Sprintf("\n- addenda-%02d: \"%s\" — applied %s — %s\n",
		nn, addendaTitle(addenda), time.Now().UTC().Format(time.RFC3339), summary)
	plan.Body = appendToSection(plan.Body, "Amendments", amend)

	// Recompute plan Lock-Hash (Status stays Locked).
	plan.Set("Lock-Hash", LockHash(plan.Body))
	if err := plan.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite plan")
	}

	// Stamp touched task files (REMOVE/MODIFY): append NN to amendments.
	for _, d := range deltas {
		if d.Verb == "ADD" {
			continue // ADD registers intent only; task file is JIT
		}
		tp := TaskPath(issue, d.ID)
		if !fsutil.Exists(tp) {
			continue
		}
		tdoc, err := ParseDoc(tp)
		if err != nil {
			return nil, exit.Wrap(exit.IOError, err, "cannot parse "+tp)
		}
		tdoc.AppendToList("amendments", AddendaFilePrefix(nn))
		if err := tdoc.Save(); err != nil {
			return nil, exit.Wrap(exit.IOError, err, "cannot update "+tp)
		}
	}

	// Mark the addenda Applied.
	addenda.Set("Status", "Applied")
	addenda.Set("Applied-At", time.Now().UTC().Format(time.RFC3339))
	if err := addenda.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+addendaPath)
	}
	logx.Info("addenda applied issue=%d nn=%d summary=%s", issue, nn, summary)

	return &AddendaApplyResult{Before: before, After: after, Summary: summary}, nil
}

// rewriteDAGSection replaces task lines within the Task DAG section per the
// simulated DAG, appending ADDed rows after the last existing row.
func rewriteDAGSection(body []byte, sim *DAG) []byte {
	lines := strings.Split(string(body), "\n")
	inDAG := false
	dagStart := -1
	var out []string
	emitted := map[string]bool{}
	lastDAGLine := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inDAG {
			out = append(out, line)
			if strings.HasPrefix(trimmed, "## Task DAG") {
				inDAG = true
				dagStart = i
			}
			continue
		}
		if isHeading([]byte(line)) && trimmed != "## Task DAG" {
			// End of section: append any new tasks before it.
			for _, t := range sim.Tasks {
				if !emitted[t.ID] {
					out = append(out, t.Line)
				}
			}
			inDAG = false
			out = append(out, line)
			continue
		}
		if m := dagLineRe.FindStringSubmatch(trimmed); m != nil {
			if t := sim.Get(m[1]); t != nil {
				out = append(out, t.Line)
				emitted[t.ID] = true
			} // REMOVEd tasks are dropped.
			lastDAGLine = len(out) - 1
			continue
		}
		out = append(out, line)
	}
	if inDAG {
		for _, t := range sim.Tasks {
			if !emitted[t.ID] {
				out = append(out, t.Line)
			}
		}
	}
	_ = dagStart
	_ = lastDAGLine
	return []byte(strings.Join(out, "\n"))
}

// appendToSection appends text at the end of a named section.
func appendToSection(body []byte, section, text string) []byte {
	lines := strings.Split(string(body), "\n")
	in := false
	end := len(lines)
	for i, line := range lines {
		if in && isHeading([]byte(line)) {
			end = i
			break
		}
		if strings.HasPrefix(strings.TrimSpace(line), "## "+section) {
			in = true
		}
	}
	out := append([]string{}, lines[:end]...)
	out = append(out, strings.TrimSuffix(text, "\n"))
	out = append(out, lines[end:]...)
	return []byte(strings.Join(out, "\n"))
}

func addendaTitle(doc *Doc) string {
	if h1 := firstH1(doc.Body); h1 != "" {
		return strings.TrimSpace(strings.TrimPrefix(h1, fmt.Sprintf("Addenda %s:", doc.Get("Addenda"))))
	}
	return ""
}

func dagLineStrings(d *DAG) []string {
	out := make([]string, len(d.Tasks))
	for i, t := range d.Tasks {
		out[i] = t.Line
	}
	return out
}
