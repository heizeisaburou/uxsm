# Single build and installation recipe. Every package except Nix calls
# `make build` and `make install`, so any new installed file must be added here
# and to postInstall in packaging/nix/package.nix as well.

PREFIX  ?= /usr/local
BINDIR  ?= $(PREFIX)/bin
# systemd --user reads units from both /usr/lib/systemd/user and
# /usr/local/lib/systemd/user, so either PREFIX works.
USERUNITDIR ?= $(PREFIX)/lib/systemd/user
MANDIR  ?= $(PREFIX)/share/man
DESTDIR ?=

# A tarball build has no git metadata, so packages pass VERSION.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0)

GO      ?= go
# No LDFLAGS: makepkg, dpkg-buildflags, and rpm already export it for the C linker.
GO_LDFLAGS ?=
# Distributions expect -trimpath and PIE; -mod=readonly prevents packaging from
# modifying go.mod, and GOTOOLCHAIN=local prevents another Go toolchain download.
GOFLAGS += -trimpath -buildmode=pie -mod=readonly
export GOFLAGS
export GOTOOLCHAIN = local
ifdef CGO_ENABLED
export CGO_ENABLED
endif

BIN := bin/uxsm
SRC := go.mod $(shell find . \( -name '*.go' -o -name '*.sh' \) -path './internal/*' -o -name '*.go' -path './cmd/*')
UNITS := $(wildcard data/systemd/user/*.in)
# The manual remains a .in source because installation must substitute @VERSION@
# and @BINDIR@ for each build and prefix.
MAN := $(wildcard data/man/*.1.in)

.PHONY: all build check test vet fmt install uninstall test-vm test-nixos release publish hooks dist bindist clean

all: build

build: $(BIN)

# A file target keeps `make install` immediately after `make build` from
# rebuilding with different options. vet runs first: a newer local Go silently
# compiles standard-library functions newer than go.mod, and only vet detects
# them; docs/development.md explains the compatibility check.
$(BIN): $(SRC)
	$(GO) vet ./...
	$(GO) build -ldflags "-X main.version=$(VERSION) $(GO_LDFLAGS)" -o $(BIN) ./cmd/uxsm

# Fast checks live in one place and are run by both the pre-commit hook and the
# GitHub workflow, avoiding two lists to maintain.
check:
	@unformatted=$$(gofmt -l cmd internal); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt'ed (make fmt):" >&2; \
		echo "$$unformatted" | sed 's/^/  /' >&2; \
		exit 1; \
	fi
	$(GO) vet ./...
	$(GO) test ./...
	@for f in $$(git ls-files --cached --others --exclude-standard '*.sh' .githooks 2>/dev/null); do \
		sh -n "$$f" || exit 1; \
	done
	@# Man pages: groff exits 0 even on warnings, so anything on stderr fails.
	@command -v groff >/dev/null 2>&1 || exit 0; \
	for f in $(MAN); do \
		out=$$(sed 's|@BINDIR@|$(BINDIR)|g;s|@VERSION@|$(VERSION)|g' "$$f" | groff -man -z -ww 2>&1 >/dev/null); \
		if [ -n "$$out" ]; then \
			echo "$$f:" >&2; \
			echo "$$out" | sed 's|^troff:<standard input>:|  line |' >&2; \
			exit 1; \
		fi; \
	done

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w .

# Units are generated at installation time, not build time: they contain the
# binary path, and packages build without PREFIX and pass it only when installing.
install: $(BIN)
	install -Dm755 $(BIN) $(DESTDIR)$(BINDIR)/uxsm
	install -d $(DESTDIR)$(USERUNITDIR)
	for f in $(UNITS); do \
		unit="$(DESTDIR)$(USERUNITDIR)/$$(basename "$$f" .in)"; \
		sed 's|@BINDIR@|$(BINDIR)|g' "$$f" > "$$unit" && chmod 644 "$$unit"; \
	done
	install -d $(DESTDIR)$(MANDIR)/man1
	for f in $(MAN); do \
		page="$(DESTDIR)$(MANDIR)/man1/$$(basename "$$f" .in)"; \
		sed 's|@BINDIR@|$(BINDIR)|g;s|@VERSION@|$(VERSION)|g' "$$f" > "$$page" && chmod 644 "$$page"; \
	done

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/uxsm
	for f in $(UNITS); do rm -f "$(DESTDIR)$(USERUNITDIR)/$$(basename "$$f" .in)"; done
	for f in $(MAN); do rm -f "$(DESTDIR)$(MANDIR)/man1/$$(basename "$$f" .in)"; done

# Integration tests use real packages in disposable VMs: test/release.sh builds
# each distribution's package on that distribution, copies it to build/release,
# and installs it on a clean VM to run test/integration/run.sh
# DISTROS is quick, pair, all, or a comma-separated list; only those
# distributions are built.
# FAST=1 builds and tests in the same VM. It is much faster, but no longer checks
# that the package declares runtime dependencies because build dependencies are
# already present. It is for development, not publishing.
DISTROS ?= quick
FAST ?=
test-vm:
	FAST=$(FAST) test/release.sh $(DISTROS)

# The NixOS test declares a complete machine—LightDM, autologin, bspwm, and the
# session entry—and starts it through the nixpkgs test framework. It requires Nix
# and its daemon; flake.lock pins nixpkgs for identical results on every machine.
NIX ?= nix
test-nixos:
	@command -v $(NIX) >/dev/null || { \
		echo "make test-nixos needs nix: install it and enable its daemon" >&2; \
		exit 1; \
	}
	$(NIX) build --extra-experimental-features "nix-command flakes" \
		'.#checks.$(shell $(NIX) eval --extra-experimental-features "nix-command flakes" --impure --raw --expr builtins.currentSystem).session' -L

# Run the same checks on every distribution and, if all pass, publish
# build/release as the releases/latest directory.
# NixOS goes first: it is the quickest of the two and the one that catches what
# the other five cannot, since nothing there sits where every other distribution
# puts it. It is not optional — a release is not a release without it — so a
# machine without nix fails here instead of publishing five sixths of the tests.
release: test-nixos
	test/release.sh --publish

# Publish a version: the tag, the push, and the GitHub release with the
# packages, both tarballs, and their checksums. TAG is the version, vX.Y.Z.
#   make publish TAG=v0.1.0 DRY_RUN=1   shows what it would do, changing nothing
TAG ?=
DRY_RUN ?=
publish:
	@[ -n "$(TAG)" ] || { echo "make publish needs the version: make publish TAG=v0.1.0" >&2; exit 2; }
	test/publish.sh $(if $(DRY_RUN),--dry-run,) $(TAG)

# Enable .githooks for this clone: pre-commit runs gofmt, vet, and unit tests;
# pre-push requires `make release` before pushing main, master, or a vX.Y.Z tag.
hooks:
	git config core.hooksPath .githooks

# A version tarball, identical to the one GitHub generates for a tag.
dist:
	mkdir -p dist
	git archive --format=tar.gz --prefix=uxsm-$(VERSION)/ -o dist/uxsm-$(VERSION).tar.gz HEAD

# The binary tarball: what someone with no uxsm package installs, with its own
# install.sh, needing neither Go nor a package manager. The binary is built
# without cgo, so it is static and does not depend on the glibc of the machine
# that built it.
ARCH ?= $(shell uname -m)
DISTDIR ?= dist
BINDIST := uxsm-$(VERSION)-linux-$(ARCH).tar.gz
bindist:
	rm -rf build/bindist
	mkdir -p build/bindist/uxsm-$(VERSION)-linux-$(ARCH)/systemd/user build/bindist/uxsm-$(VERSION)-linux-$(ARCH)/man $(DISTDIR)
	CGO_ENABLED=0 $(GO) build -ldflags "-X main.version=$(VERSION) $(GO_LDFLAGS)" \
		-o build/bindist/uxsm-$(VERSION)-linux-$(ARCH)/uxsm ./cmd/uxsm
	cp $(UNITS) build/bindist/uxsm-$(VERSION)-linux-$(ARCH)/systemd/user/
	cp $(MAN) build/bindist/uxsm-$(VERSION)-linux-$(ARCH)/man/
	cp data/install.sh LICENSE README.md build/bindist/uxsm-$(VERSION)-linux-$(ARCH)/
	echo $(VERSION) > build/bindist/uxsm-$(VERSION)-linux-$(ARCH)/version
	tar --create --directory build/bindist --owner=0 --group=0 --numeric-owner \
		--file=- uxsm-$(VERSION)-linux-$(ARCH) | gzip -n > $(DISTDIR)/$(BINDIST)
	@echo "$(DISTDIR)/$(BINDIST)"

clean:
	rm -rf bin build dist
