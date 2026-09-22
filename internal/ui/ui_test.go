package ui

import "testing"

func TestSetupPolicy(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	cases := []struct {
		cfg    string
		noFlag bool
		want   bool
		note   string
	}{
		{"auto", false, false, "piped stdout: auto disables color"},
		{"auto", true, false, "--no-color wins under auto"},
		{"never", false, false, "never disables color"},
		{"always", false, true, "always forces color"},
		{"always", true, false, "--no-color wins over always"},
		{"", false, false, "empty config falls back to auto"},
		{"garbage", false, false, "unknown value falls back to auto"},
	}
	for _, c := range cases {
		Setup(c.cfg, c.noFlag)
		if Enabled() != c.want {
			t.Errorf("%s: got %v, want %v", c.note, Enabled(), c.want)
		}
	}
}

func TestHelpersRespectPolicy(t *testing.T) {
	Setup("never", false)
	if Red("x") != "x" {
		t.Error("Red must be plain when disabled")
	}
	Setup("always", false)
	if got := Dim("hint:"); got != "\x1b[2mhint:\x1b[0m" {
		t.Errorf("Dim with color: got %q", got)
	}
	Setup("always", false)
	if got := Red(""); got != "" {
		t.Error("empty strings stay empty")
	}
}
