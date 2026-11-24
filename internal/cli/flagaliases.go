package cli

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// A flag keeps every alias it has always accepted, but only one spelling is
// shown in help and completion. The short form wins for Zacman-specific flags;
// help and version keep their long form.
var preferredFlagNames = map[string]string{
	"help":                  "--help",
	"version":               "--version",
	"yes":                   "-y",
	"default-file-config":   "-dfc",
	"add-existing-packages": "-axp",
	"dont-update":           "-du",
	"dedupe-config":         "-dc",
	"dry-run":               "--dry-run",
	"config":                "--config",
	"no-color":              "--no-color",
	"check":                 "--check",
	"no-descriptions":       "--no-descriptions",
}

var helpAliasRewrites = []struct{ from, to string }{
	{"  -h, --help", "      --help"},
	{"  -v, --version", "      --version"},
	{"  -y, --yes", "  -y"},
	{"      --default-file-config", "  -dfc"},
	{"      --add-existing-packages", "  -axp"},
	{"      --dont-update", "  -du"},
	{"      --dedupe-config", "  -dc"},
}

// aliasWriter rewrites the rendered flag list on its way to the terminal. It
// never changes parsing, so older scripts using --yes or --add-existing-packages
// keep working.
type aliasWriter struct {
	out io.Writer
	buf []byte
}

func (w *aliasWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := canonicalHelpLine(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
		if _, err := fmt.Fprintln(w.out, line); err != nil {
			return 0, err
		}
	}
	return n, nil
}

func (w *aliasWriter) Flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	line := canonicalHelpLine(string(w.buf))
	w.buf = nil
	_, err := io.WriteString(w.out, line)
	return err
}

func canonicalHelpLine(line string) string {
	for _, rewrite := range helpAliasRewrites {
		if !strings.HasPrefix(line, rewrite.from) {
			continue
		}
		prefix := rewrite.to
		if len(prefix) < len(rewrite.from) {
			prefix += strings.Repeat(" ", len(rewrite.from)-len(prefix))
		}
		return prefix + line[len(rewrite.from):]
	}
	return line
}

func (a *App) newCanonicalCompleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:                   fmt.Sprintf("%s [command-line]", cobra.ShellCompRequestCmd),
		Aliases:               []string{cobra.ShellCompNoDescRequestCmd},
		Short:                 "Request shell completion choices for the specified command-line",
		Hidden:                true,
		DisableFlagsInUseLine: true,
		DisableFlagParsing:    true,
		Args:                  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			toComplete := args[len(args)-1]
			proxy := a.newCobraRootWithCompletion(false)
			if strings.HasPrefix(toComplete, "-") && !strings.Contains(toComplete, "=") {
				comps := preferredFlagCompletions(proxy, args[:len(args)-1], toComplete)
				printShellCompletions(cmd, comps, cobra.ShellCompDirectiveNoFileComp)
				return nil
			}
			comps, directive := proxyShellCompletions(proxy, args)
			printShellCompletions(cmd, comps, directive)
			return nil
		},
	}
}

func preferredFlagCompletions(proxy *cobra.Command, args []string, toComplete string) []string {
	cmd, _, err := proxy.Find(args)
	if err != nil || cmd == nil {
		cmd = proxy
	}
	cmd.InitDefaultHelpFlag()
	if cmd == proxy {
		proxy.InitDefaultVersionFlag()
	}

	comps := []string{}
	seen := map[string]bool{}
	add := func(flag *pflag.Flag) {
		if flag.Hidden || flag.Deprecated != "" || flag.Changed {
			return
		}
		name, ok := preferredFlagNames[flag.Name]
		if !ok {
			name = "--" + flag.Name
		}
		if seen[name] || !strings.HasPrefix(name, toComplete) {
			return
		}
		seen[name] = true
		comps = append(comps, name+"\t"+flag.Usage)
	}
	cmd.Flags().VisitAll(add)
	return comps
}

func proxyShellCompletions(proxy *cobra.Command, args []string) ([]string, cobra.ShellCompDirective) {
	var out bytes.Buffer
	proxy.SetOut(&out)
	proxy.SetErr(io.Discard)
	proxy.SetArgs(append([]string{cobra.ShellCompRequestCmd}, args...))
	proxy.SilenceErrors = true
	proxy.SilenceUsage = true
	_ = proxy.Execute()

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	directive := cobra.ShellCompDirectiveDefault
	if len(lines) > 0 && strings.HasPrefix(lines[len(lines)-1], ":") {
		if value, err := strconv.Atoi(strings.TrimPrefix(lines[len(lines)-1], ":")); err == nil {
			directive = cobra.ShellCompDirective(value)
		}
		lines = lines[:len(lines)-1]
	}
	comps := []string{}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			comps = append(comps, line)
		}
	}
	return comps, directive
}

func printShellCompletions(cmd *cobra.Command, comps []string, directive cobra.ShellCompDirective) {
	noDescriptions := cmd.CalledAs() == cobra.ShellCompNoDescRequestCmd
	for _, comp := range comps {
		value := strings.SplitN(comp, "\n", 2)[0]
		if noDescriptions {
			value = strings.SplitN(value, "\t", 2)[0]
		}
		if value = strings.TrimSpace(value); value != "" {
			fmt.Fprintln(cmd.OutOrStdout(), value)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), ":%d\n", directive)
}
