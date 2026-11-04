package pacman

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInstallArgsUsesFullUpgradeAndNativeProgressDefaults(t *testing.T) {
	got := InstallArgs([]string{"firefox", "git"}, false, nil)
	want := []string{"-Syu", "--", "firefox", "git"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestExplicitInstallTargetsOverrideIgnoreList(t *testing.T) {
	got := InstallArgs([]string{"firefox", "git"}, false, []string{"git", "linux"})
	want := []string{"-Syu", "--ignore", "linux", "--", "firefox", "git"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestInstallArgsDontUpdateUsesOnlySyncInstall(t *testing.T) {
	got := InstallArgs([]string{"neovim"}, true, []string{"git"})
	want := []string{"-S", "--", "neovim"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestUpdateArgsHonorsIgnoreList(t *testing.T) {
	got := UpdateArgs([]string{"neovim"}, []string{"linux", "git"})
	want := []string{"-Syu", "--ignore", "linux,git", "--", "neovim"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestRemoveArgsDoesNotCascade(t *testing.T) {
	got := RemoveArgs([]string{"old-package"})
	want := []string{"-R", "--", "old-package"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestForeignPackagesTreatsEmptyExitOneAsNoForeignPackages(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-pacman")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := (Client{Binary: bin}).ForeignPackages()
	if err != nil {
		t.Fatalf("ForeignPackages() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ForeignPackages() = %#v, want no packages", got)
	}
}

func TestForeignPackagesKeepsRealErrors(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-pacman")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'local database is unreadable' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := (Client{Binary: bin}).ForeignPackages()
	if err == nil || !strings.Contains(err.Error(), "local database is unreadable") {
		t.Fatalf("ForeignPackages() error = %v, want local database error", err)
	}
}

func TestPassthroughPreservesNativeArgumentsOutputAndExitCode(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-pacman")
	capture := filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + capture + "'\nprintf 'native-pacman-output\\n'\nexit 7\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	client := Client{Binary: bin, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr}
	err := client.Passthrough("-Ss", "term with spaces", "--config", "/etc/pacman.conf")
	if err == nil {
		t.Fatal("expected exit status error")
	}
	var exitCoder interface{ ExitCode() int }
	if !errors.As(err, &exitCoder) || exitCoder.ExitCode() != 7 {
		t.Fatalf("expected preserved exit code 7, got %v", err)
	}
	got, readErr := os.ReadFile(capture)
	if readErr != nil {
		t.Fatal(readErr)
	}
	want := "-Ss\nterm with spaces\n--config\n/etc/pacman.conf\n"
	if string(got) != want {
		t.Fatalf("captured args = %q, want %q", got, want)
	}
	if stdout.String() != "native-pacman-output\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestExplicitNativePackagesUsesPacmanExplicitNativeFilter(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-pacman")
	capture := filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\nprintf '%s\n' \"$*\" > '" + capture + "'\nprintf 'neovim\ngit\n'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := (Client{Binary: bin}).ExplicitNativePackages()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"neovim", "git"}) {
		t.Fatalf("ExplicitNativePackages() = %#v", got)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(args)) != "-Qqen" {
		t.Fatalf("Pacman args = %q, want -Qqen", strings.TrimSpace(string(args)))
	}
}

func TestExplicitForeignPackagesUsesPacmanExplicitForeignFilter(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-pacman")
	capture := filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\nprintf '%s\n' \"$*\" > '" + capture + "'\nprintf 'yay\nlocal-tool\n'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := (Client{Binary: bin}).ExplicitForeignPackages()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"yay", "local-tool"}) {
		t.Fatalf("ExplicitForeignPackages() = %#v", got)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(args)) != "-Qqem" {
		t.Fatalf("Pacman args = %q, want -Qqem", strings.TrimSpace(string(args)))
	}
}

func TestExplicitQueryTreatsEmptyNoMatchesAsEmptyInventory(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-pacman")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := (Client{Binary: bin}).ExplicitForeignPackages()
	if err != nil {
		t.Fatalf("empty explicit foreign result should not fail: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty explicit foreign result = %#v, want []", got)
	}
}

func TestExplicitQueryDoesNotHideDatabaseErrors(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-pacman")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'local database is unreadable' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := (Client{Binary: bin}).ExplicitForeignPackages()
	if err == nil || !strings.Contains(err.Error(), "local database is unreadable") {
		t.Fatalf("ExplicitForeignPackages() error = %v, want the original diagnostic", err)
	}
}

func TestSatisfiedByInstalledProvidersUsesPacmanDeptestAndFiltersUnsatisfiedTargets(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-pacman")
	script := "#!/bin/sh\n" +
		"[ \"$1\" = \"-T\" ] || exit 9\nshift\n" +
		"for target in \"$@\"; do\n" +
		"  [ \"$target\" = \"not-installed\" ] && printf '%s\\n' \"$target\"\n" +
		"done\nexit 127\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := (Client{Binary: bin}).SatisfiedByInstalledProviders([]string{"exiftool", "xxd", "not-installed"})
	if err != nil {
		t.Fatalf("SatisfiedByInstalledProviders() error = %v", err)
	}
	want := []string{"exiftool", "xxd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SatisfiedByInstalledProviders() = %#v, want %#v", got, want)
	}
}

func TestSatisfiedByInstalledProvidersTreatsEmptySuccessfulDeptestAsSatisfied(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-pacman")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n[ \"$1\" = \"-T\" ] || exit 9\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := (Client{Binary: bin}).SatisfiedByInstalledProviders([]string{"exiftool", "xxd"})
	if err != nil {
		t.Fatalf("SatisfiedByInstalledProviders() error = %v", err)
	}
	if !reflect.DeepEqual(got, []string{"exiftool", "xxd"}) {
		t.Fatalf("SatisfiedByInstalledProviders() = %#v, want both targets", got)
	}
}

func TestSatisfiedByInstalledProvidersDoesNotHideUnexpectedQueryErrors(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-pacman")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'local database failure' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := (Client{Binary: bin}).SatisfiedByInstalledProviders([]string{"exiftool"})
	if err == nil || !strings.Contains(err.Error(), "local database failure") {
		t.Fatalf("SatisfiedByInstalledProviders() error = %v, want diagnostic", err)
	}
}

func TestInstallSyncedDoesNotUpgradeOrRefresh(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-pacman")
	capture := filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + capture + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (Client{Binary: bin}).InstallSynced([]string{"neovim", "git"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	want := "-S\n--needed\n--\nneovim\ngit\n"
	if string(got) != want {
		t.Fatalf("InstallSynced args = %q, want %q", got, want)
	}
}

func TestSyncDatabasesUsesOnlySy(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-pacman")
	capture := filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + capture + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (Client{Binary: bin}).SyncDatabases(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "-Sy" {
		t.Fatalf("SyncDatabases args = %q, want -Sy", got)
	}
}
