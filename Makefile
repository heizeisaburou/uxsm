# Receta única de compilación e instalación. Todos los paquetes salvo Nix llaman a
# `make build` y `make install`, así que un fichero nuevo que haya que instalar se
# añade aquí y en el postInstall de packaging/nix/package.nix.

PREFIX  ?= /usr/local
BINDIR  ?= $(PREFIX)/bin
# systemd --user lee unidades tanto de /usr/lib/systemd/user como de
# /usr/local/lib/systemd/user, así que sirve con cualquiera de los dos PREFIX.
USERUNITDIR ?= $(PREFIX)/lib/systemd/user
DESTDIR ?=

# Al compilar desde un tarball no hay git: los paquetes pasan VERSION.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0)

GO      ?= go
# No LDFLAGS: makepkg, dpkg-buildflags y rpm ya la exportan para el enlazador de C.
GO_LDFLAGS ?=
# -trimpath y PIE son lo que esperan las distribuciones; -mod=readonly impide
# tocar go.mod durante un empaquetado, y GOTOOLCHAIN=local, descargar otra Go.
GOFLAGS += -trimpath -buildmode=pie -mod=readonly
export GOFLAGS
export GOTOOLCHAIN = local
ifdef CGO_ENABLED
export CGO_ENABLED
endif

BIN := bin/uxsm
SRC := go.mod $(shell find . \( -name '*.go' -o -name '*.sh' \) -path './internal/*' -o -name '*.go' -path './cmd/*')
UNITS := $(wildcard data/systemd/user/*.in)

.PHONY: all build test vet fmt install uninstall test-vm release hooks dist clean

all: build

build: $(BIN)

# Un objetivo de fichero, para que `make install` justo después de `make build`
# no vuelva a compilar con otras opciones. vet va antes: una Go local más nueva
# compila sin avisar funciones de la biblioteca estándar posteriores a go.mod, y
# sólo vet las detecta (docs/go-version.md).
$(BIN): $(SRC)
	$(GO) vet ./...
	$(GO) build -ldflags "-X main.version=$(VERSION) $(GO_LDFLAGS)" -o $(BIN) ./cmd/uxsm

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w .

# Las unidades se generan al instalar, no al compilar: llevan la ruta del
# binario, y los paquetes compilan sin PREFIX y lo pasan sólo al instalar.
install: $(BIN)
	install -Dm755 $(BIN) $(DESTDIR)$(BINDIR)/uxsm
	install -d $(DESTDIR)$(USERUNITDIR)
	for f in $(UNITS); do \
		unit="$(DESTDIR)$(USERUNITDIR)/$$(basename "$$f" .in)"; \
		sed 's|@BINDIR@|$(BINDIR)|g' "$$f" > "$$unit" && chmod 644 "$$unit"; \
	done

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/uxsm
	for f in $(UNITS); do rm -f "$(DESTDIR)$(USERUNITDIR)/$$(basename "$$f" .in)"; done

# Pruebas de integración en máquinas virtuales desechables, con los paquetes
# reales: test/release.sh compila el paquete de cada distribución en una máquina
# de esa distribución, lo trae a build/release y lo instala en una máquina limpia
# para ejecutar test/integration/run.sh. DISTROS es quick, pair, all o una lista con
# comas; sólo se compilan los paquetes de esas distribuciones.
DISTROS ?= quick
test-vm:
	test/release.sh $(DISTROS)

# Lo mismo en todas las distribuciones y, si todo pasa, build/release queda como
# releases/latest.
release:
	test/release.sh --publish

# Activa los hooks de .githooks en este clon: pre-commit pasa gofmt, vet y las
# pruebas unitarias; pre-push exige `make release` antes de subir a main, a
# master o una etiqueta vX.Y.Z.
hooks:
	git config core.hooksPath .githooks

# El tarball de una versión, el mismo que genera GitHub para una etiqueta.
dist:
	mkdir -p dist
	git archive --format=tar.gz --prefix=uxsm-$(VERSION)/ -o dist/uxsm-$(VERSION).tar.gz HEAD

clean:
	rm -rf bin build dist
