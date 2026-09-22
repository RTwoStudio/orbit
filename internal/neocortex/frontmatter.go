package neocortex

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RTwoStudio/orbit-cli/internal/fsutil"
	"gopkg.in/yaml.v3"
)

// FrontmatterError distinguishes parse problems for callers mapping to
// exit codes (tamper vs not_found etc.).
type FrontmatterError struct{ msg string }

func (e *FrontmatterError) Error() string { return e.msg }

// Doc is a parsed markdown file with YAML frontmatter. The header is a
// yaml.Node mapping so key order and comments survive rewrites; the body
// is preserved byte-exact (hashes cover it).
type Doc struct {
	Path string
	Head *yaml.Node // mapping node
	Body []byte     // bytes after the closing ---, byte-exact
}

// ParseDoc reads and parses path. LF-only policy: CRLF is rejected with a
// clear error.
func ParseDoc(path string) (*Doc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDocBytes(path, data)
}

// ParseDocBytes parses frontmatter from raw bytes.
func ParseDocBytes(path string, data []byte) (*Doc, error) {
	if bytes.Contains(data, []byte("\r\n")) {
		return nil, &FrontmatterError{fmt.Sprintf("%s: CRLF line endings not supported (LF-only policy)", path)}
	}
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return nil, &FrontmatterError{fmt.Sprintf("%s: file must start with '---' frontmatter", path)}
	}
	rest := data[4:]
	idx := bytes.Index(rest, []byte("\n---\n"))
	if idx < 0 {
		return nil, &FrontmatterError{fmt.Sprintf("%s: frontmatter closing '---' not found", path)}
	}
	fmBytes := rest[:idx]
	body := rest[idx+5:]

	var doc yaml.Node
	if err := yaml.Unmarshal(fmBytes, &doc); err != nil {
		return nil, &FrontmatterError{fmt.Sprintf("%s: invalid frontmatter YAML: %v", path, err)}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, &FrontmatterError{fmt.Sprintf("%s: frontmatter must be a YAML mapping", path)}
	}
	return &Doc{Path: path, Head: doc.Content[0], Body: body}, nil
}

// Bytes re-renders the full file: frontmatter (preserving key order and
// comments) + body byte-exact.
func (d *Doc) Bytes() ([]byte, error) {
	var fm bytes.Buffer
	enc := yaml.NewEncoder(&fm)
	enc.SetIndent(2)
	if err := enc.Encode(d.Head); err != nil {
		return nil, err
	}
	enc.Close()
	fmBytes := fm.Bytes()
	fmBytes = bytes.TrimSuffix(fmBytes, []byte("\n"))
	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(fmBytes)
	out.WriteString("\n---\n")
	out.Write(d.Body)
	return out.Bytes(), nil
}

// Save writes the doc back atomically.
func (d *Doc) Save() error {
	data, err := d.Bytes()
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(d.Path, data, 0o644)
}

// Get returns the scalar value of a frontmatter key ("" if absent).
func (d *Doc) Get(key string) string {
	for i := 0; i+1 < len(d.Head.Content); i += 2 {
		if d.Head.Content[i].Value == key {
			v := d.Head.Content[i+1]
			if v.Kind == yaml.ScalarNode {
				return v.Value
			}
			return v.Value
		}
	}
	return ""
}

// Set sets (or appends) a scalar frontmatter key.
func (d *Doc) Set(key, value string) {
	for i := 0; i+1 < len(d.Head.Content); i += 2 {
		if d.Head.Content[i].Value == key {
			d.Head.Content[i+1].Value = value
			d.Head.Content[i+1].Tag = "!!str"
			d.Head.Content[i+1].Kind = yaml.ScalarNode
			d.Head.Content[i+1].Style = 0
			return
		}
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	v := &yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: "!!str"}
	d.Head.Content = append(d.Head.Content, k, v)
}

// GetStringList returns a sequence key as []string.
func (d *Doc) GetStringList(key string) []string {
	for i := 0; i+1 < len(d.Head.Content); i += 2 {
		if d.Head.Content[i].Value == key {
			v := d.Head.Content[i+1]
			if v.Kind != yaml.SequenceNode {
				return nil
			}
			var out []string
			for _, item := range v.Content {
				out = append(out, item.Value)
			}
			return out
		}
	}
	return nil
}

// AppendToList appends a scalar to a sequence key (creating it if missing).
func (d *Doc) AppendToList(key, value string) {
	for i := 0; i+1 < len(d.Head.Content); i += 2 {
		if d.Head.Content[i].Value == key {
			v := d.Head.Content[i+1]
			if v.Kind != yaml.SequenceNode {
				v.Kind = yaml.SequenceNode
				v.Tag = "!!seq"
				v.Content = nil
				v.Value = ""
				v.Style = 0
			}
			v.Content = append(v.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: "!!str"})
			return
		}
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: "!!str"})
	d.Head.Content = append(d.Head.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, seq)
}

// Section extracts the text of a "## <name>" section (excluding the heading
// line) up to the next heading of the same or higher level. Returns nil if
// absent.
func Section(body []byte, name string) []byte {
	needle := []byte("## " + name)
	var out []byte
	in := false
	for _, line := range bytes.SplitAfter(body, []byte("\n")) {
		if in {
			if isHeading(line) {
				break
			}
			out = append(out, line...)
			continue
		}
		t := bytes.TrimLeft(line, " ")
		if bytes.HasPrefix(t, needle) {
			in = true
		}
	}
	if !in {
		return nil
	}
	return bytes.TrimSuffix(out, []byte("\n"))
}

func isHeading(line []byte) bool {
	t := bytes.TrimLeft(line, " ")
	return bytes.HasPrefix(t, []byte("#"))
}

// BulletFormSection extracts text under a "- **Name:**" bullet (the stub
// style) up to the next "- **" bullet or heading.
func BulletFormSection(body []byte, name string) []byte {
	needle := []byte("- **" + name + ":**")
	var out []byte
	in := false
	for _, line := range bytes.SplitAfter(body, []byte("\n")) {
		if in {
			t := bytes.TrimLeft(line, " ")
			if isHeading(line) || bytes.HasPrefix(t, []byte("- **")) {
				break
			}
			out = append(out, line...)
			continue
		}
		if bytes.HasPrefix(line, needle) {
			in = true
			// Content may start on the same line after the bullet.
			rest := bytes.TrimSuffix(line, []byte("\n"))[len(needle):]
			if len(bytes.TrimSpace(rest)) > 0 {
				out = append(out, rest...)
			}
		}
	}
	if !in {
		return nil
	}
	return out
}

// StripHTMLComments removes <!-- ... --> blocks (multiline) and trims.
func StripHTMLComments(b []byte) string {
	var out strings.Builder
	for {
		start := strings.Index(string(b), "<!--")
		if start < 0 {
			out.Write(b)
			break
		}
		end := strings.Index(string(b[start:]), "-->")
		if end < 0 {
			out.Write(b[:start])
			b = nil
			break
		}
		out.Write(b[:start])
		b = b[start+end+3:]
	}
	return strings.TrimSpace(out.String())
}

// ListIssues returns all issue numbers present on disk, sorted ascending.
func ListIssues() ([]int, error) {
	entries, err := os.ReadDir(IssuesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []int
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, "issue-") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(name, "issue-%d", &n); err == nil {
			out = append(out, n)
		}
	}
	return out, nil
}

// SanitizeSlug converts a title to a filename slug: lowercase, non-alnum →
// '-', trim, max 40 chars.
func SanitizeSlug(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	s := b.String()
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	return s
}

// AddendaFilePrefix returns the zero-padded 2-digit NN for addenda files.
func AddendaFilePrefix(n int) string {
	return fmt.Sprintf("%02d", n)
}

// FindAddendaFile locates the file for addenda NN in an issue's addenda dir.
func FindAddendaFile(issue, nn int) (string, error) {
	dir := AddendaDir(issue)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	prefix := AddendaFilePrefix(nn) + "-"
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".md") {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", os.ErrNotExist
}
