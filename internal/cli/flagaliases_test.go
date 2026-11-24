package cli

import (
	"strings"
	"testing"
)

func TestCanonicalHelpLineShowsOneFlagSpelling(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"  -h, --help   help for init", "      --help   help for init"},
		{"  -v, --version   version for zacman", "      --version   version for zacman"},
		{"  -y, --yes   Skip confirmation", "  -y          Skip confirmation"},
		{"      --add-existing-packages   Import packages", "  -axp                          Import packages"},
		{"      --dry-run   Preview", "      --dry-run   Preview"},
	}
	for _, tc := range cases {
		if got := canonicalHelpLine(tc.line); got != tc.want {
			t.Errorf("canonicalHelpLine(%q) = %q; want %q", tc.line, got, tc.want)
		}
	}
}

func TestPreferredFlagCompletionsUseShortForms(t *testing.T) {
	proxy := New("test").newCobraRootWithCompletion(false)
	comps := preferredFlagCompletions(proxy, []string{"init"}, "-")

	want := []string{"-axp", "--config", "-dc", "-dfc", "-du", "--dry-run", "--help", "--no-color", "-y"}
	got := []string{}
	for _, comp := range comps {
		got = append(got, strings.SplitN(comp, "\t", 2)[0])
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("flag completions = %v; want %v", got, want)
	}

	partial := preferredFlagCompletions(proxy, []string{"init"}, "-a")
	if len(partial) != 1 || !strings.HasPrefix(partial[0], "-axp\t") {
		t.Fatalf("completion for -a = %v; want only -axp", partial)
	}
}
