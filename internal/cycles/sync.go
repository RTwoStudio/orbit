package cycles

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/RTwoStudio/orbit/internal/forge"
	"github.com/RTwoStudio/orbit/internal/logx"
)

// MilestoneOutcome.Action values.
const (
	// SyncCreated marks a milestone that did not exist on the forge and was
	// created by this run.
	SyncCreated = "created"
	// SyncUnchanged marks a repo whose milestone number was already recorded;
	// no forge call is made for it.
	SyncUnchanged = "unchanged"
)

// CycleSyncReport is the JSON-tagged result of SyncCycle, reused by T6's
// `cycle sync --json` rendering. Cycle is the refreshed read shape; Milestones
// holds one outcome per distinct repo and Skipped one entry per bet that could
// not contribute a milestone. Both slices are non-nil so JSON renders `[]`.
type CycleSyncReport struct {
	Cycle      *Cycle             `json:"cycle"`
	Milestones []MilestoneOutcome `json:"milestones"`
	Skipped    []SkippedBet       `json:"skipped"`
}

// MilestoneOutcome records what happened to one distinct repo's milestone.
type MilestoneOutcome struct {
	Provider string `json:"provider"`
	Repo     string `json:"repo"`
	Number   int    `json:"number"`
	URL      string `json:"url"`
	Action   string `json:"action"`
}

// SkippedBet records a bet that contributed no milestone: it has no
// `project:`, its project failed to resolve, or its repo key collided with an
// earlier repo's.
type SkippedBet struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Reason  string `json:"reason"`
}

// resolveRemote and newForgeClient are unexported injectable seams so
// SyncCycle's repo resolution and forge calls are unit-testable without git or
// the network; they mirror internal/forge's own injectable runner seam.
var (
	resolveRemote  = forge.Resolve
	newForgeClient = forge.New
)

// repoKey is the internal distinctness key for a resolved repo: the provider
// plus the owner/repo slug. Two repos with the same slug but different
// providers collide on the cycle's `owner/repo` map key.
type repoKey struct {
	provider string
	slug     string
}

// syncGroup is one distinct {provider, owner/repo} among a cycle's bets.
type syncGroup struct {
	remote  forge.Remote
	project string // representative bet project dir (the first seen)
	bets    []WorkItem
}

// SyncCycle creates one forge milestone per distinct repo among the open
// cycle's bets (title `C-#### — <goal>`, due = the cycle's `end`) and records
// each number in the cycle's `owner/repo` → number `milestone` map.
//
// It is idempotent by record-and-skip: a repo whose number is already recorded
// is reported unchanged and never re-probed, so a no-op re-sync stays fully
// offline. Repo resolution itself only reads git config; the network is
// touched exclusively by CreateMilestone, and `Available` is probed lazily
// (once per provider) only when a create is actually needed.
//
// Best-effort: a bet with no `project:` or an unresolvable project is skipped
// and reported, never fatal. A CreateMilestone failure persists the milestones
// created so far before returning the coded error, so a retry never
// duplicates. A cycle with no resolvable repo performs no frontmatter write.
// No open cycle is a state_conflict (7).
func (s *Store) SyncCycle(ctx context.Context) (*CycleSyncReport, error) {
	id, err := s.ReadCurrent()
	if err != nil {
		return nil, err
	}
	cy, err := s.ShowCycle(id)
	if err != nil {
		return nil, err
	}

	report := &CycleSyncReport{
		Cycle:      cy,
		Milestones: []MilestoneOutcome{},
		Skipped:    []SkippedBet{},
	}

	groups, order, skipped := groupBets(cy.Bets)
	report.Skipped = append(report.Skipped, skipped...)

	// A single-repo cycle records the convenience scalars; a cycle spanning
	// repos leaves them empty and relies on the milestone map.
	scalars := CycleForge{}
	if len(order) == 1 {
		g := groups[order[0]]
		scalars.Project = g.project
		scalars.Repo = g.remote.Slug()
		scalars.Provider = g.remote.Provider
	}

	existing := cy.Milestones
	// Seed the accumulator with the numbers already recorded for repos that
	// are still among the bets (this is the "recompute from the current
	// distinct repos": stale entries are simply never seeded, so the final
	// write drops them) and keeps every incremental write complete.
	recorded := map[string]int{}
	for _, key := range order {
		if n, ok := existing[key.slug]; ok && n > 0 {
			recorded[key.slug] = n
		}
	}

	clients := map[string]forge.Client{}
	probed := map[string]bool{}

	persist := func() (*Cycle, error) {
		f := scalars
		f.Milestones = cloneIntMap(recorded)
		return s.SetCycleForge(id, f)
	}

	handled := map[string]bool{}
	for _, key := range order {
		g := groups[key]
		slug := key.slug

		if handled[slug] {
			// A second distinct repo maps to the same owner/repo key
			// (necessarily a different provider). Nothing unrecordable is
			// written; report every bet as a collision.
			for _, b := range g.bets {
				report.Skipped = append(report.Skipped, SkippedBet{
					ID: b.ID, Project: b.Project, Reason: "milestone map key collision",
				})
			}
			continue
		}
		handled[slug] = true

		if n, ok := existing[slug]; ok && n > 0 {
			report.Milestones = append(report.Milestones, MilestoneOutcome{
				Provider: g.remote.Provider, Repo: slug, Number: n, Action: SyncUnchanged,
			})
			continue
		}

		client, ok := clients[key.provider]
		if !ok {
			client = newForgeClient(g.remote)
			clients[key.provider] = client
		}
		if !probed[key.provider] {
			if err := client.Available(ctx); err != nil {
				return nil, err
			}
			probed[key.provider] = true
		}

		ms, cerr := client.CreateMilestone(ctx, milestoneTitle(cy), cy.End)
		if cerr != nil {
			if _, perr := persist(); perr != nil {
				return nil, perr
			}
			return nil, cerr
		}
		recorded[slug] = ms.Number
		report.Milestones = append(report.Milestones, MilestoneOutcome{
			Provider: g.remote.Provider, Repo: slug, Number: ms.Number, URL: ms.URL, Action: SyncCreated,
		})
		// Persist incrementally so a later failure can never lose this number.
		updated, perr := persist()
		if perr != nil {
			return nil, perr
		}
		report.Cycle = updated
	}

	// One final reconciliation when at least one repo resolved: settles the
	// scalars and drops stale map entries. With zero resolvable repos, make no
	// write at all.
	if len(order) > 0 {
		updated, perr := persist()
		if perr != nil {
			return nil, perr
		}
		report.Cycle = updated
	}

	logx.Info("cycle sync id=%s milestones=%d skipped=%d", id, len(report.Milestones), len(report.Skipped))
	return report, nil
}

// groupBets partitions a cycle's bets by their resolved repo. Bets with no
// `project:` or an unresolvable project are returned as skips. The returned
// order is deterministic: provider, then owner/repo slug.
func groupBets(bets []WorkItem) (map[repoKey]*syncGroup, []repoKey, []SkippedBet) {
	groups := map[repoKey]*syncGroup{}
	var skipped []SkippedBet
	for _, b := range bets {
		if strings.TrimSpace(b.Project) == "" {
			skipped = append(skipped, SkippedBet{ID: b.ID, Project: b.Project, Reason: "no project: set"})
			continue
		}
		remote, err := resolveRemote(b.Project)
		if err != nil {
			skipped = append(skipped, SkippedBet{ID: b.ID, Project: b.Project, Reason: err.Error()})
			continue
		}
		k := repoKey{provider: remote.Provider, slug: remote.Slug()}
		g, ok := groups[k]
		if !ok {
			g = &syncGroup{remote: remote, project: b.Project}
			groups[k] = g
		}
		g.bets = append(g.bets, b)
	}

	order := make([]repoKey, 0, len(groups))
	for k := range groups {
		order = append(order, k)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].provider != order[j].provider {
			return order[i].provider < order[j].provider
		}
		return order[i].slug < order[j].slug
	})
	return groups, order, skipped
}

// milestoneTitle formats a cycle's milestone title: `C-#### — <goal>`
// (U+2014 em dash), matching the locked design.
func milestoneTitle(cy *Cycle) string {
	return fmt.Sprintf("%s — %s", cy.ID, cy.Goal)
}

// cloneIntMap copies a milestone map so an incremental write never aliases the
// accumulator.
func cloneIntMap(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
