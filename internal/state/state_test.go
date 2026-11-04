package state

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	if _, exists, err := Load(path); err != nil || exists {
		t.Fatalf("initial Load: exists=%v err=%v", exists, err)
	}
	if err := Save(path, State{Version: Version, ManagedPackages: []string{"zsh", "git", "git"}}); err != nil {
		t.Fatal(err)
	}
	got, exists, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("state should exist")
	}
	if !reflect.DeepEqual(got.ManagedPackages, []string{"git", "zsh"}) {
		t.Fatalf("unexpected normalized state: %#v", got.ManagedPackages)
	}
}

func TestLoadRejectsUnknownFieldsAndInvalidState(t *testing.T) {
	dir := t.TempDir()
	for i, raw := range []string{
		`{"version":1,"managed_packages":[],"surprise":true}`,
		`{"version":1}`,
		`{"version":1,"managed_packages":["-Rf"]}`,
	} {
		path := filepath.Join(dir, fmt.Sprintf("state-%d.json", i))
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Load(path); err == nil {
			t.Errorf("case %d: expected invalid-state error", i)
		}
	}
}

func TestLoadRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte(`{"version":1,"managed_packages":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(link); err == nil {
		t.Fatal("expected symlink refusal")
	}
}
