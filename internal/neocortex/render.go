package neocortex

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"

	"github.com/RTwoStudio/orbit/internal/exit"
)

// tokenRe matches {{UPPER_SNAKE}} tokens.
var tokenRe = regexp.MustCompile(`\{\{([A-Z_]+)\}\}`)

// Known token sets per stub (validated at cache time; §10).
var stubTokens = map[string]map[string]bool{
	"00-concept.stub.md": {"ISSUE_ID": true, "ISSUE_TITLE": true, "DATE": true, "SOURCE": true, "REGISTRY_VERSION": true, "DETAIL": true},
	"01-plan.stub.md":    {"ISSUE_ID": true, "ISSUE_TITLE": true, "DATE": true, "REGISTRY_VERSION": true},
	"addenda.stub.md":    {"ADDENDA_NUM": true, "ADDENDA_TITLE": true, "ISSUE_ID": true, "DATE": true, "REGISTRY_VERSION": true},
	"task.stub.md":       {"TASK_ID": true, "TASK_NAME": true, "ISSUE_ID": true, "DEPENDS_ON": true, "ORIGIN": true, "DATE": true, "REGISTRY_VERSION": true, "BLOCKED_BY": true, "BLOCKS": true},
}

// ValidateStub checks a stub only uses known tokens for its name.
func ValidateStub(name string, content []byte) error {
	known, ok := stubTokens[name]
	if !ok {
		return fmt.Errorf("unknown stub %q", name)
	}
	for _, m := range tokenRe.FindAllStringSubmatch(string(content), -1) {
		if !known[m[1]] {
			return fmt.Errorf("stub %s uses unknown token {{%s}}", name, m[1])
		}
	}
	return nil
}

// Render substitutes tokens; unknown leftovers are an error, never silent.
func Render(stub string, values map[string]string) ([]byte, error) {
	// Validate stub tokens against the union of known tokens first.
	known := map[string]bool{}
	for _, set := range stubTokens {
		for k := range set {
			known[k] = true
		}
	}
	for _, m := range tokenRe.FindAllStringSubmatch(stub, -1) {
		if !known[m[1]] {
			return nil, fmt.Errorf("stub contains unknown token {{%s}}", m[1])
		}
	}
	out := tokenRe.ReplaceAllStringFunc(stub, func(tok string) string {
		name := tok[2 : len(tok)-2]
		if v, ok := values[name]; ok {
			return v
		}
		return tok
	})
	// Any surviving {{TOKEN}} means a value was not supplied.
	if m := tokenRe.FindString(out); m != "" {
		return nil, fmt.Errorf("render failed: unresolved token %s — refusing silent leftovers", m)
	}
	return []byte(out), nil
}

// LockHash computes the Lock-Hash of a body: "sha256:" + hex, byte-exact
// over everything after the closing '---' of the frontmatter.
func LockHash(body []byte) string {
	h := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(h[:])
}

// VerifyHash recomputes the body hash and compares it to the recorded
// Lock-Hash. Returns a tamper_detected error naming the file on mismatch.
func VerifyHash(doc *Doc, recordedHash string) error {
	if recordedHash == "" {
		return exit.New(exit.TamperDetected,
			fmt.Sprintf("%s has no Lock-Hash recorded", doc.Path))
	}
	actual := LockHash(doc.Body)
	if actual != recordedHash {
		return exit.New(exit.TamperDetected,
			fmt.Sprintf("%s was modified after lock (hash mismatch)\n  recorded: %s\n  actual:   %s",
				doc.Path, short(recordedHash), short(actual)),
			"locked artifacts are immutable — record changes via 'orbit neocortex addenda new' instead",
			"if this was accidental, restore the file from version control")
	}
	return nil
}

func short(h string) string {
	if len(h) > 19 {
		return h[:19] + "…"
	}
	return h
}

// agentPlaceholderRe matches real placeholder comments (at line start,
// possibly indented) — not the inline documentation of the syntax inside
// blockquotes.
var agentPlaceholderRe = regexp.MustCompile(`(?m)^\s*<!-- Agent:`)

// GuardBody checks lock preflights: no agent placeholders, no tokens.
func GuardBody(path string, body []byte) error {
	if agentPlaceholderRe.Match(body) {
		return exit.New(exit.PreflightFailed,
			fmt.Sprintf("%s still contains '<!-- Agent:' placeholders — the agent must fill them before locking", path))
	}
	if m := tokenRe.FindString(string(body)); m != "" {
		return exit.New(exit.PreflightFailed,
			fmt.Sprintf("%s still contains unresolved token %s", path, m))
	}
	return nil
}

// SortKeys is a helper for deterministic JSON maps.
func SortKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
