package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetINIValuePreservesExistingSettingsAndComments(t *testing.T) {
	input := []byte("# keep this note\n[Mouse]\ncursorTheme=OldTheme # preserve inline note\ncursorSize=24\n\n[Other]\nfoo=bar\n")
	got := string(setINIValue(input, "Mouse", "cursorTheme", "Breeze_Light"))
	for _, want := range []string{"# keep this note", "cursorTheme=Breeze_Light # preserve inline note", "cursorSize=24", "[Other]", "foo=bar"} {
		if !strings.Contains(got, want) {
			t.Errorf("updated config missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "cursorTheme=") != 1 {
		t.Fatalf("duplicate cursorTheme key:\n%s", got)
	}
}

func TestSetINIValueAddsMissingKeyToExistingSection(t *testing.T) {
	input := []byte("[Mouse]\nacceleration=1\n\n[Other]\nfoo=bar\n")
	got := string(setINIValue(input, "Mouse", "cursorSize", "32"))
	if !strings.Contains(got, "[Mouse]\nacceleration=1\ncursorSize=32\n") || !strings.Contains(got, "[Other]\nfoo=bar") {
		t.Fatalf("mouse key wasn't added to the correct section:\n%s", got)
	}
}

func TestCursorThemeAndEnvironmentUpsertsAreIdempotent(t *testing.T) {
	data := setINIValue(nil, "Icon Theme", "Inherits", "Breeze_Light")
	if again := setINIValue(data, "Icon Theme", "Inherits", "Breeze_Light"); string(again) != string(data) {
		t.Fatalf("INI setting isn't idempotent: %q != %q", again, data)
	}
	env := setEnvironmentValue([]byte("# preserve\nEDITOR=nvim\n"), "XCURSOR_THEME", "Breeze_Light")
	env = setEnvironmentValue(env, "XCURSOR_SIZE", "32")
	text := string(env)
	for _, want := range []string{"# preserve", "EDITOR=nvim", "XCURSOR_THEME=Breeze_Light", "XCURSOR_SIZE=32"} {
		if !strings.Contains(text, want) {
			t.Errorf("environment file missing %q:\n%s", want, text)
		}
	}
}

func TestValidateCursorThemeDirRequiresRealCursorAssets(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "Good")
	if err := os.MkdirAll(filepath.Join(good, "cursors"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "cursors", "left_ptr"), []byte("cursor"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "index.theme"), []byte("[Icon Theme]\nName=Good\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateCursorThemeDir(good); err != nil {
		t.Fatalf("valid theme rejected: %v", err)
	}

	missingAssets := filepath.Join(root, "MissingAssets")
	if err := os.MkdirAll(missingAssets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateCursorThemeDir(missingAssets); err == nil {
		t.Fatal("theme without cursors directory should be rejected")
	}

	symlink := filepath.Join(root, "Symlink")
	if err := os.Symlink(good, symlink); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := validateCursorThemeDir(symlink); err == nil {
		t.Fatal("symlinked theme directory should be rejected")
	}
}
