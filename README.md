# uxsm

Gestor de sesiones X11 para `systemd --user`: la versión X11 de
[uwsm](https://github.com/Vladimir-csp/uwsm).

Arranca el escritorio como servicio de `systemd --user`, le prepara el entorno y, al salir, apaga
la sesión entera y deja el gestor como estaba. Se lanza desde la entrada de sesión que elige el
display manager:

- `uxsm start bspwm.desktop` o `uxsm start -- bspwm`: arranca la sesión, de una entrada o de un
  comando.
- `uxsm stop`: la cierra.
- `uxsm entry bspwm`: genera la entrada de sesión de uxsm, `bspwm-uxsm.desktop`, y la instala en
  `/usr/local/share/xsessions`.
- `uxsm check` y `uxsm setup xsessions-dir`: comprueban y arreglan que el display manager lea ese
  directorio.

Cómo funciona por dentro: [`docs/internals.md`](docs/internals.md).

## Estructura

```text
cmd/uxsm/            el binario: reparto de subórdenes y cada suborden
internal/            el código de uxsm, por paquetes
data/                lo que se instala además del binario: unidades de systemd
docs/                documentación técnica
test/                compilación de los paquetes, pruebas de integración y recogida
                     de las entradas de sesión de cada distribución, en máquinas
                     virtuales
packaging/arch/      PKGBUILD para el AUR
packaging/debian/    directorio debian/ para Debian y Ubuntu
packaging/fedora/    .spec para Fedora
packaging/opensuse/  .spec y .changes para openSUSE
packaging/nix/       paquete para nixpkgs
flake.nix            el paquete de Nix compilado desde este directorio
.githooks/           hooks de git, que se activan con `make hooks`
Makefile             compilación e instalación, lo que llaman todos salvo Nix
```

El código que no sea el `main` irá en `internal/`: uxsm es un programa, no una biblioteca.

## Compilar e instalar

Hace falta Go 1.22 o posterior, la de Ubuntu 24.04 LTS. Sólo biblioteca estándar, así que los
paquetes compilan sin red.

`make build` pasa `go vet` antes de compilar ―[`docs/go-version.md`](docs/go-version.md)―.

```sh
make
sudo make install PREFIX=/usr
```

`DESTDIR`, `PREFIX` y `BINDIR` funcionan como de costumbre. `VERSION` sale de `git describe`;
al compilar desde un tarball, sin git, hay que pasarla.

## Paquetes

Todos salvo Nix llaman a `make build` y `make install`, así que un fichero nuevo que haya que
instalar se añade en el `Makefile`, en la lista de ficheros de cada paquete y en el
`postInstall` del de Nix.

- **Arch (AUR)**: `packaging/arch/PKGBUILD`. Al publicar, se copia al repositorio del AUR junto
  con `makepkg --printsrcinfo > .SRCINFO`.
- **Debian y Ubuntu**: `packaging/debian/` se copia a `debian/` en la raíz y se compila con
  `dpkg-buildpackage -us -uc`. Para Ubuntu se sube el paquete fuente a un PPA de Launchpad.
- **Fedora**: `packaging/fedora/uxsm.spec`, con `rpmbuild` o subido a COPR.
- **openSUSE**: `packaging/opensuse/`, en OBS. openSUSE lleva el changelog en `uxsm.changes`,
  no en el `.spec`.
- **NixOS**: `packaging/nix/package.nix` va a nixpkgs como `pkgs/by-name/ux/uxsm/package.nix`.
  Desde el repositorio, `nix build` compila el directorio actual con `flake.nix`.

`make test-vm` compila los paquetes de cada distribución dentro de máquinas virtuales, con las
recetas de `packaging/`, y ejecuta las pruebas de integración con ellos instalados.
`make release` lo hace en todas las distribuciones y deja el resultado en `releases/latest`
―[`docs/internals.md`](docs/internals.md#compilación-y-prueba-de-los-paquetes)―.

Todos descargan el tarball de la etiqueta `v<versión>` de GitHub. Al publicar una versión hay
que actualizar la suma del tarball donde la haya: `hash` en el de Nix, `sha256sums` en el
PKGBUILD.

## Licencia

[Apache-2.0](LICENSE).
