#!/usr/bin/env bash
set -euo pipefail

# Publishes the binary using `go install module@version` without cloning the repo.
MODULE_PATH="${ZACMAN_MODULE:-github.com/T-Onix/zacman}"
VERSION="${ZACMAN_VERSION:-latest}"

if ! command -v go >/dev/null 2>&1; then
  printf 'error: Go is not installed or not on PATH\n' >&2
  exit 1
fi

go install "${MODULE_PATH}@${VERSION}"

GO_BIN="$(go env GOBIN)"
if [[ -z "${GO_BIN}" ]]; then
  GO_BIN="$(go env GOPATH)/bin"
fi
BINARY="${GO_BIN}/zacman"
if [[ ! -x "${BINARY}" ]]; then
  printf 'error: expected Zacman binary at %s after go install\n' "${BINARY}" >&2
  exit 1
fi

# `go install` cannot run post-install hooks. Bootstrap system config explicitly.
if [[ ! -e /etc/zacman/zacman.toml ]]; then
  if [[ "${EUID}" -eq 0 ]]; then
    "${BINARY}" init -dfc
  elif command -v sudo >/dev/null 2>&1; then
    sudo "${BINARY}" init -dfc
  elif command -v doas >/dev/null 2>&1; then
    doas "${BINARY}" init -dfc
  else
    printf 'error: Zacman was installed, but no sudo/doas was found to create /etc/zacman; run %q init -dfc as root\n' "${BINARY}" >&2
    exit 1
  fi
else
  printf 'Existing config preserved: /etc/zacman/zacman.toml\n'
fi

printf 'Zacman installed. Try: %s --help\n' "${BINARY}"
