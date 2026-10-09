package neocortex

import (
	"fmt"
	"strings"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
)

// CloseReport is the verified completion summary for a default-lane issue.
type CloseReport struct {
	Issue   int        `json:"issue"`
	Title   string     `json:"title"`
	Concept string     `json:"concept_status"`
	Plan    string     `json:"plan_status"`
	Tasks   []TaskInfo `json:"tasks"`
}

// BuildCloseReport verifies a default-lane issue is ready to close: the plan
// is Locked and every task in the effective DAG is Close. Quick runs are
// rejected — they close through the quick lane. Read-only.
func BuildCloseReport(n int) (*CloseReport, error) {
	if fsutil.Exists(QuickPath(n)) {
		return nil, exit.New(exit.StateConflict,
			fmt.Sprintf("issue %d is a quick run — close it with: orbit neocortex quick close %d", n, n))
	}
	concept, err := doc.ParseDoc(ConceptPath(n))
	if err != nil {
		return nil, exit.New(exit.NotFound, "concept missing: "+ConceptPath(n))
	}
	plan, err := doc.ParseDoc(PlanPath(n))
	if err != nil {
		return nil, exit.New(exit.NotFound, "plan missing: "+PlanPath(n))
	}
	if plan.Get("Status") != string(PlanLocked) {
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("plan is not Locked (Status: %s) — close requires a locked plan", plan.Get("Status")),
			"lock the plan first: orbit neocortex plan lock")
	}

	rows, err := ListTasks(n)
	if err != nil {
		return nil, err
	}
	var open []string
	for _, r := range rows {
		if r.Status != string(StatusClose) {
			open = append(open, fmt.Sprintf("%s (%s)", r.ID, r.Status))
		}
	}
	if len(open) > 0 {
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("issue %d has %d task(s) not Close: %s", n, len(open), strings.Join(open, ", ")),
			"close every task first; rework any that still need work")
	}

	title := strings.TrimPrefix(firstH1(concept.Body), "Concept: ")
	return &CloseReport{
		Issue:   n,
		Title:   strings.TrimSpace(title),
		Concept: concept.Get("Status"),
		Plan:    plan.Get("Status"),
		Tasks:   rows,
	}, nil
}
