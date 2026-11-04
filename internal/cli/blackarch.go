package cli

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/T-Onix/zacman/internal/config"
	"github.com/T-Onix/zacman/internal/execx"
	"github.com/T-Onix/zacman/internal/pacman"
	"github.com/T-Onix/zacman/internal/transaction"
	"github.com/spf13/cobra"
)

const (
	blackArchStrapURL    = "https://blackarch.org/strap.sh"
	blackArchStrapSHA1   = "d338a4bb95d9e09f97508da68ac9e17d963b85f6"
	blackArchScriptLimit = 2 << 20
)

func (a *App) newRepoCommand() *cobra.Command {
	repo := &cobra.Command{
		Use:   "repo",
		Short: "Configure additional package repositories",
		Args:  cobra.NoArgs,
	}
	repo.AddCommand(&cobra.Command{
		Use:   "enable <repository>",
		Short: "Enable a repository using its supported setup process",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if !strings.EqualFold(args[0], "blackarch") {
				return fmt.Errorf("unsupported repository %q; currently supported: blackarch", args[0])
			}
			if err := requireRoot("enable the BlackArch repository"); err != nil {
				return err
			}
			unlock, err := transaction.AcquireLock("")
			if err != nil {
				return err
			}
			defer unlock()
			return a.enableBlackArchRepository()
		},
	})
	repo.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show supported third-party repository status",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if pacman.HasBlackArchRepository() {
				fmt.Fprintln(os.Stdout, "BlackArch repository: enabled")
			} else {
				fmt.Fprintln(os.Stdout, "BlackArch repository: not enabled")
			}
			return nil
		},
	})
	return repo
}

// enableBlackArchRepository invokes the official BlackArch strap script only
// after HTTPS retrieval and comparison against the checksum published by
// BlackArch's installation instructions. The script runs as root because its
// documented role is to install the keyring and edit Pacman's repository setup.
func (a *App) enableBlackArchRepository() error {
	if err := requireRoot("enable the BlackArch repository and install its keyring"); err != nil {
		return err
	}
	if pacman.HasBlackArchRepository() {
		fmt.Fprintln(os.Stdout, paint("mint", "✓ BlackArch repository is already enabled."))
		return nil
	}
	fmt.Fprintln(os.Stdout, paint("cyan", "→ Downloading and verifying the official BlackArch repository bootstrap script."))
	client := &http.Client{
		Timeout: 90 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects while downloading BlackArch strap script")
			}
			if req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Hostname(), "blackarch.org") {
				return errors.New("refusing redirect away from the official HTTPS blackarch.org host")
			}
			return nil
		},
	}
	request, err := http.NewRequest(http.MethodGet, blackArchStrapURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download official BlackArch strap script: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download official BlackArch strap script: HTTP %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, blackArchScriptLimit+1))
	if err != nil {
		return fmt.Errorf("read official BlackArch strap script: %w", err)
	}
	if len(body) == 0 || len(body) > blackArchScriptLimit {
		return fmt.Errorf("official BlackArch strap script has an invalid size (%d bytes)", len(body))
	}
	checksum := fmt.Sprintf("%x", sha1.Sum(body))
	if checksum != blackArchStrapSHA1 {
		return fmt.Errorf("BlackArch strap checksum mismatch: got %s, expected %s; refusing to execute it", checksum, blackArchStrapSHA1)
	}

	tmp, err := os.CreateTemp("", "zacman-blackarch-strap-*.sh")
	if err != nil {
		return fmt.Errorf("create temporary BlackArch bootstrap script: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0700); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, paint("lavender", "→ Running the verified BlackArch bootstrap script."))
	cmd := exec.Command("/bin/sh", tmpPath)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := execx.Run(cmd); err != nil {
		return fmt.Errorf("BlackArch bootstrap did not complete: %w", err)
	}
	if !pacman.HasBlackArchRepository() {
		return errors.New("BlackArch bootstrap finished without adding an active [blackarch] section to /etc/pacman.conf; inspect the output and the official BlackArch instructions")
	}
	fmt.Fprintln(os.Stdout, paint("mint", "✓ BlackArch repository and keyring setup completed."))
	return nil
}

func (a *App) ensureBlackArchForConfig(cfg config.Config) error {
	if len(cfg.Mirrors.BlackarchServers) == 0 || pacman.HasBlackArchRepository() {
		return nil
	}
	return a.enableBlackArchRepository()
}

func parseBlackArchBatch(values []string) ([]string, bool, error) {
	if len(values) == 0 {
		return nil, false, nil
	}
	prefixed := make([]string, 0, len(values))
	found := false
	for _, value := range values {
		if strings.HasPrefix(strings.ToLower(value), "blackarch:") {
			found = true
			name := strings.TrimSpace(value[len("blackarch:"):])
			if err := config.ValidatePackages([]string{name}); err != nil {
				return nil, true, fmt.Errorf("invalid BlackArch package %q: %w", name, err)
			}
			prefixed = append(prefixed, name)
		}
	}
	if !found {
		return nil, false, nil
	}
	if len(prefixed) != len(values) {
		return nil, true, errors.New("do not mix blackarch: packages with ordinary Pacman or other manager packages; run separate commands")
	}
	return config.SortedUnique(prefixed), true, nil
}
