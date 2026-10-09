package scaffold

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidateStubAcceptsKnownTokens(t *testing.T) {
	cases := map[string]string{
		"00-concept.stub.md": "{{ISSUE_ID}} {{ISSUE_TITLE}} {{DATE}} {{SOURCE}} {{REGISTRY_VERSION}} {{DETAIL}}",
		"00-quick.stub.md":   "{{ISSUE_ID}} {{ISSUE_TITLE}} {{DATE}} {{SOURCE}} {{REGISTRY_VERSION}} {{DETAIL}}",
		"01-plan.stub.md":    "{{ISSUE_ID}} {{ISSUE_TITLE}} {{DATE}} {{REGISTRY_VERSION}}",
		"addenda.stub.md":    "{{ADDENDA_NUM}} {{ADDENDA_TITLE}} {{ISSUE_ID}} {{DATE}} {{REGISTRY_VERSION}}",
		"task.stub.md":       "{{TASK_ID}} {{TASK_NAME}} {{ISSUE_ID}} {{DEPENDS_ON}} {{ORIGIN}} {{DATE}} {{REGISTRY_VERSION}} {{BLOCKED_BY}} {{BLOCKS}}",
	}
	for name, content := range cases {
		if err := ValidateStub(name, []byte(content)); err != nil {
			t.Errorf("ValidateStub(%s) = %v, want nil", name, err)
		}
	}
}

func TestValidateStubRejects(t *testing.T) {
	if err := ValidateStub("task.stub.md", []byte("{{NOPE}}")); err == nil {
		t.Error("expected unknown-token error, got nil")
	} else if !strings.Contains(err.Error(), "unknown token") {
		t.Errorf("error = %q, want 'unknown token'", err.Error())
	}
	if err := ValidateStub("mystery.stub.md", []byte("{{TASK_ID}}")); err == nil {
		t.Error("expected unknown-stub error, got nil")
	} else if !strings.Contains(err.Error(), "unknown stub") {
		t.Errorf("error = %q, want 'unknown stub'", err.Error())
	}
}

func TestRenderSuccess(t *testing.T) {
	out, err := Render("hi {{TASK_ID}}", map[string]string{"TASK_ID": "T1"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hi T1" {
		t.Errorf("Render = %q, want %q", string(out), "hi T1")
	}
}

func TestRenderUnknownToken(t *testing.T) {
	_, err := Render("{{NOPE}}", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown token") {
		t.Fatalf("expected unknown-token error, got %v", err)
	}
}

func TestRenderUnresolvedToken(t *testing.T) {
	_, err := Render("{{TASK_ID}} {{ISSUE_ID}}", map[string]string{"TASK_ID": "T1"})
	if err == nil || !strings.Contains(err.Error(), "unresolved token") {
		t.Fatalf("expected unresolved-token error, got %v", err)
	}
}

func TestRegisterExtendsRegistry(t *testing.T) {
	Register("work.stub.md", []string{"WORK_ID", "SCOPE"})
	if err := ValidateStub("work.stub.md", []byte("{{WORK_ID}} {{SCOPE}}")); err != nil {
		t.Errorf("registered stub rejected: %v", err)
	}
	if err := ValidateStub("work.stub.md", []byte("{{TASK_ID}}")); err == nil {
		t.Error("expected registered stub to reject unregistered token")
	}
	out, err := Render("{{WORK_ID}}", map[string]string{"WORK_ID": "W-0001"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "W-0001" {
		t.Errorf("Render = %q, want W-0001", string(out))
	}
}

func TestSortKeys(t *testing.T) {
	got := SortKeys(map[string]string{"b": "1", "a": "2", "c": "3"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SortKeys = %v, want %v", got, want)
	}
}
