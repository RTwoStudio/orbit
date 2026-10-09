package cycles

import (
	"fmt"
	"strings"

	"github.com/RTwoStudio/orbit/internal/exit"
)

// WorkStatus is the canonical work-item status enum. The stored file value is
// the canonical string itself (Backlog, Pitched, Bet, Shelved, Delivered).
type WorkStatus string

const (
	WorkBacklog   WorkStatus = "Backlog"
	WorkPitched   WorkStatus = "Pitched"
	WorkBet       WorkStatus = "Bet"
	WorkShelved   WorkStatus = "Shelved"
	WorkDelivered WorkStatus = "Delivered"
)

// FileValue returns the YAML/CLI spelling of a status (identical to the
// canonical value).
func (s WorkStatus) FileValue() string { return string(s) }

// ParseWorkStatus normalizes case-insensitive CLI input to a canonical status.
// Unknown values are a preflight_failed (6).
func ParseWorkStatus(input string) (WorkStatus, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "backlog":
		return WorkBacklog, nil
	case "pitched":
		return WorkPitched, nil
	case "bet":
		return WorkBet, nil
	case "shelved":
		return WorkShelved, nil
	case "delivered":
		return WorkDelivered, nil
	}
	return "", exit.New(exit.PreflightFailed,
		fmt.Sprintf("unknown work status %q — valid: Backlog, Pitched, Bet, Shelved, Delivered", input))
}

// workStatusFromFile maps a file's status string to canonical. It is lenient
// (case-insensitive) and returns "" for anything unrecognized.
func workStatusFromFile(s string) WorkStatus {
	ws, err := ParseWorkStatus(s)
	if err != nil {
		return ""
	}
	return ws
}

// workTransitions is the only legal edge set (approved table).
//
//	Backlog  → Pitched, Shelved
//	Pitched  → Bet, Shelved
//	Bet      → Delivered, Shelved
//	Shelved  → Pitched
//	Delivered → (terminal)
var workTransitions = map[WorkStatus][]WorkStatus{
	WorkBacklog:   {WorkPitched, WorkShelved},
	WorkPitched:   {WorkBet, WorkShelved},
	WorkBet:       {WorkDelivered, WorkShelved},
	WorkShelved:   {WorkPitched},
	WorkDelivered: {},
}

// CheckWorkTransition validates from→to; an illegal edge is a state_conflict
// (7) listing the valid targets from `from`.
func CheckWorkTransition(from, to WorkStatus) error {
	for _, t := range workTransitions[from] {
		if t == to {
			return nil
		}
	}
	var valid []string
	for _, t := range workTransitions[from] {
		valid = append(valid, t.FileValue())
	}
	list := strings.Join(valid, ", ")
	if list == "" {
		list = "none (terminal)"
	}
	return exit.New(exit.StateConflict,
		fmt.Sprintf("illegal transition %s → %s (valid from %s: %s)",
			from.FileValue(), to.FileValue(), from.FileValue(), list))
}

// CycleStatus is the canonical cycle-status enum. The stored file value is the
// canonical string itself (Open, Closed).
type CycleStatus string

const (
	CycleOpen   CycleStatus = "Open"
	CycleClosed CycleStatus = "Closed"
)

// FileValue returns the YAML/CLI spelling of a cycle status.
func (s CycleStatus) FileValue() string { return string(s) }

// ParseCycleStatus normalizes case-insensitive CLI input to a canonical cycle
// status. Unknown values are a preflight_failed (6).
func ParseCycleStatus(input string) (CycleStatus, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "open":
		return CycleOpen, nil
	case "closed":
		return CycleClosed, nil
	}
	return "", exit.New(exit.PreflightFailed,
		fmt.Sprintf("unknown cycle status %q — valid: Open, Closed", input))
}

// cycleStatusFromFile maps a file's status string to canonical. It is lenient
// (case-insensitive) and returns "" for anything unrecognized.
func cycleStatusFromFile(s string) CycleStatus {
	cs, err := ParseCycleStatus(s)
	if err != nil {
		return ""
	}
	return cs
}

// cycleTransitions is the only legal edge set: a cycle opens once and closes
// once.
//
//	Open   → Closed
//	Closed → (terminal)
var cycleTransitions = map[CycleStatus][]CycleStatus{
	CycleOpen:   {CycleClosed},
	CycleClosed: {},
}

// CheckCycleTransition validates from→to; an illegal edge is a state_conflict
// (7) listing the valid targets from `from`.
func CheckCycleTransition(from, to CycleStatus) error {
	for _, t := range cycleTransitions[from] {
		if t == to {
			return nil
		}
	}
	var valid []string
	for _, t := range cycleTransitions[from] {
		valid = append(valid, t.FileValue())
	}
	list := strings.Join(valid, ", ")
	if list == "" {
		list = "none (terminal)"
	}
	return exit.New(exit.StateConflict,
		fmt.Sprintf("illegal transition %s → %s (valid from %s: %s)",
			from.FileValue(), to.FileValue(), from.FileValue(), list))
}
