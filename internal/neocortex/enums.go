package neocortex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/orbit-sh/orbit-cli/internal/exit"
)

// Status enums — single definition (§7). Canonical values are stored in
// files; CLI input is case-insensitive and normalized.
type ConceptStatus string // "Draft", "Locked"
type PlanStatus string    // "Draft", "Locked"
type AddendaStatus string // "Draft", "Approved", "Applied"
type TaskStatus string    // canonical: Open, InProgress, Revise, Rework, Close

const (
	ConceptDraft  ConceptStatus = "Draft"
	ConceptLocked ConceptStatus = "Locked"

	PlanDraft  PlanStatus = "Draft"
	PlanLocked PlanStatus = "Locked"

	AddendaDraft    AddendaStatus = "Draft"
	AddendaApproved AddendaStatus = "Approved"
	AddendaApplied  AddendaStatus = "Applied"

	StatusOpen       TaskStatus = "Open"
	StatusInProgress TaskStatus = "InProgress"
	StatusRevise     TaskStatus = "Revise"
	StatusRework     TaskStatus = "Rework"
	StatusClose      TaskStatus = "Close"
)

// FileValue maps the internal task status to the human-readable YAML value
// (same string used in files and CLI output).
func (s TaskStatus) FileValue() string {
	switch s {
	case StatusInProgress:
		return "In Progress"
	case StatusRevise:
		return "Revise"
	case StatusRework:
		return "Rework"
	case StatusClose:
		return "Close"
	}
	return "Open"
}

// taskTransitions is the only legal edge set.
var taskTransitions = map[TaskStatus][]TaskStatus{
	StatusOpen:       {StatusInProgress},
	StatusInProgress: {StatusRevise},
	StatusRevise:     {StatusRework, StatusClose},
	StatusRework:     {StatusInProgress},
	StatusClose:      {}, // terminal
}

// ParseTaskStatus normalizes case-insensitive input ("in-progress",
// "In Progress", "INPROGRESS") to the canonical value.
func ParseTaskStatus(input string) (TaskStatus, error) {
	norm := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(input), "_", "-"))
	norm = strings.ReplaceAll(norm, " ", "-")
	switch norm {
	case "open":
		return StatusOpen, nil
	case "in-progress", "inprogress", "inprogress-", "started":
		return StatusInProgress, nil
	case "revise", "revision":
		return StatusRevise, nil
	case "rework":
		return StatusRework, nil
	case "close", "closed":
		return StatusClose, nil
	}
	return "", exit.New(exit.PreflightFailed,
		fmt.Sprintf("unknown task status %q — valid: Open, In Progress, Revise, Rework, Close", input))
}

// CheckTransition validates from→to; returns a state_conflict error listing
// valid transitions when illegal.
func CheckTransition(from, to TaskStatus) error {
	for _, t := range taskTransitions[from] {
		if t == to {
			return nil
		}
	}
	var valid []string
	for _, t := range taskTransitions[from] {
		valid = append(valid, t.FileValue())
	}
	return exit.New(exit.StateConflict,
		fmt.Sprintf("illegal transition %s → %s (valid from %s: %s)",
			from.FileValue(), to.FileValue(), from.FileValue(), strings.Join(valid, ", ")))
}

// SortTaskIDs orders T-ids numerically.
func SortTaskIDs(ids []string) {
	sort.Slice(ids, func(i, j int) bool {
		var a, b int
		fmt.Sscanf(ids[i], "T%d", &a)
		fmt.Sscanf(ids[j], "T%d", &b)
		return a < b
	})
}
