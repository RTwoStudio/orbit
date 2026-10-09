package cycles

import (
	"testing"

	"github.com/RTwoStudio/orbit/internal/exit"
)

func TestParseWorkStatus(t *testing.T) {
	cases := map[string]WorkStatus{
		"Backlog":   WorkBacklog,
		"backlog":   WorkBacklog,
		"PITCHED":   WorkPitched,
		"pitched":   WorkPitched,
		"Bet":       WorkBet,
		"bet":       WorkBet,
		"Shelved":   WorkShelved,
		"delivered": WorkDelivered,
		"  Bet  ":   WorkBet,
	}
	for in, want := range cases {
		got, err := ParseWorkStatus(in)
		if err != nil {
			t.Errorf("ParseWorkStatus(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseWorkStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseWorkStatusUnknown(t *testing.T) {
	_, err := ParseWorkStatus("bogus")
	if codeOf(t, err) != exit.PreflightFailed {
		t.Errorf("code = %v, want preflight_failed", codeOf(t, err))
	}
}

func TestWorkStatusFileValue(t *testing.T) {
	for _, ws := range []WorkStatus{WorkBacklog, WorkPitched, WorkBet, WorkShelved, WorkDelivered} {
		if got, want := ws.FileValue(), string(ws); got != want {
			t.Errorf("FileValue(%q) = %q", ws, got)
		}
	}
}

func TestWorkTransitionMatrix(t *testing.T) {
	allowed := map[[2]WorkStatus]bool{
		{WorkBacklog, WorkPitched}: true,
		{WorkBacklog, WorkShelved}: true,
		{WorkPitched, WorkBet}:     true,
		{WorkPitched, WorkShelved}: true,
		{WorkBet, WorkDelivered}:   true,
		{WorkBet, WorkShelved}:     true,
		{WorkShelved, WorkPitched}: true,
	}
	all := []WorkStatus{WorkBacklog, WorkPitched, WorkBet, WorkShelved, WorkDelivered}
	for _, from := range all {
		for _, to := range all {
			err := CheckWorkTransition(from, to)
			want := allowed[[2]WorkStatus{from, to}]
			switch {
			case want && err != nil:
				t.Errorf("CheckWorkTransition(%s, %s) = %v, want nil", from, to, err)
			case !want && err == nil:
				t.Errorf("CheckWorkTransition(%s, %s) = nil, want state_conflict", from, to)
			case !want && err != nil && codeOf(t, err) != exit.StateConflict:
				t.Errorf("CheckWorkTransition(%s, %s) code = %v, want state_conflict", from, to, codeOf(t, err))
			}
		}
	}
}

func TestWorkTransitionRefusesBacklogBet(t *testing.T) {
	err := CheckWorkTransition(WorkBacklog, WorkBet)
	if codeOf(t, err) != exit.StateConflict {
		t.Fatalf("Backlog → Bet code = %v, want state_conflict", codeOf(t, err))
	}
}
