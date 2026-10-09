package neocortex

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/scaffold"
)

// LockHash computes the Lock-Hash of a body: "sha256:" + hex, byte-exact
// over everything after the closing '---' of the frontmatter.
func LockHash(body []byte) string {
	h := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(h[:])
}

// VerifyHash recomputes the body hash and compares it to the recorded
// Lock-Hash. Returns a tamper_detected error naming the file on mismatch.
func VerifyHash(d *doc.Doc, recordedHash string) error {
	if recordedHash == "" {
		return exit.New(exit.TamperDetected,
			fmt.Sprintf("%s has no Lock-Hash recorded", d.Path))
	}
	actual := LockHash(d.Body)
	if actual != recordedHash {
		return exit.New(exit.TamperDetected,
			fmt.Sprintf("%s was modified after lock (hash mismatch)\n  recorded: %s\n  actual:   %s",
				d.Path, short(recordedHash), short(actual)),
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
	if m := scaffold.TokenRe.FindString(string(body)); m != "" {
		return exit.New(exit.PreflightFailed,
			fmt.Sprintf("%s still contains unresolved token %s", path, m))
	}
	return nil
}
