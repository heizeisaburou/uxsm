# Cómo funciona uxsm

uxsm convierte una sesión X11 en un conjunto de unidades de `systemd --user`. El escritorio deja de ser un hijo opaco del display manager: pasa a ser el proceso principal de un servicio, comparte un entorno de sesión preparado de forma explícita y arrastra targets estándar de sesión gráfica. Cuando termina el escritorio o desaparece el proceso que abrió la sesión, systemd detiene el conjunto completo y uxsm restaura el entorno anterior.

Este documento explica el comportamiento observable y señala el código que lo implementa. Los diagramas muestran un recorrido representativo; no enumeran cada error ni cada combinación de opciones. Sus fuentes DOT están junto a los SVG en `docs/flows`.

## Inicio de una sesión

`uxsm start` admite dos fuentes:

```sh
uxsm start bspwm.desktop       # lee Exec= y DesktopNames= de la entrada
uxsm start -D bspwm -- bspwm   # recibe el comando directamente
```

Un único argumento terminado en `.desktop`, sin `--`, se interpreta como un ID de entrada. Se busca en `xsessions` dentro de los directorios de datos XDG; la primera coincidencia gana. Cualquier otra forma es un comando, y su ejecutable tiene que existir en `PATH`.

En ambos casos uxsm:

1. decide el ID de la instancia (`bspwm.desktop` para una entrada, `bspwm` para el comando);
2. calcula la identidad XDG de la sesión;
3. espera a que una sesión anterior de uxsm haya terminado también su limpieza; sólo mira las unidades de uxsm, porque `graphical-session.target` lo puede haber encendido otro ―el envoltorio de sesión de NixOS lo activa antes de ejecutar el `Exec=` de la entrada―;
4. guarda el entorno de login, la identidad y, si procede, el comando en `$XDG_RUNTIME_DIR/uxsm`;
5. arranca una unidad que vigila el PID entregado al display manager;
6. se sustituye por `systemctl --user start --wait uxsm-desktop@ID.service`.

![Flujo de arranque](flows/session-start.svg)

[Fuente DOT](flows/session-start.dot) · Código principal: [`cmd/uxsm/start.go`](../cmd/uxsm/start.go), [`cmd/uxsm/aux.go`](../cmd/uxsm/aux.go) y `internal/desktopentry`.

La plantilla del servicio no contiene el comando del escritorio. Ejecuta `uxsm aux exec %i`:

- con un ID terminado en `.desktop`, vuelve a leer el `Exec=` de la entrada;
- con un comando directo, lee el vector de argumentos guardado por `uxsm start`.

En el último paso `aux exec` usa `exec(2)`. El proceso principal del servicio pasa a ser el propio escritorio, de modo que systemd detecta su salida sin procesos intermediarios.

### Identidad

Sin `-e`, los nombres se combinan en este orden:

1. `XDG_CURRENT_DESKTOP` recibido del display manager;
2. `DesktopNames=` de la entrada;
3. los nombres de `-D`.

Se quitan duplicados. Si no queda ninguno, se usa como último recurso el nombre del ejecutable. Con `-e` se ignoran las fuentes anteriores y `-D` pasa a ser obligatorio.

Del resultado salen `XDG_CURRENT_DESKTOP`, `XDG_SESSION_DESKTOP`, `XDG_MENU_PREFIX` y `XDG_SESSION_TYPE=x11`. La implementación está en `internal/session`.

## Grafo de unidades y ciclo de vida

![Unidades y cierre](flows/systemd-lifecycle.svg)

[Fuente DOT](flows/systemd-lifecycle.dot) · Plantillas instaladas: `data/systemd/user`.

| Unidad | Responsabilidad |
| --- | --- |
| `uxsm-bindpid@PID.service` | Espera con `pidfd` al proceso que abrió la sesión. |
| `uxsm-env@ID.service` | Prepara el entorno antes del escritorio y lo restaura al parar. |
| `uxsm-desktop@ID.service` | Ejecuta el escritorio como proceso principal y espera a que la sesión esté lista. |
| `uxsm-session@ID.target` | Representa la sesión uxsm y arrastra `graphical-session.target`. |
| `uxsm-autostart@ID.target` | Arrastra `xdg-desktop-autostart.target` cuando el autostart le toca a uxsm. |
| `uxsm-shutdown.target` | Entra en conflicto con las unidades activas y coordina su cierre. |

La sesión se cierra por el mismo camino si:

- termina o falla el escritorio;
- el display manager mata el proceso que estaba esperando la sesión;
- alguien ejecuta `uxsm stop`.

Los dos primeros casos activan `uxsm-shutdown.target` mediante `OnSuccess=` y `OnFailure=`. `uxsm stop` activa ese target directamente. Sus conflictos paran el escritorio, los targets gráficos y el servicio de entorno; el `ExecStopPost=` de este último siempre intenta restaurar el estado previo.

### Cuándo la sesión está lista

El escritorio no cuenta como arrancado en cuanto empieza a ejecutarse. `uxsm-desktop@ID.service` lleva un `ExecStartPost=` que ejecuta `uxsm aux wait-ready`, y systemd no da el servicio por arrancado hasta que esa orden termina. Como los targets de la sesión van detrás del servicio, `uxsm-session@ID.target` y `graphical-session.target` esperan con él, y lo que arranque con la sesión gráfica encuentra un escritorio donde colocarse.

Hay dos formas de que la sesión se dé por lista, y valen lo mismo:

- **uxsm lo ve.** Es la comprobación de EWMH: la ventana raíz tiene `_NET_SUPPORTING_WM_CHECK` apuntando a una ventana del gestor de ventanas, y esa ventana tiene la misma propiedad apuntando a sí misma. Lo segundo distingue al gestor que está gobernando la pantalla de la marca que deja uno que murió de golpe. uxsm se lo pregunta al servidor X hablando el protocolo X11 desde `internal/x11`, sin libX11 ni `xprop`: es el saludo inicial y dos propiedades, y así la sesión no depende en tiempo de ejecución de ningún paquete de Xorg.
- **El escritorio lo dice.** `uxsm finalize`, como el `uwsm finalize` de uwsm, ejecutado por el escritorio desde su propia configuración. Sirve para un escritorio que no deje la marca de EWMH, o que prefiera decirlo más tarde.

Las dos encienden la misma señal, y sólo cuenta la primera: es un fichero en `$XDG_RUNTIME_DIR/uxsm/ready` creado con `O_EXCL`, así que encenderla es una sola operación del sistema y no hay dos arranques posibles. `uxsm finalize` con la señal ya encendida no es un error: lo dice y termina bien. La sesión que empieza la apaga primero, por si quedara encendida de una anterior que no llegó a limpiar.

Si no llega ninguna de las dos, `TimeoutStartSec=30` corta la espera: el servicio falla, su `OnFailure=` apaga la sesión y el display manager vuelve a la pantalla de inicio. Una sesión que no es un escritorio ―un solo programa X11, por ejemplo― puede quitar la espera con un fichero de anulación de la unidad:

```ini
# ~/.config/systemd/user/uxsm-desktop@.service.d/no-wait-ready.conf
[Service]
ExecStartPost=
```

### Autostart XDG

Detrás de la espera, el segundo `ExecStartPost=` del escritorio arranca `uxsm-autostart@ID.target`, que arrastra `xdg-desktop-autostart.target`. A partir de ahí el trabajo es de systemd: `systemd-xdg-autostart-generator` crea una `app-<nombre>@autostart.service` por cada entrada de autostart y las filtra por `OnlyShowIn=` y `NotShowIn=` con el `XDG_CURRENT_DESKTOP` del gestor. Las entradas arrancan, por tanto, con el escritorio ya en pantalla, y se paran con `graphical-session.target`.

uxsm lo lanza siempre, como uwsm: de una sesión gestionada por systemd se espera que las entradas de autostart arranquen solas. Quien no lo quiera, lo dice al arrancar la sesión, y `uxsm start` no mira qué escritorio es:

```sh
uxsm start --no-autostart bspwm.desktop
```

Quien se apoya en la tabla de escritorios conocidos es `uxsm entry`. La tabla dice, de cada sesión conocida, si lanza ella misma sus entradas de autostart. Es una pregunta sobre lo que hace, no sobre lo que es: la lanzan Xfce, GNOME, Plasma o MATE, por su gestor de sesión, y no la lanzan ni un gestor de ventanas ni una sesión con su propio fichero de arranque, como `icewm-session` con `~/.icewm/startup`, que es cosa aparte y no toca estas entradas. Cuando la tabla dice que sí, la opción va en el `Exec=` de la entrada generada:

```ini
Exec=uxsm start --no-autostart -D XFCE -- startxfce4
```

Hace falta porque ni el gestor de sesión del escritorio ni systemd comprueban si el otro ya ha lanzado una entrada: en Xfce, con los dos, cada una arranca dos veces. De un escritorio que la tabla no conoce, uxsm no supone nada: la entrada sale sin la opción, el autostart se lanza, y quien vea entradas duplicadas la añade.

La decisión se escribe en `$XDG_RUNTIME_DIR/uxsm/autostart`, y quien la mira es `uxsm aux autostart`: con la marca arranca el target, y sin ella no hace nada. Los dos rodeos tienen motivo. La decisión no puede ir en un `Condition*=` de la unidad, porque las dependencias de una unidad se resuelven al montar el trabajo, antes de comprobar sus condiciones: el autostart arrancaría igual en las sesiones en las que la unidad se salta. Y el target estándar no se puede arrancar directamente, porque lleva `RefuseManualStart=`; tiene que arrastrarlo una unidad propia.

## Entorno de la sesión

Los servicios de usuario heredan el entorno de `systemd --user`, no el del proceso que los arranca. Por eso `uxsm start` no puede limitarse a llamar a systemd: primero conserva lo que recibió del display manager y `uxsm-env@.service` lo monta en el gestor.

![Preparación y restauración del entorno](flows/environment.svg)

[Fuente DOT](flows/environment.dot) · Implementación: `internal/sessionenv`.

Durante la preparación:

1. se guarda en `env_pre` una foto filtrada del entorno de `systemd --user`;
2. el entorno de login se superpone a esa foto;
3. un cargador `/bin/sh`, empotrado en el binario, carga `/etc/profile`, `~/.profile`, la identidad y los ficheros de entorno de uxsm;
4. se calcula qué variables poner y quitar, y se guarda en `env_cleanup` qué pertenece a la sesión;
5. se actualiza el entorno de systemd y, si el bus usa `dbus-daemon`, también el de activación de D-Bus. Con `dbus-broker`, la activación ya se delega en systemd.

Los ficheros de uxsm se cargan de menor a mayor prioridad recorriendo `XDG_DATA_DIRS`, `XDG_CONFIG_DIRS` y `XDG_CONFIG_HOME`. En cada directorio se carga primero `uxsm/env`, después `uxsm/env-<escritorio>` por cada nombre de `XDG_CURRENT_DESKTOP`, y después de cada fichero su directorio `.d` en orden alfabético. Se ignoran copias y ejemplos como `*.bak`, `*.disabled` o `*.sample`.

Al cerrar, uxsm borra las variables creadas para la sesión, restaura todos los valores de `env_pre` y elimina los ficheros de trabajo. Variables de agentes SSH se conservan expresamente.

## Entradas de sesión y display managers

`uxsm entry` crea tres tipos de entrada:

```sh
uxsm entry bspwm                    # bspwm-uxsm.desktop → bspwm.desktop
uxsm entry --exec bspwm             # bspwm-uxsm.desktop → comando bspwm
uxsm entry --plain bspwm            # bspwm.desktop sin uxsm
uxsm entry --exec -- mywm --flag    # variante uxsm para un comando explícito
```

![Generación de entradas](flows/session-entries.svg)

[Fuente DOT](flows/session-entries.dot) · Implementación: [`cmd/uxsm/entry.go`](../cmd/uxsm/entry.go) e `internal/sessionentry`.

Una fuente puede ser una entrada existente, un comando o la tabla de escritorios conocidos. La tabla completa nombres, comentarios, `DesktopNames` y, cuando es portable entre distribuciones, el comando; y dice también qué sesiones lanzan su propio autostart XDG, para escribir `--no-autostart` en su `Exec=`. El generador rechaza entradas que ya usan uxsm, sesiones que ya arrancan el escritorio mediante `systemd --user` y metasesiones que sólo ejecutan el script personal del usuario.

Sin `-i`, la orden es una previsualización. Con `-i` escribe en `/usr/local/share/xsessions`; hace falta ejecutarla con permisos de root. No sobrescribe ni oculta otra entrada con el mismo ID sin `-f`.

No todos los display managers leen ese directorio:

- `uxsm check` identifica el display manager activo y enseña de dónde obtiene su lista;
- `uxsm setup xsessions-dir` calcula el cambio para LightDM y SDDM, y sólo lo aplica con `-i`;
- para GDM explica el cambio necesario en `XDG_DATA_DIRS`, pero no modifica su unidad.

Este código está aislado en `internal/dm` y se prueba contra árboles de configuración falsos, sin modificar el sistema que ejecuta las pruebas unitarias.

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
| `07-xdg-autostart.sh` | Autostart XDG: con gestor de ventanas, con `--no-autostart` y en la entrada generada. |
| `08-display-manager.sh` | La sesión abierta por LightDM de verdad, con autologin sobre Xvfb. |

Las distribuciones cubiertas son Ubuntu 24.04, Debian 13, Arch, Fedora 43 y openSUSE Tumbleweed. `quick` usa Ubuntu; `pair`, Ubuntu y Arch; `all`, las cinco.

### NixOS

`make test-nixos` levanta una máquina NixOS declarada entera ―LightDM, autologin, bspwm y la entrada de sesión― con el sistema de pruebas de nixpkgs, y comprueba lo mismo que la prueba del display manager: que la sesión de uxsm arranca, que el proceso principal del servicio es el escritorio, que la identidad llega y que al parar el display manager se apaga todo. La fuente está en [`test/nixos/session.nix`](../test/nixos/session.nix) y se expone como `checks` del flake.

Hace falta Nix con su demonio en marcha, igual que las otras pruebas de máquinas necesitan QEMU. La versión de nixpkgs queda fijada en `flake.lock`, así que la máquina de prueba es la misma en cualquier sitio. Esta prueba sirve para lo que las demás no pueden: en NixOS nada está donde lo ponen las otras distribuciones, y el paquete de Nix instala las unidades por su cuenta.

### Integración continua

[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) ejecuta `make check` y `make build` en cada push, que es la misma lista que el hook `pre-commit`, y deja `make test-vm` a petición (`workflow_dispatch`), con las distribuciones como parámetro. Los runners de GitHub traen `/dev/kvm`, así que la tanda completa de una distribución ―compilar el paquete en una máquina e instalarlo en otra limpia, con las ocho pruebas de integración― tarda allí unos cuatro minutos.

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
| `internal/pidwait` | Espera de procesos ajenos mediante `pidfd`. |
| `internal/x11` | Conversación con el servidor X para saber si hay gestor de ventanas. |
| `data/systemd/user` | Plantillas de unidades instaladas. |
| `test` | Integración, VMs, paquetes y recogida de sesiones de distribuciones. |
| `packaging` | Recetas de paquetes usadas por las pruebas de release. |
