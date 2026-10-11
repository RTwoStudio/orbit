package domain

import "github.com/RTwoStudio/orbit/internal/scaffold"

// Cycles stub token sets. Registered here so cache-time validation of the
// cycles/ registry tree works before T3 authors its stubs. T3's
// cycles/stubs/work.stub.md and cycles/stubs/cycle.stub.md MUST use exactly
// these tokens (or update this registration in lockstep).
func init() {
	scaffold.Register("work.stub.md", []string{
		"WORK_ID", "WORK_TITLE", "WORK_OWNER", "WORK_OWNER_NAME", "SCOPE", "DATE", "REGISTRY_VERSION",
	})
	scaffold.Register("cycle.stub.md", []string{
		"RELEASE", "CYCLE_ID", "CYCLE_GOAL", "CREATED_BY", "START_DATE", "END_DATE", "DATE", "REGISTRY_VERSION",
	})
}
