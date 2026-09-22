package neocortex

import (
	"regexp"
	"strings"
	"testing"
)

func TestRenderSubstitution(t *testing.T) {
	out, err := Render("# {{ISSUE_TITLE}} id={{ISSUE_ID}}", map[string]string{
		"ISSUE_TITLE": "Hello", "ISSUE_ID": "7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "# Hello id=7" {
		t.Errorf("got %q", out)
	}
}

func TestRenderRefusesUnknownToken(t *testing.T) {
	if _, err := Render("# {{NOT_A_TOKEN}}", map[string]string{}); err == nil {
		t.Fatal("unknown token should error")
	}
}

func TestRenderRefusesUnresolved(t *testing.T) {
	if _, err := Render("{{ISSUE_ID}} {{DATE}}", map[string]string{"ISSUE_ID": "1"}); err == nil {
		t.Fatal("unresolved token should error, not render silently")
	}
}

func TestValidateStubKnownSets(t *testing.T) {
	if err := ValidateStub("01-plan.stub.md", []byte("{{ISSUE_ID}} {{ISSUE_TITLE}} {{DATE}} {{REGISTRY_VERSION}}")); err != nil {
		t.Errorf("plan stub tokens should validate: %v", err)
	}
	if err := ValidateStub("01-plan.stub.md", []byte("{{DETAIL}}")); err == nil {
		t.Error("DETAIL does not belong in plan stub")
	}
}

func TestLockHashStability(t *testing.T) {
	a := LockHash([]byte("body\nline2\n"))
	b := LockHash([]byte("body\nline2\n"))
	if a != b {
		t.Error("same body must hash identically")
	}
	c := LockHash([]byte("body\nline2 "))
	if a == c {
		t.Error("whitespace change must change hash")
	}
	if !strings.HasPrefix(a, "sha256:") {
		t.Errorf("hash prefix: %q", a)
	}
}

func TestParseDocRoundTrip(t *testing.T) {
	src := "---\nIssue-ID: 7\nStatus: Draft\nLocked-At:\nLock-Hash:\n---\n\n# Title\n\nbody line\n"
	doc, err := ParseDocBytes("t.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != src {
		t.Errorf("round-trip mismatch:\n got %q\nwant %q", out, src)
	}
}

func TestParseDocRejectsCRLF(t *testing.T) {
	_, err := ParseDocBytes("t.md", []byte("---\r\nStatus: Draft\r\n---\r\nbody\r\n"))
	if err == nil || !strings.Contains(err.Error(), "CRLF") {
		t.Fatalf("CRLF must be rejected with clear error, got: %v", err)
	}
}

func TestDocGetSet(t *testing.T) {
	doc, _ := ParseDocBytes("t.md", []byte("---\nStatus: Draft\nIssue-ID: 7\n---\nbody\n"))
	if doc.Get("Status") != "Draft" {
		t.Error("Get failed")
	}
	doc.Set("Status", "Locked")
	if doc.Get("Status") != "Locked" {
		t.Error("Set failed")
	}
	doc.AppendToList("amendments", "01")
	doc.AppendToList("amendments", "02")
	got := doc.GetStringList("amendments")
	if len(got) != 2 || got[0] != "01" || got[1] != "02" {
		t.Errorf("list append failed: %v", got)
	}
}

func TestSectionAndBulletForm(t *testing.T) {
	body := []byte("## Open Questions\n\n- [ ] q1\n\n## Task DAG\n\n- [T1] A (Blocked by: None)\n")
	sec := Section(body, "Open Questions")
	if string(sec) != "\n- [ ] q1\n" {
		t.Errorf("section = %q", sec)
	}
	dag := Section(body, "Task DAG")
	if !strings.Contains(string(dag), "- [T1] A") {
		t.Errorf("dag section = %q", dag)
	}
	b := []byte("- **Completion Notes:**\n    implemented stuff\n\n- **Verification:**\n  - [x] v1\n")
	if got := StripHTMLComments(BulletFormSection(b, "Completion Notes")); got != "implemented stuff" {
		t.Errorf("completion notes = %q", got)
	}
}

func TestSanitizeSlug(t *testing.T) {
	cases := map[string]string{
		"Hello World":        "hello-world",
		"A  Weird!! Title--": "a-weird-title",
		"Ünïcode Tïtlé":      "n-code-t-tl",
	}
	for in, want := range cases {
		got := SanitizeSlug(in)
		if got != want {
			t.Errorf("SanitizeSlug(%q) = %q, want %q", in, got, want)
		}
	}
	long := SanitizeSlug(strings.Repeat("a", 100))
	if len(long) > 40 {
		t.Errorf("slug too long: %d", len(long))
	}
}

func TestAgentPlaceholderGuard(t *testing.T) {
	if err := GuardBody("f.md", []byte("ok body")); err != nil {
		t.Errorf("clean body should pass: %v", err)
	}
	if err := GuardBody("f.md", []byte("text\n<!-- Agent: fill me -->\n")); err == nil {
		t.Error("agent placeholder must fail guard")
	}
	if err := GuardBody("f.md", []byte("{{ISSUE_ID}}")); err == nil {
		t.Error("token must fail guard")
	}
	var dagLine = regexp.MustCompile(`^- \[(T[0-9]+)\]`)
	if !dagLine.MatchString("- [T1] x (Blocked by: None)") {
		t.Error("sanity: regex should match")
	}
}
