package neocortex

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RTwoStudio/orbit-cli/internal/exit"
	"github.com/RTwoStudio/orbit-cli/internal/logx"
)

// LockPlan performs plan lock (§5.8): concept hash verify FIRST (tamper
// check), then plan preflights, then locked-at/hash/status.
func LockPlan(n int) (*DAG, error) {
	// Verify-first: hash-verify the concept before anything else.
	concept, err := ParseDoc(ConceptPath(n))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, exit.New(exit.NotFound, fmt.Sprintf("concept file missing: %s", ConceptPath(n)))
		}
		return nil, exit.New(exit.PreflightFailed, err.Error())
	}
	if err := VerifyHash(concept, concept.Get("Lock-Hash")); err != nil {
		return nil, err
	}

	// Plan preflights.
	planPath := PlanPath(n)
	doc, err := ParseDoc(planPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, exit.New(exit.NotFound, fmt.Sprintf("plan file missing: %s", planPath))
		}
		return nil, exit.New(exit.PreflightFailed, err.Error())
	}
	switch doc.Get("Status") {
	case string(PlanLocked):
		return nil, exit.New(exit.StateConflict, fmt.Sprintf("%s is already Locked — lock is one-way", planPath))
	case string(PlanDraft):
	default:
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("%s has Status %q (must be %q)", planPath, doc.Get("Status"), PlanDraft))
	}
	if err := GuardBody(planPath, doc.Body); err != nil {
		return nil, err
	}

	// Open Questions must have no unticked checkboxes.
	oq := Section(doc.Body, "Open Questions")
	if oq != nil {
		for _, line := range strings.Split(string(oq), "\n") {
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "- [ ]") {
				return nil, exit.New(exit.PreflightFailed,
					fmt.Sprintf("%s: open question remains before lock:\n  %s", planPath, t),
					"resolve the question, move it to Architectural Decisions, then re-run")
			}
		}
	}

	// Task DAG must parse.
	dagSec := Section(doc.Body, "Task DAG")
	if dagSec == nil {
		return nil, exit.New(exit.PreflightFailed, fmt.Sprintf("%s has no '## Task DAG' section", planPath))
	}
	dag, err := ParseDAG(dagSec)
	if err != nil {
		return nil, err
	}

	// Actions.
	hash := LockHash(doc.Body)
	doc.Set("Status", string(PlanLocked))
	doc.Set("Locked-At", time.Now().UTC().Format(time.RFC3339))
	doc.Set("Lock-Hash", hash)
	if err := doc.Save(); err != nil {
		return nil, exit.Wrap(exit.IOError, err, "cannot rewrite "+planPath)
	}
	logx.Info("plan lock | issue=%d | tasks=%d | locked hash=%s", n, len(dag.Tasks), hash[:19]+"…")
	return dag, nil
}

// EffectiveDAG parses the plan's Task DAG section (as amended).
func EffectiveDAG(planDoc *Doc) (*DAG, error) {
	dagSec := Section(planDoc.Body, "Task DAG")
	if dagSec == nil {
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("%s has no '## Task DAG' section", planDoc.Path))
	}
	return ParseDAG(dagSec)
}

// Amendment is one parsed Amendments line.
type Amendment struct {
	NN      int
	Title   string
	Date    string
	Summary string
}

var amendmentRe = regexpAmendment()

// ParseAmendments extracts all Amendments lines in order.
func ParseAmendments(body []byte) []Amendment {
	sec := Section(body, "Amendments")
	if sec == nil {
		return nil
	}
	var out []Amendment
	for _, line := range strings.Split(string(sec), "\n") {
		if m := amendmentRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			var nn int
			fmt.Sscanf(m[1], "%d", &nn)
			out = append(out, Amendment{NN: nn, Title: m[2], Date: m[3], Summary: m[4]})
		}
	}
	return out
}
