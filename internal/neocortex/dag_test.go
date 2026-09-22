package neocortex

import (
	"strings"
	"testing"
)

func TestDAGCorpusValid(t *testing.T) {
	text := `
- [T1] Seed schema (Blocked by: None)
- [T2] Parser (Blocked by: T1) — uses T1
- [T3] Client (Blocked by: T1, T2)
`
	dag, err := ParseDAG([]byte(text))
	if err != nil {
		t.Fatalf("valid corpus refused: %v", err)
	}
	if len(dag.Tasks) != 3 {
		t.Fatalf("tasks = %d", len(dag.Tasks))
	}
	if got := dag.Tasks[2].DependsOn; len(got) != 2 || got[0] != "T1" || got[1] != "T2" {
		t.Errorf("deps = %v", got)
	}
	if dag.Tasks[1].Context != "uses T1" {
		t.Errorf("context = %q", dag.Tasks[1].Context)
	}
	if dag.BlocksOf("T1")[0] != "T2" {
		t.Errorf("blocks inverse broken")
	}
}

func TestDAGCorpusBadGrammar(t *testing.T) {
	_, err := ParseDAG([]byte("- [T1] Missing parens"))
	if err == nil || !strings.Contains(err.Error(), "does not match grammar") {
		t.Errorf("bad grammar accepted: %v", err)
	}
}

func TestDAGCorpusDuplicateID(t *testing.T) {
	_, err := ParseDAG([]byte("- [T1] A (Blocked by: None)\n- [T1] B (Blocked by: None)"))
	if err == nil || !strings.Contains(err.Error(), "duplicate ID") {
		t.Errorf("duplicate accepted: %v", err)
	}
}

func TestDAGCorpusSelfDep(t *testing.T) {
	_, err := ParseDAG([]byte("- [T1] A (Blocked by: T1)"))
	if err == nil || !strings.Contains(err.Error(), "self-dependency") {
		t.Errorf("self-dep accepted: %v", err)
	}
}

func TestDAGCorpusForwardRef(t *testing.T) {
	// T2 depends on T1 but appears BEFORE T1 → not topologically valid.
	_, err := ParseDAG([]byte("- [T2] B (Blocked by: T1)\n- [T1] A (Blocked by: None)"))
	if err == nil || !strings.Contains(err.Error(), "topologically") {
		t.Errorf("forward ref accepted: %v", err)
	}
}

func TestDAGCorpusMissingDep(t *testing.T) {
	_, err := ParseDAG([]byte("- [T1] A (Blocked by: T9)"))
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing dep accepted: %v", err)
	}
}

func TestDAGCorpusEmpty(t *testing.T) {
	_, err := ParseDAG([]byte("<!-- only comments -->"))
	if err == nil || !strings.Contains(err.Error(), "no tasks") {
		t.Errorf("empty accepted: %v", err)
	}
}

func TestDAGCommentLinesIgnored(t *testing.T) {
	bq := "`"
	text := "<!-- Agent: One task per line.\n" +
		"- " + bq + "- [T1] Task name (Blocked by: None)" + bq + "\n" +
		"- " + bq + "- [T2] Task name (Blocked by: T1) — related: T5 controller, T1 seeds" + bq + "\n" +
		"-->\n" +
		"- [T1] Real task (Blocked by: None)\n"
	dag, err := ParseDAG([]byte(text))
	if err != nil {
		t.Fatalf("comment examples must be ignored: %v", err)
	}
	if len(dag.Tasks) != 1 || dag.Tasks[0].ID != "T1" {
		t.Errorf("parsed tasks: %+v", dag.Tasks)
	}
}

func TestTransitionTable(t *testing.T) {
	legal := map[string]string{
		"Open": "In Progress", "In Progress": "Revise",
		"Revise": "Rework", "Rework": "In Progress",
	}
	for from, to := range legal {
		if err := CheckTransition(statusFromFile(from), statusFromFile(to)); err != nil {
			t.Errorf("%s → %s should be legal: %v", from, to, err)
		}
	}
	illegal := [][2]string{
		{"Open", "Close"}, {"Open", "Revise"}, {"Close", "Open"},
		{"Rework", "Close"}, {"In Progress", "Close"}, {"Revise", "Open"},
	}
	for _, p := range illegal {
		if err := CheckTransition(statusFromFile(p[0]), statusFromFile(p[1])); err == nil {
			t.Errorf("%s → %s should be illegal", p[0], p[1])
		}
	}
}

func TestParseTaskStatusNormalization(t *testing.T) {
	cases := map[string]TaskStatus{
		"Open": StatusOpen, "open": StatusOpen, "OPEN": StatusOpen,
		"in-progress": StatusInProgress, "In Progress": StatusInProgress,
		"INPROGRESS": StatusInProgress, "in_progress": StatusInProgress,
		"Revise": StatusRevise, "rework": StatusRework, "close": StatusClose,
		"Closed": StatusClose,
	}
	for in, want := range cases {
		got, err := ParseTaskStatus(in)
		if err != nil || got != want {
			t.Errorf("ParseTaskStatus(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseTaskStatus("done"); err == nil {
		t.Error("unknown status should error")
	}
}
