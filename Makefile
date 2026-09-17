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
SRC := go.mod $(shell find . -name '*.go' -not -path './.gocache/*' -not -path './.private/*')
UNITS := $(wildcard data/systemd/user/*.in)

.PHONY: all build test vet fmt install uninstall test-vm dist clean

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

# Pruebas de integración en máquinas virtuales desechables: instala uxsm en un
# árbol aparte, lo sube a cada máquina con las pruebas y ejecuta
# test/integration/run.sh. DISTROS es quick, pair, all o una lista con comas.
# Sin cgo, para que el binario no dependa de la glibc de este sistema.
DISTROS ?= quick
VM_STAGE := build/vm
test-vm:
	rm -rf $(VM_STAGE)
	$(MAKE) install BIN=$(VM_STAGE)/uxsm DESTDIR=$(CURDIR)/$(VM_STAGE)/root PREFIX=/usr CGO_ENABLED=0
	cp -r test/integration $(VM_STAGE)/integration
	UXSM_VM_UPLOAD=$(VM_STAGE) test/vm.sh $(DISTROS) 'sh ~/uxsm/integration/run.sh'

# El tarball de una versión, el mismo que genera GitHub para una etiqueta.
dist:
	mkdir -p dist
	git archive --format=tar.gz --prefix=uxsm-$(VERSION)/ -o dist/uxsm-$(VERSION).tar.gz HEAD

clean:
	rm -rf bin build dist
