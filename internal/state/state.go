package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"syscall"
	"time"
)

const (
	DefaultPath = "/var/lib/zacman/state.json"
	Version     = 1
)

var packageNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._+-]*$`)

type State struct {
	Version         int       `json:"version"`
	ManagedPackages []string  `json:"managed_packages"`
	LastSuccessful  time.Time `json:"last_successful,omitempty"`
}

func Empty() State { return State{Version: Version, ManagedPackages: []string{}} }

func Load(path string) (State, bool, error) {
	if path == "" {
		path = DefaultPath
	}
	parentInfo, parentErr := os.Lstat(filepath.Dir(path))
	if parentErr == nil {
		if err := validateParentDirectory(filepath.Dir(path), parentInfo); err != nil {
			return State{}, false, err
		}
	}
	if parentErr != nil && !errors.Is(parentErr, os.ErrNotExist) {
		return State{}, false, fmt.Errorf("inspect state directory: %w", parentErr)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("inspect state file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return State{}, false, fmt.Errorf("refusing unsafe state path: %s", path)
	}
	if err := validateStateFile(path, info); err != nil {
		return State{}, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, false, fmt.Errorf("read state file: %w", err)
	}
	var value State
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return State{}, false, fmt.Errorf("parse state file %q: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return State{}, false, fmt.Errorf("parse state file %q: unexpected trailing JSON value", path)
		}
		return State{}, false, fmt.Errorf("parse state file %q: %w", path, err)
	}
	if value.Version != Version {
		return State{}, false, fmt.Errorf("state file version %d is unsupported (supported: %d)", value.Version, Version)
	}
	if value.ManagedPackages == nil {
		return State{}, false, fmt.Errorf("state file %q is missing a managed_packages array", path)
	}
	seen := make(map[string]struct{}, len(value.ManagedPackages))
	for _, pkg := range value.ManagedPackages {
		if !packageNameRE.MatchString(pkg) {
			return State{}, false, fmt.Errorf("state file %q contains invalid package name %q", path, pkg)
		}
		if _, exists := seen[pkg]; exists {
			return State{}, false, fmt.Errorf("state file %q contains duplicate package %q", path, pkg)
		}
		seen[pkg] = struct{}{}
	}
	return value, true, nil
}

func Save(path string, value State) error {
	if path == "" {
		path = DefaultPath
	}
	if value.Version == 0 {
		value.Version = Version
	}
	if value.Version != Version {
		return fmt.Errorf("cannot write unsupported state version %d", value.Version)
	}
	value.ManagedPackages = sorted(value.ManagedPackages)
	for _, pkg := range value.ManagedPackages {
		if !packageNameRE.MatchString(pkg) {
			return fmt.Errorf("invalid managed package name %q", pkg)
		}
	}
	if value.LastSuccessful.IsZero() {
		value.LastSuccessful = time.Now().UTC()
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect state directory: %w", err)
	}
	if err := validateParentDirectory(parent, parentInfo); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace unsafe state path: %s", path)
		}
		if err := validateStateFile(path, info); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect state path: %w", err)
	}
	tmp, err := os.CreateTemp(parent, ".zacman-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	if dir, err := os.Open(parent); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func validateParentDirectory(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refusing state path with unsafe parent directory: %s", path)
	}
	if os.Geteuid() == 0 {
		if !ownedByRoot(info) {
			return fmt.Errorf("privileged state directory is not owned by root: %s", path)
		}
		if info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("privileged state directory is group/world-writable: %s", path)
		}
	}
	return nil
}

func validateStateFile(path string, info os.FileInfo) error {
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("state file is group/world-writable: %s", path)
	}
	if os.Geteuid() == 0 && !ownedByRoot(info) {
		return fmt.Errorf("privileged state file is not owned by root: %s", path)
	}
	return nil
}

func ownedByRoot(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0
}

func sorted(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	unique := out[:0]
	for _, item := range out {
		if len(unique) == 0 || unique[len(unique)-1] != item {
			unique = append(unique, item)
		}
	}
	return unique
}
