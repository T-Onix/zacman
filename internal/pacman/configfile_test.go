package pacman

import (
	"strings"
	"testing"
)

func TestRenderILoveCandyIdempotentAndDisable(t *testing.T) {
	original := []byte("[options]\nArchitecture = auto\n\n[core]\nInclude = /etc/pacman.d/mirrorlist\n")
	got, err := RenderILoveCandy(original, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "[options]\n"+candyBegin+"\nILoveCandy\n"+candyEnd) {
		t.Fatalf("expected managed candy option, got:\n%s", got)
	}
	again, err := RenderILoveCandy(got, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(got) {
		t.Fatalf("sync is not idempotent:\n%s\nvs\n%s", got, again)
	}
	disabled, err := RenderILoveCandy(got, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disabled), "ILoveCandy") || strings.Contains(string(disabled), candyBegin) {
		t.Fatalf("expected candy setting removed, got:\n%s", disabled)
	}
	if !strings.Contains(string(disabled), "[core]\nInclude = /etc/pacman.d/mirrorlist") {
		t.Fatalf("unrelated config was changed:\n%s", disabled)
	}
}

func TestRenderILoveCandyRespectsExistingOption(t *testing.T) {
	original := []byte("[options]\nILoveCandy\n\n[core]\n")
	got, err := RenderILoveCandy(original, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), candyBegin) || strings.Count(string(got), "ILoveCandy") != 1 {
		t.Fatalf("existing option should not be duplicated:\n%s", got)
	}
}

func TestRenderMirrorlistPreservesUnmanagedEntries(t *testing.T) {
	original := []byte("## Existing local mirror\nServer = https://existing.example/$repo/os/$arch\n")
	got, err := RenderMirrorlist(original, []string{"https://new.example/$repo/os/$arch"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "Server = https://new.example/$repo/os/$arch") || !strings.Contains(text, "Server = https://existing.example/$repo/os/$arch") {
		t.Fatalf("expected managed and unmanaged mirrors:\n%s", text)
	}
	again, err := RenderMirrorlist(got, []string{"https://new.example/$repo/os/$arch"})
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(got) {
		t.Fatalf("mirror update not idempotent:\n%s\nvs\n%s", got, again)
	}
	removed, err := RenderMirrorlist(got, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(removed), mirrorBegin) || !strings.Contains(string(removed), "https://existing.example") {
		t.Fatalf("did not safely remove only managed mirror block:\n%s", removed)
	}
}

func TestRenderFunctionsRejectBrokenManagedBlocks(t *testing.T) {
	if _, err := RenderMirrorlist([]byte(mirrorBegin+"\nServer = https://bad.example/$repo/os/$arch\n"), nil); err == nil {
		t.Fatal("expected unterminated mirror block error")
	}
	if _, err := RenderILoveCandy([]byte("[options]\n"+candyEnd+"\n"), true); err == nil {
		t.Fatal("expected orphan candy block error")
	}
}

func TestRenderBlackarchIncludeRequiresExistingRepoAndIsIdempotent(t *testing.T) {
	original := []byte("[options]\nILoveCandy\n\n[core]\nInclude = /etc/pacman.d/mirrorlist\n\n[blackarch]\nSigLevel = Required DatabaseOptional\n")
	got, err := RenderBlackarchInclude(original, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), blackarchIncludeBegin+"\nInclude = "+DefaultBlackarchMirrors+"\n"+blackarchIncludeEnd) {
		t.Fatalf("expected BlackArch mirror include:\n%s", got)
	}
	again, err := RenderBlackarchInclude(got, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(got) {
		t.Fatalf("BlackArch include not idempotent:\n%s\nvs\n%s", got, again)
	}
	disabled, err := RenderBlackarchInclude(got, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disabled), blackarchIncludeBegin) || strings.Contains(string(disabled), DefaultBlackarchMirrors) {
		t.Fatalf("BlackArch include not removed:\n%s", disabled)
	}
	if _, err := RenderBlackarchInclude([]byte("[options]\n"), true); err == nil {
		t.Fatal("expected error if [blackarch] repository is not enabled")
	}
}

func TestRenderBlackarchMirrorlistUsesDedicatedMarkers(t *testing.T) {
	got, err := RenderBlackarchMirrorlist([]byte("# User comment\n"), []string{"https://mirror.example/blackarch/$repo/os/$arch"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), blackarchMirrorBegin) || !strings.Contains(string(got), "Server = https://mirror.example/blackarch/$repo/os/$arch") {
		t.Fatalf("unexpected BlackArch mirrorlist:\n%s", got)
	}
}

func TestHasBlackArchRepositoryInIgnoresCommentedSection(t *testing.T) {
	if HasBlackArchRepositoryIn([]byte("[options]\n#[blackarch]\nServer = https://example.invalid/$repo/os/$arch\n")) {
		t.Fatal("commented [blackarch] section must not count as enabled")
	}
	for _, input := range [][]byte{
		[]byte("[blackarch]\nServer = https://example.invalid/$repo/os/$arch\n"),
		[]byte("[options]\n\n[BlackArch]\nSigLevel = Required DatabaseOptional\n"),
	} {
		if !HasBlackArchRepositoryIn(input) {
			t.Fatalf("active BlackArch section not detected in:\n%s", input)
		}
	}
}
