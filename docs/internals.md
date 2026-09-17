# Cómo funciona uxsm por dentro

Documentación técnica: qué hace cada parte y por qué está hecha así. Crece con cada paso del desarrollo; lo que todavía no existe no aparece aquí.

## La idea

Un display manager como LightDM arranca una sesión X11 ejecutando el `Exec=` de una entrada de `/usr/share/xsessions/`, y da la sesión por terminada cuando ese proceso termina. Normalmente el proceso es el propio escritorio: `bspwm`, `startxfce4`.

uxsm se mete en medio. La entrada de sesión ejecuta `uxsm start bspwm.desktop`, y uxsm lanza el escritorio como **servicio de `systemd --user`** en lugar de como hijo suyo. Es lo que hace uwsm con los compositores de Wayland, y lo que permite que el resto ―entorno, `graphical-session.target`, autostart, limpieza al salir― lo resuelvan las dependencias entre unidades en vez de código.

Antes de escribir código se comprobó a mano que un escritorio X11 aguanta así: bspwm y Xfce funcionan como servicio, polkit sigue permitiendo apagar y suspender aunque el proceso quede fuera de la sesión de logind, y al salir no queda nada.

## Línea de órdenes

Las subórdenes van en **grupos** (`group` en `cmd/uxsm/main.go`), cada uno con su lista y su ayuda, y todos reparten con el mismo código (`dispatch`):

- **`uxsm`**: `start`, `stop`, `version` y `aux`. `aux` está marcada como oculta: la llaman las unidades de systemd, no las personas, así que no sale en la ayuda general, pero se ejecuta igual que las demás.
- **`uxsm aux`**: `exec` y `waitpid`, lo que ejecutan las unidades en sus `Exec*=`.

Un grupo sólo mira la primera palabra: si es una suborden, le pasa el resto; si es `help` o una forma de pedir ayuda, enseña la ayuda del grupo; si no hay palabra o no se conoce, enseña la ayuda por la salida de error. Por eso `uxsm foo` y `uxsm aux foo` se comportan igual. Cada suborden final lee sus opciones con su propio `flag.FlagSet`, así que cada una tiene su ayuda.

**La ayuda se pide en cualquier posición** (`parseFlags`). `flag` deja de leer opciones en el primer argumento que no lo es, y por sí solo no vería el `-h` de `uxsm start bspwm.desktop -h`. Antes de leer opciones se mira toda la línea hasta `--`, y si aparece `-h`, `-help`, `--h` o `--help`, se enseña la ayuda de esa suborden. Así se puede añadir `-h` al final de una orden a medio escribir, ver la ayuda y seguir escribiendo. Lo que va detrás de `--` es del programa que se lanza, no de uxsm.

Las cuatro formas son las que reconoce el paquete `flag`, y valen igual en cualquier sitio (`isHelpFlag`): `uxsm -help` y `uxsm start bspwm.desktop -help`. Además, `uxsm help` enseña la ayuda general, pero `help` sólo cuenta como primera palabra, porque en otra posición podría ser un argumento de verdad.

**Códigos de salida**: 0 si va bien o se ha pedido ayuda, 2 si los argumentos están mal y 1 con cualquier otro error. La ayuda pedida sale por la salida estándar; la que acompaña a un error de argumentos, por la de error.

## Antes de uxsm: el login

Cuando `uxsm start` se ejecuta, el proceso ya corre como el usuario y el gestor `systemd --user` ya está en marcha. Todo eso ocurre durante el login, antes de que arranque uxsm.

### Qué pasa desde que escribes la contraseña

```text
systemd (PID 1, el del sistema)
 └─ lightdm.service                        corre como root
     │
     │ 1. escribes la contraseña en el greeter
     │ 2. LightDM te autentica con PAM
     │ 3. PAM abre la sesión ── pam_systemd ──▶ systemd-logind
     │                                           ├─ crea /run/user/1000  (XDG_RUNTIME_DIR)
     │                                           ├─ crea la sesión: session-N.scope en user-1000.slice
     │                                           └─ si es tu primera sesión, arranca user@1000.service
     │                                               └─ systemd --user   ← el gestor de usuario, ya como tú
     │
     └─ 4. «session child»: un hijo de LightDM que deja de ser root y pasa a ser tu usuario
           └─ Xsession (el wrapper de sesión)
               └─ uxsm start bspwm.desktop    ← nace ya como tu usuario
```

1. **LightDM corre como root:** es un servicio del sistema, y necesita serlo para autenticar a cualquiera.
2. **Autenticación con PAM:** comprueba la contraseña.
3. **PAM abre la sesión, y ahí entra `pam_systemd`.** En Arch está en `/etc/pam.d/system-login`, que usa LightDM. Según su manual, al entrar se encarga de tres cosas junto con `systemd-logind`:
   - **crear `/run/user/1000`** si no existe, con tu usuario como dueño. Es tu `XDG_RUNTIME_DIR`;
   - **registrar la sesión:** un scope `session-N.scope` dentro de tu slice de usuario;
   - **si es tu primera sesión abierta, arrancar `user@1000.service`.** Ese servicio del gestor del sistema ejecuta `systemd --user` **como tu usuario**; `systemctl status user@1000.service` lo enseña como `User Manager for UID 1000`.
4. **LightDM crea un proceso hijo para la sesión,** que suelta los privilegios de root y pasa a ser tu usuario. Ese hijo ejecuta el wrapper de sesión, y el wrapper ejecuta el `Exec=` de la entrada: `uxsm start`. **Por eso uxsm ya nace siendo tú:** el cambio de usuario ocurrió antes, en su padre.

## Arranque de una sesión

```text
LightDM
 └─ uxsm start bspwm.desktop              (1) busca la entrada, importa DISPLAY
	 ├─ systemctl --user start uxsm-bindpid@<PID>.service
	 │                                     (2) vigila su propio PID
	 └─ exec systemctl --user start --wait uxsm-desktop@bspwm.desktop.service
										   (3) mismo PID: LightDM y bindpid vigilan este proceso

systemd --user
 ├─ uxsm-bindpid@<PID>.service
 │   └─ uxsm aux waitpid <PID>
 ├─ uxsm-desktop@bspwm.desktop.service
 │   └─ uxsm aux exec bspwm.desktop        (4) vuelve a leer la entrada
 │       └─ exec bspwm                     (5) mismo PID: el servicio es el escritorio
 ├─ uxsm-session@bspwm.desktop.target      (6) lo arrastra el escritorio
 └─ graphical-session.target               (6) lo arrastra la sesión
```

1. **`uxsm start`** (`cmd/uxsm/start.go`) comprueba que la entrada existe y que su ID sirve como instancia de unidad. Lo hace antes de tocar systemd, para que un error salga en el acto y no dentro de un servicio. Después **importa `DISPLAY` y `XAUTHORITY`** al gestor de systemd: un servicio no hereda el entorno de quien lo arranca, sino el del gestor, y sin esas dos variables el escritorio no encuentra el servidor X que ha arrancado LightDM.
2. **Arranca `uxsm-bindpid@<PID>.service` con su propio PID**, para que la sesión se apague si LightDM mata este proceso (ver [Fin de la sesión](#fin-de-la-sesión)).
3. **Se sustituye por `systemctl --user start --wait`** con `exec`. El PID no cambia, así que el proceso que vigilan LightDM y bindpid pasa a ser ese `systemctl`, que no vuelve hasta que la unidad termina. La sesión dura, por construcción, lo mismo que el escritorio.
4. **La unidad `uxsm-desktop@.service`** ejecuta `uxsm aux exec %i`. La instancia es el ID de la entrada, así que la unidad no necesita saber el comando: `aux exec` vuelve a leer la entrada.
5. **`aux exec` se sustituye por el `Exec=` de la entrada**, otra vez con `exec`. El proceso principal del servicio es el propio escritorio, y cuando el escritorio termina, termina el servicio.
6. **El escritorio arrastra la sesión.** `uxsm-desktop@` tiene `BindsTo=uxsm-session@%i.target`, y ese target, `BindsTo=graphical-session.target`, el target estándar de systemd que significa «hay una sesión gráfica». `graphical-session.target` no se puede arrancar a mano (`RefuseManualStart`); sólo llega activo porque algo lo arrastra, y aquí lo arrastra la sesión.

Pasar sólo el ID tiene dos ventajas: las unidades son ficheros fijos que instala el paquete, sin generar nada en tiempo de ejecución, y `systemctl --user status` enseña `uxsm-desktop@bspwm.desktop.service`, que se entiende de un vistazo.

## Fin de la sesión

La sesión puede terminar por tres caminos, y los tres acaban igual: en `uxsm-shutdown.target`.

| Cómo termina | Qué lo detecta | Qué arranca el apagado |
| --- | --- | --- |
| Se sale del escritorio (`bspc quit`, cerrar sesión en Xfce) | Termina el proceso principal de `uxsm-desktop@` | Su `OnSuccess=`, o `OnFailure=` si terminó con error |
| El display manager mata el proceso de la sesión | `uxsm-bindpid@<PID>` ve terminar el PID | Su `OnSuccess=` |
| `uxsm stop` | ― | Lo arranca directamente (`cmd/uxsm/stop.go`) |

**`uxsm-shutdown.target`** tiene `Conflicts=` con `graphical-session.target`, y cada unidad de uxsm tiene `Conflicts=uxsm-shutdown.target`. Al arrancarlo, systemd para todo lo que choca con él: el escritorio, la sesión, `graphical-session.target` y la vigilancia del PID. Los `OnSuccess=` usan `OnSuccessJobMode=replace-irreversibly` para que ningún otro trabajo pendiente pueda cancelar el apagado. Como el target tiene `StopWhenUnneeded=yes`, él mismo se para en cuanto ha hecho su trabajo, y no se queda activo para la siguiente sesión.

Además, parar el escritorio para también la sesión y `graphical-session.target` por `PropagatesStopTo=`, sin esperar al target de apagado.

Esta estructura está copiada de uwsm: `wayland-wm@.service`, `wayland-session@.target`, `wayland-session-bindpid@.service` y `wayland-session-shutdown.target`.

### Esperar a un PID que no es hijo

`uxsm aux waitpid` (`internal/pidwait`) tiene que esperar a un proceso que no es hijo suyo: es hijo del display manager. `waitpid(2)` sólo sirve con hijos, así que usa **`pidfd_open(2)`** (Linux 5.3): da un descriptor de fichero para cualquier proceso, y ese descriptor se vuelve legible cuando el proceso termina. Basta con esperar a que lo sea con `select`, sin preguntar en bucle. Si el proceso ya no existe, `pidfd_open` devuelve `ESRCH` y la espera termina en el acto.

El paquete `syscall` de Go no le pone nombre a `pidfd_open` en la mayoría de arquitecturas, así que se llama por su número, 434: las llamadas añadidas desde Linux 5.1 tienen el mismo número en todas. Es lo mismo que hacen `uwsm aux waitpid` y el comando `waitpid` de util-linux, que Ubuntu 24.04 no trae; por eso uxsm no depende del comando.

## Entradas de sesión

`internal/desktopentry` lee las entradas según la _Desktop Entry Specification_, pero sólo lo que uxsm usa.

- **Dónde se buscan** (`Find`): en el subdirectorio `xsessions` de cada directorio de datos XDG, por orden de preferencia (`internal/xdg`): primero `XDG_DATA_HOME`, después cada uno de `XDG_DATA_DIRS`. La primera que aparece gana, así que una entrada del usuario tapa a la del sistema con el mismo ID.
- **Qué es un ID válido**: el nombre de un fichero terminado en `.desktop`, sin barras. Sin barras para que un ID no pueda salirse del directorio de entradas.
- **Qué se lee**: el grupo `[Desktop Entry]` y, de él, `Name=`, `Exec=` y `DesktopNames=`. Las traducciones (`Name[es]=`) y los grupos de acciones se ignoran. Una entrada sin `Exec=` es un error.

### Exec=

`SplitExec` convierte el valor de `Exec=` en la lista de argumentos, sin pasar por ninguna shell:

1. Se deshacen los escapes generales de las cadenas (`\s`, `\n`, `\t`, `\r`, `\\`). La especificación los aplica **antes** de separar argumentos, así que `a\sb` da dos argumentos.
2. Se separa por espacios. Las comillas dobles agrupan un argumento con espacios, y dentro de ellas la comilla doble, la comilla invertida, el dólar y la barra invertida van escapados.
3. Los códigos de campo (`%f`, `%U`, `%i`…) desaparecen, porque una sesión no recibe ficheros ni URLs; `%%` queda como `%`. Un código desconocido es un error.

## Instancias de unidad

`systemd.CheckInstance` rechaza los IDs con caracteres que systemd no admite en un nombre de unidad: cualquier cosa que no sea letra, número, `:`, `-`, `_` o `.`. Se podrían escapar como hace `systemd-escape`, pero las entradas de sesión reales no los usan, y rechazarlos es más fácil de seguir que un nombre escapado.

## Hablar con systemd

De momento `internal/systemd` llama a `systemctl`, como las pruebas a mano. Es lo más sencillo de leer y no necesita un cliente de D-Bus, que Go no trae. Si más adelante hace falta algo que `systemctl` no dé bien, se decidirá entonces.

## Las unidades

En `data/systemd/user/`, como plantillas `.in`:

| Unidad                  | Qué es                                                              |
| ----------------------- | ------------------------------------------------------------------- |
| `uxsm-desktop@.service` | El escritorio de la entrada `%i`                                    |
| `uxsm-session@.target`  | La sesión; mientras está activa, lo está `graphical-session.target` |
| `uxsm-bindpid@.service` | Espera al proceso de la sesión, con PID `%i`                        |
| `uxsm-shutdown.target`  | Apaga todo lo anterior                                              |

- **`Type=exec`** en los dos servicios: systemd los da por arrancados cuando el programa se ha ejecutado, así que un `Exec=` que no existe hace fallar el arranque en vez de parecer que funciona.
- **`Slice=`**: el escritorio va a `session.slice`, la porción de systemd para los procesos esenciales de la sesión; la vigilancia del PID, a `background.slice`. Son los mismos que usa uwsm.
- **`CollectMode=inactive-or-failed`**: las instancias desaparecen al terminar aunque hayan fallado, y la siguiente sesión empieza sin restos.
- **`@BINDIR@`** se sustituye al instalar (`make install`), no al compilar, porque los paquetes compilan sin `PREFIX` y lo pasan sólo al instalar.

## Pruebas

- **Unitarias** (`go test ./...`): lectura de entradas, `Exec=`, orden de búsqueda y nombres de instancia. No necesitan systemd ni X.
- **De integración** (`make test-vm`): compilan uxsm sin cgo, lo instalan en un árbol aparte y lo prueban en máquinas virtuales desechables (`test/vm.sh`), con las mismas pruebas en todas (`test/integration/`). El display manager se sustituye por una unidad pasajera que ejecuta `uxsm start` con `DISPLAY` puesto, y el servidor X, por Xvfb. `DISTROS=quick` usa Ubuntu 24.04, `pair` añade Arch y `all` prueba todas.

`01-desktop-service.sh` comprueba el paso 1: que una entrada inexistente es un error, que la unidad arranca, que su proceso principal es el propio bspwm, que `DISPLAY` llega al gestor, que bspwm gestiona el display y que salir de bspwm termina el proceso de la sesión.

`02-session-shutdown.sh` comprueba el paso 2: que la sesión activa `graphical-session.target` y vigila el PID del proceso de la sesión, y que cada uno de los tres cierres ―salir de bspwm, matar el proceso de la sesión con `SIGTERM` y `uxsm stop`― deja inactivos el escritorio, la sesión, `graphical-session.target` y la vigilancia, sin ningún bspwm vivo.
