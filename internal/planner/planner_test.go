package planner

import (
	"reflect"
	"testing"
)

func TestInitialPlanNeverRemovesUnknownPackages(t *testing.T) {
	p := Build([]string{"firefox", "git"}, nil, []string{"old-tool", "git"}, false)
	if !reflect.DeepEqual(p.Install, []string{"firefox"}) {
		t.Fatalf("install = %#v", p.Install)
	}
	if len(p.Remove) != 0 {
		t.Fatalf("initial plan removed unknown packages: %#v", p.Remove)
	}
}

func TestPlanRemovesOnlyPreviouslyManagedPackages(t *testing.T) {
	p := Build([]string{"git"}, []string{"git", "firefox", "uninstalled"}, []string{"git", "firefox", "unrelated"}, true)
	if !reflect.DeepEqual(p.Remove, []string{"firefox"}) {
		t.Fatalf("remove = %#v", p.Remove)
	}
	if !reflect.DeepEqual(p.AlreadyInstalled, []string{"git"}) {
		t.Fatalf("installed = %#v", p.AlreadyInstalled)
	}
}

func TestBuildWithRetainedDoesNotRemoveForeignInventory(t *testing.T) {
	got := BuildWithRetained(
		[]string{},
		[]string{"old-repo-package", "migrated-foreign"},
		[]string{"old-repo-package", "migrated-foreign"},
		[]string{"migrated-foreign"},
		true,
	)
	if len(got.Remove) != 1 || got.Remove[0] != "old-repo-package" {
		t.Fatalf("unexpected removal plan: %#v", got.Remove)
	}
}
