package cycles

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusBoardWithCurrent(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewCycle("Board", "0.1.0", "", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := s.NewWork("Alpha", "scope-a"); err != nil { // W-0001 Backlog
		t.Fatal(err)
	}
	mustWork(t, s, "Beta", "scope-a", false) // W-0002 Pitched
	it3 := mustWork(t, s, "Gamma", "scope-a", false)
	if _, err := s.ShelveWork(it3.ID); err != nil { // W-0003 Shelved
		t.Fatal(err)
	}
	mustWork(t, s, "Delta", "scope-a", true) // W-0004 Bet

	board, err := s.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if board.Current == nil || board.Current.ID != "C-0001" {
		t.Fatalf("Current = %+v, want C-0001", board.Current)
	}
	if len(board.Current.Bets) != 1 || board.Current.Bets[0].ID != "W-0004" {
		t.Errorf("Current.Bets = %+v, want [W-0004]", board.Current.Bets)
	}
	if len(board.Backlog.Backlog) != 1 || board.Backlog.Backlog[0].ID != "W-0001" {
		t.Errorf("Backlog = %+v, want [W-0001]", board.Backlog.Backlog)
	}
	if len(board.Backlog.Pitched) != 1 || board.Backlog.Pitched[0].ID != "W-0002" {
		t.Errorf("Pitched = %+v, want [W-0002]", board.Backlog.Pitched)
	}
	if len(board.Backlog.Shelved) != 1 || board.Backlog.Shelved[0].ID != "W-0003" {
		t.Errorf("Shelved = %+v, want [W-0003]", board.Backlog.Shelved)
	}
}

func TestStatusBoardNoCurrent(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewWork("Alpha", "scope-a"); err != nil {
		t.Fatal(err)
	}
	board, err := s.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if board.Current != nil {
		t.Errorf("Current = %+v, want nil when no cycle is open", board.Current)
	}
	if len(board.Backlog.Backlog) != 1 {
		t.Errorf("Backlog len = %d, want 1", len(board.Backlog.Backlog))
	}
}

func TestStatusBoardJSONShape(t *testing.T) {
	s := setupVault(t)
	if _, err := s.NewWork("Alpha", "scope-a"); err != nil {
		t.Fatal(err)
	}
	board, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(board)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"current"`, `"backlog"`, `"pitched"`, `"shelved"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("marshalled StatusBoard missing %s: %s", key, data)
		}
	}
	if !strings.Contains(string(data), `"current":null`) {
		t.Errorf("StatusBoard current should marshal as null when empty: %s", data)
	}
}
