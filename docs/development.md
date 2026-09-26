# Tocar el código de uxsm

Esto es para quien compila, prueba o empaqueta uxsm. Lo demás de `docs/` es para quien lo usa: [`architecture.md`](architecture.md) explica qué hace uxsm en la máquina y [`troubleshooting.md`](troubleshooting.md) qué hacer cuando algo no va.

## Compilación y pruebas

![Compilación, pruebas y release](flows/build-and-tests.svg)

[Fuente DOT](flows/build-and-tests.dot).

La diferencia esencial es el límite de la prueba:

| Orden | Dónde se ejecuta | Qué demuestra |
| --- | --- | --- |
| `make test` | Sistema actual; uxsm no se instala | Prueba funciones aisladas. |
| `make test-vm` | VMs desechables | Prueba el paquete instalado con X11 y `systemd --user`. |

Por tanto, `make test-vm` no es simplemente la misma prueba en otra distribución. Primero construye el paquete nativo en una VM y después lo instala en otra VM limpia, donde abre y cierra sesiones de prueba completas.

El proyecto sólo usa la biblioteca estándar de Go. Los recorridos habituales son:

```sh
make build             # go vet y bin/uxsm
make test              # pruebas unitarias
make test-vm           # Ubuntu por defecto
make test-vm DISTROS=pair
make test-vm DISTROS=all
make release           # todas las distribuciones y releases/latest
make test-vm FAST=1    # compilando y probando en la misma máquina
make test-nixos        # la máquina NixOS del flake
```

### Pruebas unitarias

`go test ./...` comprueba piezas sin privilegios ni una sesión gráfica real:

- reparto de subórdenes y distinción entre entrada y comando;
- lectura y resolución XDG de entradas `.desktop`, incluido el entrecomillado de `Exec=`;
- identidad, ficheros de runtime y carga de los ficheros de entorno;
- cálculo de cambios y restauración del entorno;
- generación y vuelta a leer de entradas;
- configuración de LightDM, SDDM y GDM sobre un sistema de ficheros falso;
- nombres de unidades, detección del bus de sesión y espera mediante `pidfd`.

### Pruebas de integración

[`test/release.sh`](../test/release.sh) crea un tarball del árbol actual, también con cambios aún no commiteados, y usa [`test/vm.sh`](../test/vm.sh) para trabajar en máquinas QEMU desechables. Para cada distribución se usan dos máquinas:

1. una compila el paquete nativo con la receta real de `packaging/`;
2. otra parte limpia, instala ese paquete con el gestor de la distribución y ejecuta las pruebas.

Las pruebas de `test/integration` usan Xvfb como servidor X y sustituyen al display manager por una unidad transitoria, salvo la última, que instala LightDM y abre la sesión con él:

| Script | Recorrido representativo |
| --- | --- |
| `01-desktop-service.sh` | Entrada y comando directo; el escritorio queda como proceso principal. |
| `02-session-shutdown.sh` | Salida del escritorio, muerte del proceso de login y `uxsm stop`. |
| `03-session-identity.sh` | `DesktopNames`, `-D`, `-e` y variables XDG. |
| `04-session-environment.sh` | Carga de `env*` y restauración exacta por los tres cierres. |
| `05-generated-entries.sh` | Instalación, sobrescritura, `check` y `setup`. |
| `06-session-ready.sh` | Las dos formas de estar lista, y la sesión que no llega a estarlo. |
| `07-xdg-autostart.sh` | Autostart XDG: con gestor de ventanas, con `--no-autostart`, el slice y en la entrada generada. |
| `08-display-manager.sh` | La sesión abierta por LightDM de verdad, con autologin sobre Xvfb. |
| `09-app.sh` | `uxsm app`: unidades, slices, entradas y acciones, y que se paran con la sesión. |

Las distribuciones cubiertas son Ubuntu 24.04, Debian 13, Arch, Fedora 43 y openSUSE Tumbleweed. `quick` usa Ubuntu; `pair`, Ubuntu y Arch; `all`, las cinco.

Con `FAST=1`, la misma máquina compila el paquete y ejecuta las pruebas con él instalado, lo que ahorra un arranque y una instalación de dependencias por distribución: medido en Ubuntu, 3:37 en vez de 4:07. A cambio se pierde lo que da la máquina limpia, que es donde se nota que a un paquete le falte declarar una dependencia de ejecución: allí sólo está instalado el paquete y lo que piden las pruebas. Por eso vale para trabajar y no para publicar; `make release` siempre usa las dos máquinas.

### NixOS

`make test-nixos` levanta una máquina NixOS declarada entera ―LightDM, autologin, bspwm y la entrada de sesión― con el sistema de pruebas de nixpkgs, y comprueba lo mismo que la prueba del display manager: que la sesión de uxsm arranca, que el proceso principal del servicio es el escritorio, que la identidad llega y que al parar el display manager se apaga todo. La fuente está en [`test/nixos/session.nix`](../test/nixos/session.nix) y se expone como `checks` del flake.

Hace falta Nix con su demonio en marcha, igual que las otras pruebas de máquinas necesitan QEMU. La versión de nixpkgs queda fijada en `flake.lock`, así que la máquina de prueba es la misma en cualquier sitio. Esta prueba sirve para lo que las demás no pueden: en NixOS nada está donde lo ponen las otras distribuciones, y el paquete de Nix instala las unidades por su cuenta.

### Integración continua

[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) ejecuta `make check` y `make build` en cada push, que es la misma lista que el hook `pre-commit`, y deja `make test-vm` a petición (`workflow_dispatch`), con las distribuciones como parámetro. Los runners de GitHub traen `/dev/kvm`, así que la tanda completa de una distribución ―compilar el paquete en una máquina e instalarlo en otra limpia, con las nueve pruebas de integración― tarda allí unos cuatro minutos.

El hook `pre-commit` ejecuta `gofmt`, `go vet`, las pruebas unitarias y `sh -n` sobre los scripts. El hook `pre-push` exige que los pushes a `main`, `master` o una etiqueta `vX.Y.Z` tengan un `make release` satisfactorio del mismo commit. Se activan con `make hooks`.

## Mapa del código

| Ruta | Papel |
| --- | --- |
| `cmd/uxsm` | CLI pública y subórdenes internas llamadas por las unidades. |
| `internal/desktopentry` | Búsqueda y lectura de sesiones X11. |
| `internal/session` | Identidad y ficheros de intercambio en runtime. |
| `internal/sessionenv` | Preparación y restauración del entorno. |
| `internal/sessionentry` | Tabla conocida y generación de entradas. |
| `internal/systemd` | Operaciones contra `systemd --user` y D-Bus. |
| `internal/dm` | Detección y configuración de display managers. |
| `internal/appunit` | Nombres, slices y órdenes de las unidades de `uxsm app`. |
| `internal/autostart` | El añadido que lleva el autostart XDG al slice de la sesión. |
| `internal/pidwait` | Espera de procesos ajenos mediante `pidfd`. |
| `internal/x11` | Conversación con el servidor X para saber si hay gestor de ventanas. |
| `data/systemd/user` | Plantillas de unidades instaladas. |
| `test` | Integración, VMs, paquetes y recogida de sesiones de distribuciones. |
| `packaging` | Recetas de paquetes usadas por las pruebas de release. |

## Versión de Go

### Por qué 1.22

Primero pusimos 1.24, la de Debian 13, y la bajamos a 1.22 por **Ubuntu 24.04 LTS**, que trae Go 1.22 y no compilaría nada con un mínimo más alto.

Versiones de Go de cada distribución, consultado el 2026-09-17:

| Distribución                              | Go        |
| ----------------------------------------- | --------- |
| Ubuntu 24.04 LTS                          | 1.22      |
| Debian 13 (trixie)                        | 1.24      |
| Ubuntu 26.04 LTS                          | 1.26      |
| Fedora 43                                 | 1.26      |
| Arch, openSUSE Tumbleweed, NixOS unstable | la última |

`.0` detrás no es capricho: `go 1.22` a secas es una versión del lenguaje, no una versión publicada de Go, y una Go 1.21 que intente descargar la toolchain que pide `go.mod` fallaría.

### go vet

`make build` pasa `go vet` antes de compilar. Con una Go más nueva que la de `go.mod`, `go build` acepta sin avisar funciones de la biblioteca estándar posteriores, y `go vet` las detecta.

### No disponible en 1.22

| Desde | Qué |
| --- | --- |
| 1.23 | iteradores: `range` sobre funciones, paquete `iter`, `slices.Collect`, `slices.Sorted`, `maps.Keys`, `maps.Values`; paquete `unique` |
| 1.24 | alias de tipos genéricos, `os.Root`, `strings.Lines`, `strings.SplitSeq`, `testing.B.Loop`, `omitzero` en `encoding/json` |
| 1.25 | `sync.WaitGroup.Go`, `testing/synctest` |

Lo que sí hay en 1.22 y apetece usar: `for i := range 10`, la variable del `for` nueva en cada vuelta, `min` y `max`, `slices` y `maps` sin iteradores, `math/rand/v2`, `log/slog`.
