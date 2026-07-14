package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("roots: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.FetchIntervalMinutes != defaultFetchMinutes {
		t.Errorf("fetch interval = %d, want %d", c.FetchIntervalMinutes, defaultFetchMinutes)
	}
	if c.LocalRefreshSeconds != defaultLocalSeconds {
		t.Errorf("local refresh = %d, want %d", c.LocalRefreshSeconds, defaultLocalSeconds)
	}
	if c.ClickAction != ActionOpenFolder {
		t.Errorf("click action = %q, want %q", c.ClickAction, ActionOpenFolder)
	}
	if len(c.IgnoreDirs()) == 0 {
		t.Error("expected default ignore list")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("fetchIntervalMinutes: 15\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetClickAction(ActionEditor, "code {path}"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetRoots([]Root{{Path: "~/work", Depth: 3, AutoFetch: true}}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ClickAction != ActionEditor || reloaded.CustomCommand != "code {path}" {
		t.Errorf("click not persisted: %q %q", reloaded.ClickAction, reloaded.CustomCommand)
	}
	if reloaded.FetchIntervalMinutes != 15 {
		t.Errorf("fetch interval not preserved: %d", reloaded.FetchIntervalMinutes)
	}
	roots := reloaded.RootList()
	if len(roots) != 1 || roots[0].Path != "~/work" || roots[0].Depth != 3 || !roots[0].AutoFetch {
		t.Errorf("roots not persisted: %+v", roots)
	}
}

func TestSetKeepFreshRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := c.SetKeepFresh(repo, true); err != nil {
		t.Fatal(err)
	}
	if !c.KeepFreshRepos()[repo] {
		t.Fatal("repository was not marked keep-fresh")
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.KeepFreshRepos()[repo] {
		t.Fatal("keep-fresh repository was not persisted")
	}
	if err := reloaded.SetKeepFresh(repo, false); err != nil {
		t.Fatal(err)
	}
	if reloaded.KeepFreshRepos()[repo] {
		t.Fatal("repository remained keep-fresh after disabling it")
	}
}

func TestThemeDefaultsToSystem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("roots: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.ThemeMode() != ThemeSystem {
		t.Errorf("theme = %q, want %q", c.ThemeMode(), ThemeSystem)
	}
}

func TestThemeNormalizesInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("theme: neon\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.ThemeMode() != ThemeSystem {
		t.Errorf("invalid theme = %q, want fallback %q", c.ThemeMode(), ThemeSystem)
	}
}

func TestSetThemeModeRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetThemeMode(ThemeLight); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ThemeMode() != ThemeLight {
		t.Errorf("theme not persisted: %q", reloaded.ThemeMode())
	}
	// An unknown mode is coerced to the system default rather than persisted as-is.
	if err := c.SetThemeMode("bogus"); err != nil {
		t.Fatal(err)
	}
	if c.ThemeMode() != ThemeSystem {
		t.Errorf("bogus mode = %q, want %q", c.ThemeMode(), ThemeSystem)
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := ExpandPath("~"); got != home {
		t.Errorf("ExpandPath(~) = %q, want %q", got, home)
	}
	if got := ExpandPath("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("ExpandPath(~/x) = %q, want %q", got, filepath.Join(home, "x"))
	}
	if got := ExpandPath(""); got != "" {
		t.Errorf("ExpandPath(\"\") = %q, want empty", got)
	}
	if got := ExpandPath("/abs/path"); got != "/abs/path" {
		t.Errorf("ExpandPath(/abs/path) = %q", got)
	}
}
