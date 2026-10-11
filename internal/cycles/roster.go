package cycles

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/RTwoStudio/orbit/internal/doc"
	"github.com/RTwoStudio/orbit/internal/exit"
	"gopkg.in/yaml.v3"
)

// Developer is one roster entry: a stable handle plus the identity the CLI maps
// to. GitHub/GitLab are forge usernames (optional; consumed by `work sync`).
// Email is required and drives local handle resolution.
type Developer struct {
	Handle string
	Name   string
	Email  string
	GitHub string
	GitLab string
}

// Roster is the validated developer roster, keyed by stable handle. Every fault
// in the file (missing, empty, malformed, a handle that is not the slug of its
// name, a blank or duplicated email/name) is a preflight_failed (6).
type Roster struct {
	byHandle map[string]Developer
}

// rawDeveloper is the on-disk YAML shape of one roster entry.
type rawDeveloper struct {
	Name   string `yaml:"name"`
	Email  string `yaml:"email"`
	GitHub string `yaml:"github"`
	GitLab string `yaml:"gitlab"`
}

// newRoster validates a hand-written map and returns a Roster. Order is
// normalized for deterministic error messages.
func newRoster(path string, raw map[string]rawDeveloper) (*Roster, error) {
	if len(raw) == 0 {
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("developer roster %s has no entries", path),
			"add one entry per developer: <handle>: {name, email, github, gitlab}",
			"see the example in "+path)
	}
	bad := func(format string, args ...any) error {
		return exit.New(exit.PreflightFailed, fmt.Sprintf(format, args...),
			"fix "+path)
	}
	handles := make([]string, 0, len(raw))
	for h := range raw {
		handles = append(handles, h)
	}
	sort.Strings(handles)

	byHandle := make(map[string]Developer, len(raw))
	seenEmail := map[string]string{}
	seenName := map[string]string{}
	for _, handle := range handles {
		e := raw[handle]
		name := strings.TrimSpace(e.Name)
		email := strings.TrimSpace(e.Email)
		if name == "" {
			return nil, bad("developer roster %s: entry %q has no name", path, handle)
		}
		if want := doc.SanitizeSlug(name); handle != want {
			return nil, bad("developer roster %s: handle %q does not match the slug of name %q (want %q)",
				path, handle, name, want)
		}
		if email == "" {
			return nil, bad("developer roster %s: entry %q has no email", path, handle)
		}
		if prev, ok := seenEmail[strings.ToLower(email)]; ok {
			return nil, bad("developer roster %s: duplicate email %q (%s and %s)", path, email, prev, handle)
		}
		if prev, ok := seenName[strings.ToLower(name)]; ok {
			return nil, bad("developer roster %s: duplicate name %q (%s and %s)", path, name, prev, handle)
		}
		seenEmail[strings.ToLower(email)] = handle
		seenName[strings.ToLower(name)] = handle
		byHandle[handle] = Developer{
			Handle: handle,
			Name:   name,
			Email:  email,
			GitHub: strings.TrimSpace(e.GitHub),
			GitLab: strings.TrimSpace(e.GitLab),
		}
	}
	return &Roster{byHandle: byHandle}, nil
}

// Lookup returns the developer for a handle and whether it exists.
func (r *Roster) Lookup(handle string) (Developer, bool) {
	d, ok := r.byHandle[strings.TrimSpace(handle)]
	return d, ok
}

// Handles returns every roster handle in sorted order.
func (r *Roster) Handles() []string {
	out := make([]string, 0, len(r.byHandle))
	for h := range r.byHandle {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// LoadRoster reads and validates <vaultRoot>/Cycles/developers.yml. A missing
// file is a preflight_failed (6) with an install hint.
func (s *Store) LoadRoster() (*Roster, error) {
	path := s.RosterPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, exit.New(exit.PreflightFailed,
				fmt.Sprintf("no developer roster at %s", path),
				"run: orbit cycles install",
				"then add your entry: <handle>: {name, email, github, gitlab}")
		}
		return nil, exit.Wrap(exit.IOError, err, "cannot read "+path)
	}
	var raw map[string]rawDeveloper
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, exit.New(exit.PreflightFailed,
			fmt.Sprintf("invalid developer roster %s: %v", path, err),
			"fix the YAML in "+path)
	}
	return newRoster(path, raw)
}

// ResolveHandle resolves the local developer's handle from the vault clone's
// git identity (git config user.name / user.email) matched against the roster:
// email (case-insensitive) first, then name. Every fault — no roster, no git
// identity, or no roster match — is a preflight_failed (6) with a fix hint.
func (s *Store) ResolveHandle() (string, error) {
	dev, err := s.ResolveDeveloper()
	if err != nil {
		return "", err
	}
	return dev.Handle, nil
}

// ResolveDeveloper is ResolveHandle returning the whole matched roster entry;
// T4's `work sync` consumes its forge usernames.
func (s *Store) ResolveDeveloper() (Developer, error) {
	r, err := s.LoadRoster()
	if err != nil {
		return Developer{}, err
	}
	name, email, err := s.gitIdentity()
	if err != nil {
		return Developer{}, err
	}
	if email != "" {
		for _, h := range r.Handles() {
			d, _ := r.Lookup(h)
			if strings.EqualFold(d.Email, email) {
				return d, nil
			}
		}
	}
	if name != "" {
		for _, h := range r.Handles() {
			d, _ := r.Lookup(h)
			if strings.EqualFold(d.Name, name) {
				return d, nil
			}
		}
	}
	return Developer{}, exit.New(exit.PreflightFailed,
		fmt.Sprintf("no roster entry matches the git identity name=%q email=%q", name, email),
		"add yourself to "+s.RosterPath()+" (handle: {name, email, ...})",
		fmt.Sprintf("or set the clone identity: git -C %s config user.name \"<Your Name>\"", s.vaultRoot()))
}

// gitIdentity reads the vault clone's git user.name / user.email. An absent
// git binary or a clone without an identity is a preflight_failed (6).
func (s *Store) gitIdentity() (name, email string, err error) {
	dir := s.vaultRoot()
	if _, lerr := exec.LookPath("git"); lerr != nil {
		return "", "", exit.New(exit.PreflightFailed,
			"git is not installed — needed to resolve your developer handle",
			"install git, then retry")
	}
	name = gitConfigValue(dir, "user.name")
	email = gitConfigValue(dir, "user.email")
	if name == "" && email == "" {
		return "", "", exit.New(exit.PreflightFailed,
			fmt.Sprintf("no git identity in %s — cannot resolve your developer handle", dir),
			fmt.Sprintf("set one: git -C %s config user.name \"<Your Name>\"", dir),
			fmt.Sprintf("and: git -C %s config user.email <you@example.com>", dir))
	}
	return name, email, nil
}

// gitConfigValue runs `git -C dir config <key>` and returns the trimmed value
// ("" on any error, so a missing key is never fatal on its own).
func gitConfigValue(dir, key string) string {
	ctx := context.Background()
	full := []string{"-C", dir, "config", key}
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
