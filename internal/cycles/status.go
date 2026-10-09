package cycles

import (
	"errors"

	"github.com/RTwoStudio/orbit/internal/exit"
)

// BacklogLadder groups the work items that are not committed to a cycle, by
// status. Bet/Delivered items live in a cycle and are carried by
// StatusBoard.Current instead.
type BacklogLadder struct {
	Backlog []WorkItem `json:"backlog"`
	Pitched []WorkItem `json:"pitched"`
	Shelved []WorkItem `json:"shelved"`
}

// StatusBoard is the `orbit cycles status` read shape: the open cycle (nil when
// none) plus the backlog ladder. Its json tags are the contract reuse by T6's
// `--json`.
type StatusBoard struct {
	Current *Cycle        `json:"current"`
	Backlog BacklogLadder `json:"backlog"`
}

// Status assembles the status board. Read-only: it never mutates the vault.
// A missing or empty CURRENT pointer yields a nil Current (not an error).
func (s *Store) Status() (*StatusBoard, error) {
	board := &StatusBoard{Backlog: BacklogLadder{
		Backlog: []WorkItem{},
		Pitched: []WorkItem{},
		Shelved: []WorkItem{},
	}}

	if id, err := s.ReadCurrent(); err == nil {
		cy, err := s.ShowCycle(id)
		if err != nil {
			return nil, err
		}
		board.Current = cy
	} else {
		var e *exit.Error
		if !errors.As(err, &e) || e.Code != exit.StateConflict {
			return nil, err
		}
	}

	items, err := s.ListWork("", "")
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		switch workStatusFromFile(it.Status) {
		case WorkBacklog:
			board.Backlog.Backlog = append(board.Backlog.Backlog, it)
		case WorkPitched:
			board.Backlog.Pitched = append(board.Backlog.Pitched, it)
		case WorkShelved:
			board.Backlog.Shelved = append(board.Backlog.Shelved, it)
		}
	}
	return board, nil
}
