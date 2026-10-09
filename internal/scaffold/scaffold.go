// Package scaffold holds domain-agnostic stub rendering and validation: the
// {{UPPER_SNAKE}} token convention, a per-stub registry of known tokens, and
// the render/validate helpers. Domains register their own stubs via Register;
// the package ships with the NeoCortex defaults so existing behavior is
// unchanged.
package scaffold

import (
	"fmt"
	"regexp"
	"sort"
)

// TokenRe matches {{UPPER_SNAKE}} tokens. Exported so domain packages can run
// their own preflight scans with the exact same grammar.
var TokenRe = regexp.MustCompile(`\{\{([A-Z_]+)\}\}`)

// stubTokens is the per-stub token registry (validated at cache time; §10).
// The defaults are the NeoCortex stub sets; additional domains extend it via
// Register.
var stubTokens = map[string]map[string]bool{
	"00-concept.stub.md": {"ISSUE_ID": true, "ISSUE_TITLE": true, "DATE": true, "SOURCE": true, "REGISTRY_VERSION": true, "DETAIL": true},
	"00-quick.stub.md":   {"ISSUE_ID": true, "ISSUE_TITLE": true, "DATE": true, "SOURCE": true, "REGISTRY_VERSION": true, "DETAIL": true},
	"01-plan.stub.md":    {"ISSUE_ID": true, "ISSUE_TITLE": true, "DATE": true, "REGISTRY_VERSION": true},
	"addenda.stub.md":    {"ADDENDA_NUM": true, "ADDENDA_TITLE": true, "ISSUE_ID": true, "DATE": true, "REGISTRY_VERSION": true},
	"task.stub.md":       {"TASK_ID": true, "TASK_NAME": true, "ISSUE_ID": true, "DEPENDS_ON": true, "ORIGIN": true, "DATE": true, "REGISTRY_VERSION": true, "BLOCKED_BY": true, "BLOCKS": true},
}

// Register adds (or replaces) the known token set for a stub name. Domains
// call it at init to make their stubs validatable and renderable.
func Register(name string, tokens []string) {
	set := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		set[t] = true
	}
	stubTokens[name] = set
}

// ValidateStub checks a stub only uses known tokens for its name.
func ValidateStub(name string, content []byte) error {
	known, ok := stubTokens[name]
	if !ok {
		return fmt.Errorf("unknown stub %q", name)
	}
	for _, m := range TokenRe.FindAllStringSubmatch(string(content), -1) {
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
	for _, m := range TokenRe.FindAllStringSubmatch(stub, -1) {
		if !known[m[1]] {
			return nil, fmt.Errorf("stub contains unknown token {{%s}}", m[1])
		}
	}
	out := TokenRe.ReplaceAllStringFunc(stub, func(tok string) string {
		name := tok[2 : len(tok)-2]
		if v, ok := values[name]; ok {
			return v
		}
		return tok
	})
	// Any surviving {{TOKEN}} means a value was not supplied.
	if m := TokenRe.FindString(out); m != "" {
		return nil, fmt.Errorf("render failed: unresolved token %s — refusing silent leftovers", m)
	}
	return []byte(out), nil
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
