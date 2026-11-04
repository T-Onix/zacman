package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestGhgrabAURAliasMatchesCanonicalInstalledPackage(t *testing.T) {
	if got := missingAURPackageTargets([]string{"ghgrab"}, []string{"ghgrab-bin"}); len(got) != 0 {
		t.Fatalf("installed ghgrab-bin was treated as missing: %#v", got)
	}
}

func TestGhgrabAURAliasInstallsCanonicalPackage(t *testing.T) {
	got := missingAURPackageTargets([]string{"ghgrab"}, nil)
	if !reflect.DeepEqual(got, []string{"ghgrab-bin"}) {
		t.Fatalf("missing AUR targets = %#v, want ghgrab-bin", got)
	}
}

func TestCanonicalAURPackagesDeduplicateLegacyAndCanonicalNames(t *testing.T) {
	got := canonicalAURPackages([]string{"ghgrab", "ghgrab-bin"})
	if !reflect.DeepEqual(got, []string{"ghgrab-bin"}) {
		t.Fatalf("canonical AUR targets = %#v, want one ghgrab-bin", got)
	}
}

func TestLookupAURPackagesUsesExactRPCv5Names(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc/v5/info" {
			t.Errorf("RPC path = %q", r.URL.Path)
		}
		if got := r.URL.Query()["arg[]"]; !reflect.DeepEqual(got, []string{"neovim-nightly", "some-tool"}) {
			t.Errorf("requested names = %#v", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":5,"type":"multiinfo","resultcount":2,"results":[{"Name":"neovim-nightly"},{"Name":"not-requested"}]}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: time.Second}
	got, err := lookupAURPackagesAt(context.Background(), client, server.URL, []string{"some-tool", "neovim-nightly"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"neovim-nightly": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AUR result = %#v, want %#v", got, want)
	}
}

func TestAURInitInstallArgsNeverTriggerFullUpgrade(t *testing.T) {
	got := aurInstallArgs([]string{"tool-a", "tool-b"}, false)
	want := []string{"-S", "--needed", "tool-a", "tool-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("init AUR args = %#v, want %#v", got, want)
	}
	full := aurInstallArgs([]string{"tool-a"}, true)
	if !reflect.DeepEqual(full, []string{"-Syu", "--needed", "tool-a"}) {
		t.Fatalf("explicit full-upgrade AUR args = %#v", full)
	}
}
