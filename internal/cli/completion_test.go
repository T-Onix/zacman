package cli

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestMakeBashCompletionSelfContainedAddsFallback(t *testing.T) {
	generated := "# generated\n__zacman_init_completion()\n{\n  _get_comp_words_by_ref \"$@\" cur prev words cword\n}\n"
	got, err := makeBashCompletionSelfContained(generated)
	if err != nil {
		t.Fatalf("makeBashCompletionSelfContained: %v", err)
	}
	if !strings.Contains(got, "if ! declare -F _get_comp_words_by_ref") {
		t.Fatal("generated script does not include the fallback helper")
	}
	if strings.Index(got, "if ! declare -F _get_comp_words_by_ref") > strings.Index(got, "__zacman_init_completion()") {
		t.Fatal("fallback helper must be defined before completion initialization")
	}
	if !strings.Contains(got, "words=(\"${COMP_WORDS[@]}\")") {
		t.Fatal("fallback helper does not populate the completion words array")
	}
}

func TestMakeBashCompletionSelfContainedIsIdempotent(t *testing.T) {
	generated := "# generated\n" + bashCompletionFallback + "\n__zacman_init_completion()\n{\n}\n"
	got, err := makeBashCompletionSelfContained(generated)
	if err != nil {
		t.Fatalf("makeBashCompletionSelfContained: %v", err)
	}
	if strings.Count(got, "if ! declare -F _get_comp_words_by_ref") != 1 {
		t.Fatal("fallback helper duplicated")
	}
}

func TestMakeBashCompletionSelfContainedRejectsUnknownCobraTemplate(t *testing.T) {
	if _, err := makeBashCompletionSelfContained("# unexpected Cobra script"); err == nil {
		t.Fatal("expected an error when Cobra's function marker is missing")
	}
}

func TestCompletionScriptsGenerateForAllSupportedShells(t *testing.T) {
	root := New("test").newCobraRoot()
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			var output bytes.Buffer
			if err := writeCompletion(root, shell, false, &output); err != nil {
				t.Fatalf("writeCompletion(%s): %v", shell, err)
			}
			script := output.String()
			if strings.TrimSpace(script) == "" {
				t.Fatal("completion script is empty")
			}
			if !strings.Contains(strings.ToLower(script), "zacman") {
				t.Fatal("completion script does not reference zacman")
			}
			if shell == "bash" {
				if !strings.Contains(script, "if ! declare -F _get_comp_words_by_ref") {
					t.Fatal("Bash completion must be self-contained")
				}
				check := exec.Command("bash", "-n")
				check.Stdin = strings.NewReader(script)
				if result, err := check.CombinedOutput(); err != nil {
					t.Fatalf("generated Bash completion has invalid syntax: %v\n%s", err, result)
				}
			}
		})
	}
}
