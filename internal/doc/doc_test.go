package doc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestParseDocBytesRoundTrip pins byte-exact re-rendering: parsing then
// Bytes() must reproduce the input for canonical frontmatter.
func TestParseDocBytesRoundTrip(t *testing.T) {
	cases := []string{
		"---\nIssue-ID: 1\nStatus: Draft\n---\n# Body\n",
		"---\nIssue-ID: 7\nTitle: Some title\nTags:\n  - a\n  - b\nLock-Hash: sha256:abc\n---\nline one\n\nline two\n",
		"---\nStatus: Locked\ncount: 3\nflag: true\n---\n",
	}
	for i, src := range cases {
		d, err := ParseDocBytes("mem.md", []byte(src))
		if err != nil {
			t.Fatalf("case %d: parse: %v", i, err)
		}
		out, err := d.Bytes()
		if err != nil {
			t.Fatalf("case %d: bytes: %v", i, err)
		}
		if string(out) != src {
			t.Errorf("case %d: round-trip mismatch\n got: %q\nwant: %q", i, string(out), src)
		}
	}
}

func TestParseDocBytesErrors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"crlf", "---\r\nA: 1\r\n---\r\nbody\n", "CRLF"},
		{"no-prefix", "A: 1\n---\nbody\n", "must start with '---'"},
		{"no-close", "---\nA: 1\nbody\n", "closing '---' not found"},
		{"bad-yaml", "---\n: : :\n---\nbody\n", "invalid frontmatter YAML"},
		{"not-mapping", "---\n- a\n- b\n---\nbody\n", "must be a YAML mapping"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDocBytes("mem.md", []byte(tc.in))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if _, ok := err.(*FrontmatterError); !ok {
				t.Fatalf("expected *FrontmatterError, got %T", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestGetSetAppend(t *testing.T) {
	src := "---\nIssue-ID: 1\nTags:\n  - a\nNote: old\n---\nbody\n"
	d, err := ParseDocBytes("mem.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Get("Issue-ID"); got != "1" {
		t.Errorf("Get Issue-ID = %q, want 1", got)
	}
	if got := d.Get("Missing"); got != "" {
		t.Errorf("Get Missing = %q, want empty", got)
	}
	if got := d.GetStringList("Tags"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("GetStringList Tags = %v, want [a]", got)
	}
	if got := d.GetStringList("Note"); got != nil {
		t.Errorf("GetStringList on scalar = %v, want nil", got)
	}
	if got := d.GetStringList("Absent"); got != nil {
		t.Errorf("GetStringList absent = %v, want nil", got)
	}

	d.Set("Note", "new")
	d.Set("Added", "x")
	d.AppendToList("Tags", "b")
	d.AppendToList("Fresh", "y")

	out, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	d2, err := ParseDocBytes("mem.md", out)
	if err != nil {
		t.Fatal(err)
	}
	if got := d2.Get("Note"); got != "new" {
		t.Errorf("after Set Note = %q, want new", got)
	}
	if got := d2.Get("Added"); got != "x" {
		t.Errorf("after Set Added = %q, want x", got)
	}
	if got := d2.GetStringList("Tags"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("after Append Tags = %v, want [a b]", got)
	}
	if got := d2.GetStringList("Fresh"); !reflect.DeepEqual(got, []string{"y"}) {
		t.Errorf("after Append Fresh = %v, want [y]", got)
	}
	if string(d2.Body) != "body\n" {
		t.Errorf("body = %q, want %q", string(d2.Body), "body\n")
	}
}

func TestGetStringMap(t *testing.T) {
	src := "---\nMilestone:\n  b/repo: \"2\"\n  a/repo: \"1\"\nScalar: x\nSeq:\n  - a\n---\nbody\n"
	d, err := ParseDocBytes("mem.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a/repo": "1", "b/repo": "2"}
	if got := d.GetStringMap("Milestone"); !reflect.DeepEqual(got, want) {
		t.Errorf("GetStringMap Milestone = %v, want %v", got, want)
	}
	if got := d.GetStringMap("Scalar"); got != nil {
		t.Errorf("GetStringMap on scalar = %v, want nil", got)
	}
	if got := d.GetStringMap("Seq"); got != nil {
		t.Errorf("GetStringMap on sequence = %v, want nil", got)
	}
	if got := d.GetStringMap("Absent"); got != nil {
		t.Errorf("GetStringMap absent = %v, want nil", got)
	}
}

func TestSetStringMap(t *testing.T) {
	src := "---\nKeep: yes\nmilestone: {}\n---\nbody\n"
	d, err := ParseDocBytes("mem.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d.SetStringMap("milestone", map[string]string{"zeta/repo": "9", "alpha/repo": "3"})
	out, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	iAlpha, iZeta := strings.Index(s, "alpha/repo"), strings.Index(s, "zeta/repo")
	if iAlpha < 0 || iZeta < 0 || iAlpha > iZeta {
		t.Errorf("SetStringMap keys not sorted:\n%s", s)
	}
	d2, err := ParseDocBytes("mem.md", out)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"alpha/repo": "3", "zeta/repo": "9"}
	if got := d2.GetStringMap("milestone"); !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip map = %v, want %v", got, want)
	}
	if string(d2.Body) != "body\n" {
		t.Errorf("body = %q, want %q", string(d2.Body), "body\n")
	}
	if d2.Get("Keep") != "yes" {
		t.Errorf("SetStringMap clobbered a sibling key: %q", d2.Get("Keep"))
	}

	// A nil map renders as an empty mapping, not an empty block.
	d2.SetStringMap("empty", nil)
	out2, err := d2.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out2), "empty: {}") {
		t.Errorf("nil map should render as `empty: {}`:\n%s", out2)
	}

	// A missing key is appended and reads back.
	d2.SetStringMap("fresh", map[string]string{"k": "v"})
	out3, err := d2.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	d3, err := ParseDocBytes("mem.md", out3)
	if err != nil {
		t.Fatal(err)
	}
	if got := d3.GetStringMap("fresh"); !reflect.DeepEqual(got, map[string]string{"k": "v"}) {
		t.Errorf("appended map = %v, want {k:v}", got)
	}
}

func TestSetInt(t *testing.T) {
	src := "---\nissue:\n---\nbody\n"
	d, err := ParseDocBytes("mem.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d.SetInt("issue", 42)
	d.SetInt("added", 7)
	out, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "issue: 42\n") {
		t.Errorf("SetInt issue not a bare int:\n%s", s)
	}
	if strings.Contains(s, `"42"`) {
		t.Errorf("SetInt emitted a quoted string:\n%s", s)
	}
	if !strings.Contains(s, "added: 7\n") {
		t.Errorf("SetInt appended key not a bare int:\n%s", s)
	}
	d2, err := ParseDocBytes("mem.md", out)
	if err != nil {
		t.Fatal(err)
	}
	if d2.Get("issue") != "42" || d2.Get("added") != "7" {
		t.Errorf("round-trip ints = %q/%q, want 42/7", d2.Get("issue"), d2.Get("added"))
	}
	if string(d2.Body) != "body\n" {
		t.Errorf("body = %q, want %q", string(d2.Body), "body\n")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	src := "---\nStatus: Draft\n---\nhello\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := ParseDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Set("Status", "Locked")
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "---\nStatus: Locked\n---\nhello\n" {
		t.Errorf("saved = %q", string(raw))
	}
}

func TestSection(t *testing.T) {
	body := []byte("# H1\n\n## A\nline1\nline2\n## B\nline3\n")
	if got := string(Section(body, "A")); got != "line1\nline2" {
		t.Errorf("Section A = %q, want %q", got, "line1\nline2")
	}
	if got := string(Section(body, "B")); got != "line3" {
		t.Errorf("Section B = %q, want %q", got, "line3")
	}
	if got := Section(body, "Missing"); got != nil {
		t.Errorf("Section Missing = %q, want nil", string(got))
	}
}

func TestBulletFormSection(t *testing.T) {
	body := []byte("- **Objective:**\nline one\nline two\n\n- **Next:**\nrest\n")
	if got := string(BulletFormSection(body, "Objective")); got != "line one\nline two\n\n" {
		t.Errorf("BulletFormSection = %q", got)
	}
	if got := BulletFormSection(body, "Missing"); got != nil {
		t.Errorf("BulletFormSection Missing = %q, want nil", string(got))
	}
}

func TestIsHeading(t *testing.T) {
	cases := map[string]bool{
		"# h":      true,
		"  ## h":   true,
		"plain":    false,
		"- bullet": false,
	}
	for in, want := range cases {
		if got := IsHeading([]byte(in)); got != want {
			t.Errorf("IsHeading(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestStripHTMLComments(t *testing.T) {
	cases := map[string]string{
		"<!-- a -->hello<!-- b\nc -->": "hello",
		"  plain text  ":               "plain text",
		"":                             "",
	}
	for in, want := range cases {
		if got := StripHTMLComments([]byte(in)); got != want {
			t.Errorf("StripHTMLComments(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeSlug(t *testing.T) {
	cases := map[string]string{
		"Voice Search: v1!":     "voice-search-v1",
		"  Hello   World  ":     "hello-world",
		"":                      "",
		"!!!":                   "",
		strings.Repeat("a", 50): strings.Repeat("a", 40),
	}
	for in, want := range cases {
		if got := SanitizeSlug(in); got != want {
			t.Errorf("SanitizeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
