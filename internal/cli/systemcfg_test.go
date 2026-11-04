package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeTestSDDMTheme(t *testing.T, dir string, metadata string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.desktop"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
	mainScript := "Main.qml"
	for _, line := range strings.Split(metadata, "\n") {
		if key, value, found := strings.Cut(line, "="); found && strings.TrimSpace(key) == "MainScript" {
			mainScript = strings.TrimSpace(value)
			break
		}
	}
	mainPath := filepath.Join(dir, filepath.Clean(mainScript))
	if err := os.MkdirAll(filepath.Dir(mainPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte("import QtQuick\nItem {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSDDMThemeDirRequiresMetadataAndMainScript(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "theme")
	makeTestSDDMTheme(t, dir, "[SddmGreeterTheme]\nName=Test\nMainScript=Main.qml\n")
	if err := validateSDDMThemeDir(dir); err != nil {
		t.Fatalf("valid theme rejected: %v", err)
	}

	if err := os.Remove(filepath.Join(dir, "Main.qml")); err != nil {
		t.Fatal(err)
	}
	if err := validateSDDMThemeDir(dir); err == nil {
		t.Fatal("theme without its configured MainScript must be rejected")
	}
}

func TestValidateSDDMThemeDirRejectsMainScriptTraversal(t *testing.T) {
	root := t.TempDir()
	theme := filepath.Join(root, "theme")
	makeTestSDDMTheme(t, theme, "[SddmGreeterTheme]\nName=Test\nMainScript=../outside.qml\n")
	if err := validateSDDMThemeDir(theme); err == nil {
		t.Fatal("theme MainScript traversal should be rejected")
	}
}

func TestValidateSDDMThemeDirRejectsSymlinkedTheme(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	makeTestSDDMTheme(t, real, "[SddmGreeterTheme]\nName=Test\nMainScript=Main.qml\n")
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := validateSDDMThemeDir(link); err == nil {
		t.Fatal("symlinked theme directory should be rejected")
	}
}

func TestSDDMThemeFingerprintChangesWhenQMLChanges(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "theme")
	makeTestSDDMTheme(t, dir, "[SddmGreeterTheme]\nName=Test\nMainScript=Main.qml\n")
	before := sddmThemeFingerprint(dir)
	if err := os.WriteFile(filepath.Join(dir, "Main.qml"), []byte("import QtQuick\nItem { objectName: \"changed\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := sddmThemeFingerprint(dir)
	if before == after {
		t.Fatal("fingerprint did not change after QML changed")
	}
}

func TestSDDMThemeLabelIncludesBuiltinFallback(t *testing.T) {
	if got := sddmThemeLabel(""); !strings.Contains(got, "embedded fallback") {
		t.Fatalf("unexpected empty fallback label: %q", got)
	}
	if got := sddmThemeLabel("breeze"); got != "theme breeze" {
		t.Fatalf("unexpected Breeze label: %q", got)
	}
}

func TestParseSDDMThemeVerificationRequiresManagedFileAndValidOutcome(t *testing.T) {
	valid := []byte("# Managed by Zacman.\n# Zacman theme verification: {\"desired\":\"/usr/share/sddm/themes/custom\",\"fingerprint\":\"abc\",\"outcome\":\"fallback\",\"fallback\":\"breeze\"}\n[Theme]\nCurrent=breeze\n")
	record, ok := parseSDDMThemeVerification(valid)
	if !ok || record.Desired != "/usr/share/sddm/themes/custom" || record.Fallback != "breeze" {
		t.Fatalf("valid fallback marker not parsed: record=%#v ok=%t", record, ok)
	}
	if _, ok := parseSDDMThemeVerification([]byte("# Zacman theme verification: {\"desired\":\"theme\",\"outcome\":\"approved\"}\n")); ok {
		t.Fatal("verification marker in an unmanaged SDDM config must not be trusted")
	}
	if _, ok := parseSDDMThemeVerification([]byte("# Managed by Zacman.\n# Zacman theme verification: not-json\n")); ok {
		t.Fatal("invalid verification JSON must be ignored")
	}
}

func TestValidateSDDMThemeDirSupportsMainScriptWithWhitespaceAroundEquals(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "theme")
	makeTestSDDMTheme(t, dir, "[SddmGreeterTheme]\nName=Test\nMainScript = Main.qml\n")
	if err := validateSDDMThemeDir(dir); err != nil {
		t.Fatalf("valid MainScript assignment with whitespace rejected: %v", err)
	}
}

func TestFallbackSDDMThemeUsesBreezeOnlyWhenValid(t *testing.T) {
	root := t.TempDir()
	if got := fallbackSDDMThemeUnder(root); got != "" {
		t.Fatalf("fallback should use SDDM embedded default when Breeze is missing, got %q", got)
	}
	makeTestSDDMTheme(t, filepath.Join(root, "breeze"), "[SddmGreeterTheme]\nName=Breeze\nMainScript=Main.qml\n")
	if got := fallbackSDDMThemeUnder(root); got != "breeze" {
		t.Fatalf("expected Breeze fallback when valid, got %q", got)
	}
}

func TestRenderSDDMThemeConfigContainsFallbackMarkerAndTheme(t *testing.T) {
	record := &sddmThemeVerification{Desired: "/usr/share/sddm/themes/custom", Fingerprint: "abc", Outcome: "fallback", Fallback: "breeze", Reason: "preview rejected"}
	data, err := renderSDDMThemeConfig("breeze", record)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "# Managed by Zacman.") || !strings.Contains(text, `"outcome":"fallback"`) || !strings.Contains(text, "Current=breeze") {
		t.Fatalf("rendered SDDM fallback configuration missing expected metadata:\n%s", text)
	}
	if _, err := renderSDDMThemeConfig("breeze\nExecStart=/bin/sh", nil); err == nil {
		t.Fatal("theme name with a newline should be rejected")
	}
}
