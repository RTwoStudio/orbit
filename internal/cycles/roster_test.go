package cycles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/fsutil"
)

// testStore builds an initialized Store over a fresh vault with an isolated
// $HOME, but no roster and no git identity — the raw state each roster test
// starts from.
func testStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s := Open(t.TempDir())
	for _, d := range []string{s.BacklogDir(), s.LedgerDir()} {
		if err := fsutil.EnsureDir(d); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// writeRoster writes raw roster YAML into the vault, replacing any existing one.
func writeRoster(t *testing.T, s *Store, content string) {
	t.Helper()
	if err := fsutil.AtomicWrite(s.RosterPath(), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRosterValid(t *testing.T) {
	s := testStore(t)
	writeRoster(t, s, testRoster)

	r, err := s.LoadRoster()
	if err != nil {
		t.Fatalf("LoadRoster: %v", err)
	}
	dev, ok := r.Lookup(testOwnerHandle)
	if !ok {
		t.Fatalf("Lookup(%q) not found", testOwnerHandle)
	}
	if dev.Name != testOwnerName || dev.Email != testOwnerEmail || dev.GitHub != "arashjf" || dev.GitLab != "arashjf" {
		t.Errorf("developer = %+v", dev)
	}
	if _, ok := r.Lookup("nobody"); ok {
		t.Error("Lookup(nobody) = found, want miss")
	}
	if got := r.Handles(); len(got) != 1 || got[0] != testOwnerHandle {
		t.Errorf("Handles() = %v", got)
	}
}

func TestLoadRosterFaults(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"missing entry", "# only comments\n"},
		{"blank name", "arash-jafarpour:\n  name: \"\"\n  email: a@example.com\n"},
		{"handle not slug of name", "arash:\n  name: Arash Jafarpour\n  email: a@example.com\n"},
		{"missing email", "arash-jafarpour:\n  name: Arash Jafarpour\n"},
		{"duplicate email", "arash-jafarpour:\n  name: Arash Jafarpour\n  email: dup@example.com\nseyed-mahdi-rostami:\n  name: Seyed Mahdi Rostami\n  email: DUP@example.com\n"},
		{"malformed yaml", "arash-jafarpour: [unterminated\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			writeRoster(t, s, tc.content)
			_, err := s.LoadRoster()
			if err == nil {
				t.Fatalf("LoadRoster(%s) = nil, want preflight_failed", tc.name)
			}
			if code := codeOf(t, err); code != exit.PreflightFailed {
				t.Errorf("code = %v, want preflight_failed", code)
			}
		})
	}
}

func TestLoadRosterMissingFile(t *testing.T) {
	s := testStore(t)
	_, err := s.LoadRoster()
	if codeOf(t, err) != exit.PreflightFailed {
		t.Fatalf("LoadRoster (no file) code = %v, want preflight_failed", codeOf(t, err))
	}
	var e *exit.Error
	if !asExit(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), "cycles install") {
		t.Errorf("missing-roster hint should point at 'cycles install': %v", err)
	}
	// It names the canonical path.
	if !strings.Contains(err.Error(), s.RosterPath()) {
		t.Errorf("error %q does not name %q", err.Error(), s.RosterPath())
	}
}

// resolveRosterStore is a store with a roster and a matching git identity.
func resolveRosterStore(t *testing.T, roster string, gitName, gitEmail string) *Store {
	t.Helper()
	s := testStore(t)
	writeRoster(t, s, roster)
	setGitIdentityAs(t, s.vaultRoot(), gitName, gitEmail)
	return s
}

func TestResolveHandleByEmail(t *testing.T) {
	s := resolveRosterStore(t, testRoster, "Some Other Name", testOwnerEmail)
	got, err := s.ResolveHandle()
	if err != nil {
		t.Fatalf("ResolveHandle: %v", err)
	}
	if got != testOwnerHandle {
		t.Errorf("ResolveHandle = %q, want %q", got, testOwnerHandle)
	}
}

func TestResolveHandleByName(t *testing.T) {
	s := resolveRosterStore(t, testRoster, testOwnerName, "different@example.com")
	got, err := s.ResolveHandle()
	if err != nil {
		t.Fatalf("ResolveHandle: %v", err)
	}
	if got != testOwnerHandle {
		t.Errorf("ResolveHandle = %q, want %q", got, testOwnerHandle)
	}
}

func TestResolveHandleNoMatch(t *testing.T) {
	s := resolveRosterStore(t, testRoster, "Stranger Danger", "stranger@example.com")
	_, err := s.ResolveHandle()
	if codeOf(t, err) != exit.PreflightFailed {
		t.Fatalf("no-match code = %v, want preflight_failed", codeOf(t, err))
	}
	var e *exit.Error
	if !asExit(err, &e) || !strings.Contains(strings.Join(e.Hints, " "), s.RosterPath()) {
		t.Errorf("no-match hint should point at the roster: %v", err)
	}
}

func TestResolveHandleNoGitIdentity(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	s := testStore(t)
	writeRoster(t, s, testRoster)
	// The vault is not a repo and $HOME carries no git identity.
	if _, err := s.ResolveHandle(); codeOf(t, err) != exit.PreflightFailed {
		t.Fatalf("no-identity code = %v, want preflight_failed", codeOf(t, err))
	}
}

func TestNewWorkStampsOwner(t *testing.T) {
	s := setupVault(t)
	item, err := s.NewWork("Voice search", "delijan-driver-app")
	if err != nil {
		t.Fatalf("NewWork: %v", err)
	}
	if want := "W-0001-" + testOwnerHandle; item.ID != want {
		t.Errorf("id = %q, want %q", item.ID, want)
	}
	if item.Owner != testOwnerHandle || item.OwnerName != testOwnerName {
		t.Errorf("owner = %q/%q, want %q/%q", item.Owner, item.OwnerName, testOwnerHandle, testOwnerName)
	}
	wantPath := filepath.Join(s.BacklogDir(), "W-0001-arash-jafarpour - voice-search.md")
	if item.Path != wantPath {
		t.Errorf("path = %q, want %q", item.Path, wantPath)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"owner: " + testOwnerHandle, `owner_name: "` + testOwnerName + `"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("note missing %q:\n%s", want, data)
		}
	}
}

func TestNewWorkPerOwnerSequence(t *testing.T) {
	s := setupVault(t)
	first, err := s.NewWork("First", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.NewWork("Second", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if want := "W-0001-" + testOwnerHandle; first.ID != want {
		t.Errorf("first id = %q, want %q", first.ID, want)
	}
	if want := "W-0002-" + testOwnerHandle; second.ID != want {
		t.Errorf("second id = %q, want %q", second.ID, want)
	}
}

// setGitIdentityAs inits dir as a git repo with a local identity.
func setGitIdentityAs(t *testing.T, dir, name, email string) {
	t.Helper()
	if !gitAvailable() {
		t.Skip("git not installed")
	}
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.name", name)
	runGit(t, dir, "config", "user.email", email)
}

// asExit is errors.As restricted to *exit.Error, kept local to avoid importing
// errors in every test helper.
func asExit(err error, target **exit.Error) bool {
	for err != nil {
		if e, ok := err.(*exit.Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
