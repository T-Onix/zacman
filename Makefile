.PHONY: build test test-race vet fmt fmt-check check install uninstall clean

BINARY       := zacman
VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PREFIX       ?= /usr/local
SYSCONFDIR   ?= /etc
DESTDIR      ?=
GOFLAGS      ?= -mod=readonly
LDFLAGS      ?= -s -w -X main.version=$(VERSION)

build:
	@mkdir -p dist
	go build $(GOFLAGS) -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY) .

test:
	go test $(GOFLAGS) ./...

test-race:
	go test $(GOFLAGS) -race ./...

vet:
	go vet $(GOFLAGS) ./...

fmt:
	gofmt -w main.go $$(find internal -name '*.go' -type f)

fmt-check:
	@files="$$(gofmt -l main.go $$(find internal -name '*.go' -type f))"; \
	if [ -n "$$files" ]; then printf 'Go files need gofmt:\n%s\n' "$$files"; exit 1; fi

check: fmt-check test vet

install:
	@test -x dist/$(BINARY) || { echo 'Build first with: make build' >&2; exit 1; }
	install -Dm755 "dist/$(BINARY)" "$(DESTDIR)$(PREFIX)/bin/$(BINARY)"
	install -d -m755 "$(DESTDIR)$(SYSCONFDIR)/zacman"
	if [ ! -e "$(DESTDIR)$(SYSCONFDIR)/zacman/zacman.toml" ]; then \
		install -m644 examples/zacman.toml "$(DESTDIR)$(SYSCONFDIR)/zacman/zacman.toml"; \
	fi

uninstall:
	rm -f "$(DESTDIR)$(PREFIX)/bin/$(BINARY)"

clean:
	rm -rf dist
