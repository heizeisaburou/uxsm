# Cómo funciona uxsm por dentro

Documentación técnica: qué hace cada parte y por qué está hecha así. Crece con cada paso del desarrollo; lo que todavía no existe no aparece aquí.

## La idea

Un display manager como LightDM arranca una sesión X11 ejecutando el `Exec=` de una entrada de `/usr/share/xsessions/`, y da la sesión por terminada cuando ese proceso termina. Normalmente el proceso es el propio escritorio: `bspwm`, `startxfce4`.

uxsm se mete en medio. La entrada de sesión ejecuta `uxsm start bspwm.desktop`, y uxsm lanza el escritorio como **servicio de `systemd --user`** en lugar de como hijo suyo. Es lo que hace uwsm con los compositores de Wayland, y lo que permite que el resto ―entorno, `graphical-session.target`, autostart, limpieza al salir― lo resuelvan las dependencias entre unidades en vez de código.

Antes de escribir código se comprobó a mano que un escritorio X11 aguanta así: bspwm y Xfce funcionan como servicio, polkit sigue permitiendo apagar y suspender aunque el proceso quede fuera de la sesión de logind, y al salir no queda nada.

## Línea de órdenes

Las subórdenes van en **grupos** (`group` en `cmd/uxsm/main.go`), cada uno con su lista y su ayuda, y todos reparten con el mismo código (`dispatch`):

- **`uxsm`**: `start`, `stop`, `entry`, `check`, `setup`, `version` y `aux`. `aux` está marcada como oculta: la llaman las unidades de systemd, no las personas, así que no sale en la ayuda general, pero se ejecuta igual que las demás.
- **`uxsm aux`**: `exec`, `waitpid`, `prepare-env` y `cleanup-env`, lo que ejecutan las unidades en sus `Exec*=`.
- **`uxsm setup`**: `xsessions-dir`. Cada arreglo es una suborden con nombre propio, no un lanzador de scripts.

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
 └─ uxsm start bspwm.desktop              (1) busca la entrada, calcula la identidad
     │                                        y la guarda con el entorno de login
     ├─ systemctl --user start uxsm-bindpid@<PID>.service
     │                                    (2) vigila su propio PID
     └─ exec systemctl --user start --wait uxsm-desktop@bspwm.desktop.service
                                          (3) mismo PID: LightDM y bindpid vigilan este proceso

systemd --user
 ├─ uxsm-bindpid@<PID>.service
 │   └─ uxsm aux waitpid <PID>
 ├─ uxsm-env@bspwm.desktop.service
 │   └─ uxsm aux prepare-env              (4) monta el entorno en el gestor
 ├─ uxsm-desktop@bspwm.desktop.service
 │   └─ uxsm aux exec bspwm.desktop       (5) vuelve a leer la entrada
 │       └─ exec bspwm                    (6) mismo PID: el servicio es el escritorio
 ├─ uxsm-session@bspwm.desktop.target     (7) lo arrastra el escritorio
 └─ graphical-session.target              (7) lo arrastra la sesión
```

1. **`uxsm start`** (`cmd/uxsm/start.go`) comprueba que la entrada existe, que su `Exec=` se puede partir y que su ID sirve como instancia de unidad. Lo hace antes de tocar systemd, para que un error salga en el acto y no dentro de un servicio. Después calcula la **identidad de la sesión** con `-D` y `-e` ―[Identidad de la sesión](#identidad-de-la-sesión)― y la guarda en `$XDG_RUNTIME_DIR/uxsm` junto con el entorno que le ha dado el display manager. **No toca el entorno del gestor**: de eso se encarga `uxsm-env@`, que es el que sabe deshacerlo al cerrar.
2. **Arranca `uxsm-bindpid@<PID>.service` con su propio PID**, para que la sesión se apague si LightDM mata este proceso (ver [Fin de la sesión](#fin-de-la-sesión)).
3. **Se sustituye por `systemctl --user start --wait`** con `exec`. El PID no cambia, así que el proceso que vigilan LightDM y bindpid pasa a ser ese `systemctl`, que no vuelve hasta que la unidad termina. La sesión dura, por construcción, lo mismo que el escritorio.
4. **`uxsm-env@.service` monta el entorno de la sesión en el gestor**, antes que el escritorio: `uxsm-desktop@` lo pide con `Requires=uxsm-env@%i.service` y espera a que termine con `After=`. Hace falta porque **un servicio no hereda el entorno de quien lo arranca, sino el del gestor**: sin este paso, el escritorio no tendría ni la identidad ni el `DISPLAY` del servidor X que ha arrancado LightDM. Ver [Entorno de la sesión](#entorno-de-la-sesión).
5. **La unidad `uxsm-desktop@.service`** ejecuta `uxsm aux exec %i`. La instancia es el ID de la entrada, así que la unidad no necesita saber el comando: `aux exec` vuelve a leer la entrada.
6. **`aux exec` se sustituye por el `Exec=` de la entrada**, otra vez con `exec`. El proceso principal del servicio es el propio escritorio, y cuando el escritorio termina, termina el servicio.
7. **El escritorio arrastra la sesión.** `uxsm-desktop@` tiene `BindsTo=uxsm-session@%i.target`, y ese target, `BindsTo=graphical-session.target`, el target estándar de systemd que significa «hay una sesión gráfica». `graphical-session.target` no se puede arrancar a mano (`RefuseManualStart`); sólo llega activo porque algo lo arrastra, y aquí lo arrastra la sesión.

Pasar sólo el ID tiene dos ventajas: las unidades son ficheros fijos que instala el paquete, sin generar nada en tiempo de ejecución, y `systemctl --user status` enseña `uxsm-desktop@bspwm.desktop.service`, que se entiende de un vistazo.

### Con un comando en vez de una entrada

Como uwsm, `uxsm start` también arranca un comando: `uxsm start -- bspwm`, o `uxsm start -D foo -- mywm --config x`. La regla es la de uwsm (`resolveTarget`): un único argumento acabado en `.desktop` es una entrada, y todo lo demás es un comando con sus argumentos. `--` sirve para pasarle al programa argumentos que empiezan por `-`, que si no se tomarían por opciones de uxsm, y detrás de él todo es un comando aunque acabe en `.desktop`. El paquete `flag` se come el `--` sin avisar, así que `afterDashes` mira si iba justo delante de los argumentos que quedan.

El resto del arranque es el mismo, con dos diferencias:

- **La instancia es el nombre del programa**, como en uwsm: `uxsm start -- bspwm` arranca `uxsm-desktop@bspwm.service`. No choca con las entradas, porque sus IDs acaban en `.desktop`.
- **La orden la guarda `uxsm start`** en `$XDG_RUNTIME_DIR/uxsm/command`, en el mismo formato que los entornos, y `uxsm aux exec bspwm` la lee de ahí, porque no hay entrada que volver a leer. `aux exec` distingue los dos casos por el `.desktop` del final, y no ejecuta un fichero cuyo programa no sea el de su instancia. `uxsm aux cleanup-env` lo borra al cerrar, con los demás.

Sin entrada no hay `DesktopNames=`, así que los nombres salen de `XDG_CURRENT_DESKTOP`, de `-D` o, como último recurso, del nombre del programa. Un programa que no existe es un error en el acto, igual que una entrada que no existe.

## Identidad de la sesión

La identidad son las variables que dicen qué escritorio es la sesión. Las calcula `uxsm start` (`internal/session`) con el mismo criterio que uwsm:

| Variable | Valor |
| --- | --- |
| `XDG_CURRENT_DESKTOP` | Los nombres del escritorio, separados por `:` |
| `XDG_SESSION_DESKTOP` | El primer nombre |
| `XDG_MENU_PREFIX` | El primer nombre en minúsculas y con `-` detrás, el prefijo de los ficheros de menú de XDG (`xfce-applications.menu`) |
| `XDG_SESSION_TYPE` | `x11`; uwsm pone `wayland` |

**De dónde salen los nombres** (`session.DesktopNames`):

- **Sin `-e`**, por este orden: el `XDG_CURRENT_DESKTOP` que ya traiga el entorno ―lo pone el display manager a partir del `DesktopNames=` de la entrada―, el `DesktopNames=` de la entrada y los de `-D`. Se quitan los repetidos dejando la primera aparición.
- **Si todo eso queda vacío**, el nombre del programa: el del `Exec=` de la entrada o el del comando. Pasa de verdad: `DesktopNames=` es opcional, y el `bspwm.desktop` de bspwm 0.9.10-2, el que lleva Ubuntu 24.04, sólo trae `Name=`, `Comment=`, `Exec=` y `Type=`. El de Arch y el de bspwm 0.9.12-1 sí lo traen. Con la entrada de 24.04, la sesión se llama `bspwm` porque así se llama el programa.
- **Con `-e`**, sólo los de `-D`, para que el resultado no dependa de lo que hubiera antes en el entorno. `-e` sin `-D` es un error.

`-D` admite letras, números, `_`, `.` y `-`, separados por `:`. Un `-D` mal escrito o `-e` sin `-D` son errores de argumentos: salen antes de tocar systemd y terminan con código 2 (`session.ErrBadNames`).

**Lo que se guarda en `$XDG_RUNTIME_DIR/uxsm`** (`internal/session/runtime.go`), para que lo lea el servicio de entorno:

- **`env_login`**: el entorno con el que el display manager lanzó la sesión, sin las variables propias de la shell (`_`, `SHELL`, `PWD`, `OLDPWD`) ni las de nombre inválido. Es el mismo filtro que aplica uwsm a su `env_login`.
- **`env_identity`**: las variables de identidad.

Los dos van separados por caracteres nulos, el único que no puede aparecer en un valor. Se escriben en un fichero temporal que después se renombra, para que nadie lea uno a medias, y sólo los puede leer el usuario. `$XDG_RUNTIME_DIR` lo crea logind al abrir la sesión y lo borra al cerrar la última, así que no sobreviven al usuario.

## Entorno de la sesión

`uxsm-env@.service` es el que pone y quita el entorno de la sesión en el gestor de systemd (`internal/sessionenv`). Es un `Type=oneshot` con `RemainAfterExit=yes`: arranca, monta el entorno y se queda activo sin ningún proceso, sólo para marcar que el entorno está puesto y para tener un `ExecStopPost=` que lo desmonte al parar.

Está copiado de `uwsm aux prepare-env` y `uwsm aux cleanup-env`, con el mismo criterio.

### Preparar

`uxsm aux prepare-env`, el `ExecStart=`:

1. **Guarda una foto** del entorno del gestor en `$XDG_RUNTIME_DIR/uxsm/env_pre`.
2. **Ejecuta el cargador** (`internal/sessionenv/loader.sh`, empotrado en el binario con `go:embed`) partiendo de esa foto, con el `env_login` por encima y la identidad de la sesión. El cargador es un `/bin/sh` porque los ficheros de entorno son fragmentos de shell: carga `/etc/profile` y `~/.profile`, pone los directorios XDG que falten, exporta la identidad, y carga de menos a más prioridad `uxsm/env`, un `uxsm/env-<escritorio>` por cada nombre de `XDG_CURRENT_DESKTOP` en minúsculas y el directorio `.d` de cada uno, en cada directorio de configuración y de datos de XDG. Al final escribe una marca y detrás su entorno con `env -0`; lo que escriba antes de la marca son mensajes y acaban en el journal.
3. **Compara la foto con el resultado** (`computeChanges`), apunta en `env_cleanup` lo que habrá que borrar al cerrar, y sólo entonces toca el gestor. Apuntar antes de tocar es lo que permite limpiar bien si algo falla a medias.

### Qué se sube y qué se borra

Comparar la foto con el resultado del cargador no basta, así que hay cinco listas de nombres (`internal/sessionenv/lists.go`), copiadas de la clase `Varnames` de uwsm:

| Lista | Qué hace |
| --- | --- |
| `alwaysExport` | Se suben aunque el gestor ya tuviera el mismo valor |
| `alwaysUnset` | No se suben nunca y se borran del gestor al empezar |
| `neverExport` | No se suben nunca: son del proceso o de la shell, no de la sesión |
| `alwaysCleanup` | Se borran al cerrar aunque no las subiera la sesión |
| `neverCleanup` | No se borran al cerrar |

Frente a uwsm hay tres cambios, todos por lo mismo: en Wayland `DISPLAY` es el de XWayland y lo pone el compositor al arrancar; en X11 lo pone el display manager antes de la sesión y llega en el `env_login`.

- **`DISPLAY` y `XAUTHORITY` van en `alwaysExport`**: son las que el escritorio necesita para conectarse al servidor X, y tienen que llegar al gestor sí o sí.
- **`DISPLAY` sale de `alwaysUnset`**, donde uwsm la tiene, porque aquí la buena viene del login.
- **`XAUTHORITY` entra en `alwaysCleanup`**, para que no quede apuntando a un fichero de una sesión que ya no existe.

A `neverExport` se le añaden además las variables que systemd le pone a todo servicio que arranca (`MANAGERPID`, `JOURNAL_STREAM`, `SYSTEMD_EXEC_PID`, `MEMORY_PRESSURE_*`): llegan al `env_login` cuando la sesión la lanza una unidad en vez de un display manager, como en las pruebas de integración.

`uxsm aux cleanup-env`, el `ExecStopPost=`, hace lo contrario: borra lo que apuntó `env_cleanup` más lo de `alwaysCleanup`, siempre que el gestor las tenga ahora y **no estuvieran en la foto**; después restaura la foto entera y borra los ficheros de trabajo. Lo que estaba antes vuelve con su valor de entonces, basura incluida: si el gestor traía un `WAYLAND_DISPLAY` viejo de una sesión que no limpió, la sesión de uxsm no lo ve pero al cerrar vuelve a estar. Es lo mismo que hace uwsm, y es a propósito: uxsm deshace lo suyo, no arregla lo de otros.

Como `ExecStopPost=` corre al parar el servicio por cualquier motivo, la limpieza también se hace si la preparación falló a medias. Sin foto no hay nada que deshacer, así que en ese caso no hace nada.

### Una sesión cada vez

El cierre de una sesión no es instantáneo, y lo último que hace es borrar los ficheros de `$XDG_RUNTIME_DIR/uxsm`. Si una sesión nueva empezara en ese momento ―un autologin rápido―, la limpieza de la anterior le borraría los ficheros recién escritos.

Por eso `uxsm start` espera, antes de escribirlos, a que no quede viva ninguna unidad de sesión gráfica: `graphical-session.target`, `graphical-session-pre.target` y las unidades de uxsm (`waitForPreviousSession`). Si a los diez segundos sigue habiendo alguna, es que hay otra sesión de verdad en marcha, y falla diciendo cuál.

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
- **Qué se lee**: el grupo `[Desktop Entry]` y, de él, `Name=`, `Comment=`, `Exec=` y `DesktopNames=`. Las traducciones (`Name[es]=`) y los grupos de acciones se ignoran. Una entrada sin `Exec=` es un error.

### Exec=

`SplitExec` convierte el valor de `Exec=` en la lista de argumentos, sin pasar por ninguna shell:

1. Se deshacen los escapes generales de las cadenas (`\s`, `\n`, `\t`, `\r`, `\\`). La especificación los aplica **antes** de separar argumentos, así que `a\sb` da dos argumentos.
2. Se separa por espacios. Las comillas dobles agrupan un argumento con espacios, y dentro de ellas la comilla doble, la comilla invertida, el dólar y la barra invertida van escapados.
3. Los códigos de campo (`%f`, `%U`, `%i`…) desaparecen, porque una sesión no recibe ficheros ni URLs; `%%` queda como `%`. Un código desconocido es un error.

### Entradas generadas

`uxsm entry` genera entradas de sesión (`internal/sessionentry`) y las instala en `/usr/local/share/xsessions`. Una entrada sale de una **fuente**: una entrada que ya existe (`FromEntry`), un escritorio de la tabla de escritorios conocidos (`FromTable`) o un comando (`FromCommand`). De cualquier fuente se genera uno de tres ficheros:

- **La entrada de uxsm que apunta a otra entrada** (`uxsm entry bspwm`, `Source.Uxsm`): `bspwm-uxsm.desktop` con `Exec=uxsm start bspwm.desktop`. Necesita que `bspwm.desktop` exista; si no, falla y dice cómo seguir, en vez de crear dos ficheros.
- **La entrada de uxsm con el comando directo** (`uxsm entry --exec bspwm` o `--exec -- mywm args`, `Source.UxsmExec`): `bspwm-uxsm.desktop` con `Exec=uxsm start -D bspwm -- bspwm`. Con un nombre, la orden sale de la entrada si existe y, si no, de la tabla.
- **La entrada normal, sin uxsm** (`uxsm entry --plain bspwm` o `--plain -- mywm args`, `Source.Plain`): `bspwm.desktop` con `Exec=bspwm`, la que debería haber instalado el paquete. Con un nombre, sale siempre de la tabla.

```ini
[Desktop Entry]
# Generated by uxsm from bspwm.desktop.
Type=Application
Name=bspwm (uxsm)
Comment=Binary space partitioning window manager
Exec=uxsm start -D bspwm bspwm.desktop
TryExec=uxsm
DesktopNames=bspwm;
X-UXSM-Source=bspwm.desktop
```

La que apunta a otra entrada sigue lo que recomienda uwsm para Wayland:

- **`Exec=` apunta a la entrada original** por su ID, no al programa: `uxsm start` lee de ella el `Exec=`, y no hay que copiarlo en argumentos que algunos display managers no saben entrecomillar. El ID va sin comillas porque, al tener que valer como instancia de unidad, sólo lleva letras, números y `:-_.`.
- **`-D` en el `Exec=` con los nombres que no trae la original**. `uxsm start` lee la entrada original, no la generada, y no todos los display managers pasan el `DesktopNames=` de la generada a `XDG_CURRENT_DESKTOP`. Con el comando directo los nombres van siempre en `-D`, porque no hay entrada de la que leerlos.
- **`TryExec=uxsm`** esconde la entrada si uxsm no está instalado, y **`X-UXSM-Source=`** la marca como generada, para no confundirla con una escrita a mano. La entrada normal lleva en `TryExec=` su propio programa.

**Los nombres del escritorio** son los conocidos ―los del `DesktopNames=` de la entrada o, si no trae, los de la tabla― y detrás los de `-D`, que siempre **añade**, como en `uxsm start`. `-e` deja sólo los de `-D`, y si con eso tira nombres conocidos se niega, salvo con `--force-names`: `-e -D Cinnamon` tiraría `X-Cinnamon`. Si no hay ningún nombre ―ni `DesktopNames=`, ni tabla, ni `-D`― se niega y pide `-D`, sin opción para forzarlo: la sesión acabaría llamándose como el programa.

**Adónde**: sólo a `/usr/local/share/xsessions`, ni a la salida estándar ni a otro directorio, y nunca a `/usr/share/xsessions`, que es de los paquetes, donde una actualización pisaría la entrada o la nuestra chocaría con la de un paquete. Quien la quiera en otro sitio, que la cree ahí y la mueva. Sin `-i` sólo enseña qué fichero escribiría y con qué contenido; con `-i` lo escribe, y eso necesita root. Si ya hay una entrada con ese nombre en `/usr/local/share/xsessions`, o en otro directorio de xsessions de los que ésta taparía ―`/usr/local/share` va antes que `/usr/share`―, se niega salvo con `-f`. Si el display manager en uso no lee `/usr/local/share/xsessions`, avisa de que la entrada no saldrá en la pantalla de inicio (ver [Display managers](#display-managers)).

**No se genera nada** a partir de una entrada o un comando que ya use uxsm ―se arrancaría a sí mismo― ni de uno que ya arranque su escritorio como servicio de `systemd --user`, como el `qtile.desktop` de Arch, que hace `systemctl --user start --wait qtile.service`: serían dos gestores para la misma sesión. Ni de una **meta-sesión**, que en vez de un escritorio arranca el script personal del usuario (`lightdm-xsession.desktop`, cuyo `Exec=default` sólo entiende LightDM, y `xinit-compat.desktop`, que ejecuta `~/.xsession`): no hay forma de saber qué escritorio correrá, y si el script arranca uxsm, la sesión se arrancaría a sí misma. Se reconocen por el programa del `Exec=` (`default` y `xinit-compat`), no por el nombre de la entrada (`sessionentry.IsMetaSession`). `uxsm start` no las comprueba, como tampoco uwsm: con `default` falla porque no es un programa.

**Las órdenes de la tabla** van sin ruta, para que valgan en cualquier distribución, y sin nada que entrecomillar. Las entradas cuya orden no cumple eso (GNOME Classic, que empieza por `env`, y GNOME Flashback, cuyo programa no está en `PATH`) no la llevan, y `FromTable` las rechaza. Las órdenes que da el usuario sí se entrecomillan (`quoteExec`), con los escapes de las cadenas por encima: una barra invertida dentro de unas comillas se escribe doble.

**Los escritorios conocidos** (`known.go`) completan lo que la entrada no trae; lo que trae la entrada manda siempre. `DesktopNames=` es opcional y en la práctica falta mucho: de las 100 entradas distintas que traen los paquetes de Arch, Debian 13, Ubuntu 24.04 y Fedora 43 (`test/xsessions.sh`, que las extrae de los paquetes sin instalarlos), awesome, Cinnamon, Openbox, xmonad, fluxbox o LXDE no lo traen en ninguna, y bspwm lo trae en Arch y en Debian pero no en Ubuntu ni en Fedora. Los nombres de la tabla salen, por orden:

1. del `DesktopNames=` de alguna de esas distribuciones;
2. del `XDG_CURRENT_DESKTOP` que pone el propio arrancador de la sesión, para que el gestor tenga el mismo nombre que ven los procesos del escritorio: `cinnamon-session` lo toma de `DesktopName=X-Cinnamon` en su `.session`, `startlxde` exporta `LXDE` y `dde-session`, `DDE`. Sin la tabla, Cinnamon tendría en el gestor `cinnamon-session-cinnamon`;
3. si nadie pone ninguno, como pasa con todos los gestores de ventanas, del nombre del programa en minúsculas, que es lo que usan los que sí lo declaran (`i3`, `bspwm`, `dwm`, `qtile`).

| Entrada | `DesktopNames` | De dónde |
| --- | --- | --- |
| `budgie-desktop.desktop` | `Budgie;GNOME` | Debian, Ubuntu; Fedora sólo `Budgie` |
| `cinnamon.desktop`, `cinnamon2d.desktop` | `X-Cinnamon` | su `.session` |
| `deepin.desktop` | `DDE` | `dde-session` |
| `enlightenment.desktop` | `Enlightenment` | todas |
| `gnome-xorg.desktop` | `GNOME` | todas |
| `gnome-classic-xorg.desktop` | `GNOME-Classic;GNOME` | todas |
| `gnome-flashback-metacity.desktop` | `GNOME-Flashback;GNOME` | todas |
| `LXDE.desktop` | `LXDE` | `startlxde` |
| `lxqt.desktop` | `LXQt` | todas |
| `mate.desktop` | `MATE` | todas |
| `plasmax11.desktop`, `plasma.desktop` | `KDE` | todas; `plasma.desktop` es el nombre anterior a Plasma 6 |
| `xfce.desktop` | `XFCE` | todas |
| `bspwm.desktop` | `bspwm` | Arch, Debian |
| `cwm.desktop` | `cwm` | Fedora |
| `dwm.desktop` | `dwm` | Debian |
| `fvwm3.desktop` | `FVWM3;FVWM` | Arch, Debian |
| `i3.desktop` | `i3` | todas |
| `icewm.desktop`, `icewm-session.desktop` | `ICEWM` | todas |
| `qtile.desktop` | `qtile` | Arch |
| `spectrwm.desktop` | `spectrwm` | Debian |
| `wmaker.desktop` | `WindowMaker` | todas |
| `awesome`, `blackbox`, `fluxbox`, `herbstluftwm`, `jwm`, `openbox`, `pekwm`, `stumpwm`, `xmonad` | el nombre en minúsculas | ninguna |
| `sawfish-mate.desktop`, `xmonad-mate.desktop` | `MATE` | su script acaba en `mate-session` |
| `sawfish-xfce.desktop` | `XFCE` | su script acaba en `startxfce4` |
| `sawfish-kde5.desktop` | `KDE` | su script acaba en `startplasma-x11` |
| `openbox-gnome.desktop` | `GNOME` | `gnome-session` con `DesktopName=GNOME` |

Las cinco últimas son **un gestor de ventanas dentro de un escritorio**: su `Exec=` es un script que arranca el gestor de ventanas y después el gestor de sesión del escritorio, así que lo que corre es el escritorio, y su nombre es el del escritorio, no el del gestor de ventanas ni el del script. Sin la tabla, el último recurso daría `sawfish-mate-session`. Faltan a propósito las que ya no funcionan (`openbox-kde`, `e16-kde-session` y `sawfish-kde4` acaban en `startkde`, que Plasma ya no trae; `e16-gnome2-session` y `e16-gnome3-session` son de Fedora, donde GNOME ya no tiene sesión X11) y `sawfish-lumina`, porque no está claro qué nombre pone Lumina.

## Display managers

`internal/dm` averigua qué display manager usa el sistema y en qué directorios busca las entradas de sesión X11, sus **directorios de xsessions**. Importa porque `uxsm entry` instala en `/usr/local/share/xsessions`, y un display manager que no lea ese directorio no enseña las entradas en la pantalla de inicio. Ninguno lee `~/.local/share/xsessions`: enseñan la lista antes de que inicies sesión y con su propio usuario.

El display manager en uso es el que arranca `display-manager.service`: `systemctl show -p Id display-manager.service` da su unidad real, `lightdm.service`. Hay un adaptador para cada uno de los tres importantes, y de cualquier otro (greetd, ly…) se dice que no se puede determinar, sin suponer nada:

- **LightDM**: la opción `sessions-directory` del grupo `[LightDM]`, separada por `:`. Lee sus ficheros en este orden, y gana el último, comprobado con `lightdm --show-config` en Debian 13: los `lightdm/lightdm.conf.d` de los directorios de datos XDG (primero `/usr/share`), el de `/etc/xdg`, el de `/etc/lightdm` y por último `/etc/lightdm/lightdm.conf`. Su valor de serie, compilado en el binario, es `/usr/share/lightdm/sessions:/usr/share/xsessions:/usr/share/wayland-sessions`: **no incluye `/usr/local/share/xsessions`**.
- **SDDM**: la opción `SessionDir` del grupo `[X11]`, separada por comas, en `/usr/lib/sddm/sddm.conf.d`, `/etc/sddm.conf.d` y `/etc/sddm.conf`, según `sddm.conf(5)`. Su valor de serie ya incluye `/usr/local/share/xsessions`.
- **GDM**: el subdirectorio `xsessions` de cada directorio de su `XDG_DATA_DIRS`, más `/usr/share/xsessions`, que lleva compilado. Ese `XDG_DATA_DIRS` se calcula a partir de cómo lo arranca systemd: el entorno del gestor del sistema (`systemctl show-environment`), pisado por las líneas `Environment=` de su unidad y éstas por `EnvironmentFile=`. No se lee de su proceso, que necesitaría root y que estuviera en marcha; a cambio no se ve lo que GDM cambie por su cuenta, y por eso se dice «as systemd launches it».

**`uxsm check`** ejecuta todas las comprobaciones cada vez, sin subórdenes, y cada línea dice qué ha comprobado y con qué resultado: `ok`, `warning` o `unknown`. Por ahora hay una, `xsessions dir`: si el display manager en uso lee `/usr/local/share/xsessions`. Sale con código 1 si alguna da aviso, para poder usarlo en scripts, como `uwsm check`.

**`uxsm setup xsessions-dir [lightdm|sddm|gdm]`** hace que el display manager lea `/usr/local/share/xsessions`; sin display manager, usa el que está en uso. Sin `-i` sólo explica qué cambiaría; con `-i` lo cambia. Si ya lo lee, no hace nada. El cambio sale de la lista en vigor, no de la de serie, para no perder lo que el usuario hubiera cambiado, y pone `/usr/local/share/xsessions` delante de `/usr/share/xsessions`, como en `XDG_DATA_DIRS`. Dónde lo escribe:

- **En el fichero que pone la opción**, aunque sea del usuario. Un fichero nuevo que la pisara dejaría dos sitios con la misma opción, y cambiar el primero no haría nada.
- **En un fichero propio** (`/etc/lightdm/lightdm.conf.d/99-uxsm.conf`, `/etc/sddm.conf.d/99-uxsm.conf`), que se lee después, si nadie pone la opción o si la pone un fichero de un paquete, bajo `/usr`, que la próxima actualización pisaría.
- **Con GDM, en ninguno**: sólo explica cómo añadir `/usr/local/share` a `XDG_DATA_DIRS` en su unidad, y que hay que mantener los directorios de siempre, porque esa variable también decide dónde busca iconos, esquemas y lo demás.

## Instancias de unidad

La instancia de las unidades de una sesión es el ID de la entrada (`bspwm.desktop`) o, con un comando, el nombre del programa (`bspwm`). `systemd.CheckInstance` rechaza los que llevan caracteres que systemd no admite en un nombre de unidad: cualquier cosa que no sea letra, número, `:`, `-`, `_` o `.`. Se podrían escapar como hace `systemd-escape`, pero casi ninguna entrada real los usa ―entre las cien de Arch, Debian, Ubuntu y Fedora, sólo `aewm++.desktop`―, y rechazarlos es más fácil de seguir que un nombre escapado.

## Hablar con systemd

De momento `internal/systemd` llama a `systemctl`, como las pruebas a mano. Es lo más sencillo de leer y no necesita un cliente de D-Bus, que Go no trae. Si más adelante hace falta algo que `systemctl` no dé bien, se decidirá entonces.

Con dos excepciones, las dos por el entorno:

- **Leer el entorno del gestor** se hace con `busctl --user --json=short get-property … Environment`, no con `systemctl show-environment`: `show-environment` escribe una salida pensada para una shell y escapa algunos valores como `$'…'`, que habría que deshacer a mano. La propiedad de D-Bus da cada variable tal cual.
- **El entorno de activación de D-Bus** se actualiza con `busctl --user call … UpdateActivationEnvironment`, porque no hay orden de `systemctl` que lo haga. Sólo hace falta con `dbus-daemon`, que tiene su propio entorno para los servicios que activa; con `dbus-broker` los activa systemd y les llega el del gestor, así que uxsm mira primero a qué unidad apunta `dbus.service`. D-Bus no permite borrar una variable de ahí: lo más parecido es ponerle el valor vacío.
- **Hace falta el bus de sesión** de D-Bus, en `$XDG_RUNTIME_DIR/bus`: lo usan `busctl` y `systemctl --user start --wait`, que espera por D-Bus a que el escritorio termine. Las demás órdenes de `systemctl --user` se apañan sin él, con el socket privado del gestor. No viene en todas las instalaciones ―Ubuntu lo trae, la imagen mínima de Debian no―, así que el `.deb` depende de `dbus-user-session`; en Arch lo arrastra `systemd`. `uxsm start` lo comprueba antes de tocar systemd (`systemd.CheckUserBus`), porque sin él `systemctl` sólo dice _Failed to connect to user scope bus via local transport_.

## Las unidades

En `data/systemd/user/`, como plantillas `.in`:

| Unidad                  | Qué es                                                              |
| ----------------------- | ------------------------------------------------------------------- |
| `uxsm-env@.service`     | El entorno de la sesión `%i`                                        |
| `uxsm-desktop@.service` | El escritorio de la sesión `%i`                                     |
| `uxsm-session@.target`  | La sesión; mientras está activa, lo está `graphical-session.target` |
| `uxsm-bindpid@.service` | Espera al proceso de la sesión, con PID `%i`                        |
| `uxsm-shutdown.target`  | Apaga todo lo anterior                                              |

- **`Type=exec`** en el escritorio y en la vigilancia del PID: systemd los da por arrancados cuando el programa se ha ejecutado, así que un `Exec=` que no existe hace fallar el arranque en vez de parecer que funciona.
- **`Type=oneshot` con `RemainAfterExit=yes`** en `uxsm-env@`: no tiene proceso, sólo un `ExecStart=` que monta el entorno y un `ExecStopPost=` que lo desmonta, y se queda activo entre los dos.
- **`Slice=`**: el escritorio y el entorno van a `session.slice`, la porción de systemd para los procesos esenciales de la sesión; la vigilancia del PID, a `background.slice`. Son los mismos que usa uwsm.
- **`CollectMode=inactive-or-failed`**: las instancias desaparecen al terminar aunque hayan fallado, y la siguiente sesión empieza sin restos.
- **`@BINDIR@`** se sustituye al instalar (`make install`), no al compilar, porque los paquetes compilan sin `PREFIX` y lo pasan sólo al instalar.

## Pruebas

- **Unitarias** (`go test ./...`): lectura de entradas, `Exec=`, orden de búsqueda y nombres de instancia. No necesitan systemd ni X.
- **De integración** (`make test-vm`): prueban los paquetes reales, instalados, en máquinas virtuales desechables (`test/vm.sh`), con las mismas pruebas en todas (`test/integration/`). El display manager se sustituye por una unidad pasajera que ejecuta `uxsm start` con `DISPLAY` puesto, y el servidor X, por Xvfb. `DISTROS=quick` usa Ubuntu 24.04, `pair` añade Arch y `all` añade Debian 13, Fedora 43 y openSUSE Tumbleweed; también vale una lista, `DISTROS=fedora,opensuse`.

### Compilación y prueba de los paquetes

Un solo flujo, `test/release.sh`, compila y prueba; `make test-vm` y `make release` sólo cambian en qué distribuciones y en si publica el resultado:

1. Empaqueta el árbol de trabajo en `uxsm-<versión>.tar.gz`, con los cambios sin commitear y los ficheros nuevos que git no ignora: se prueba lo que hay en disco, no el último commit.
2. Compila el paquete de cada distribución en una máquina de esa distribución, con su herramienta y con la receta de `packaging/`, como lo haría la distribución: `makepkg -s` en Arch, `apt-get build-dep` y `dpkg-buildpackage` en Ubuntu y en Debian, y `rpmbuild` con las `BuildRequires` que da `rpmspec` en Fedora y en openSUSE, cada una con su `.spec`. Aunque compartan formato, Ubuntu y Debian tienen cada una su `.deb`, compilado contra sus bibliotecas y sus herramientas; la revisión lo dice, `-1~ubuntu24.04` o `-1~debian13`, y con `~` un `-1` oficial de la distribución va por delante. Los paquetes se traen a `build/release/<distro>`.
3. En una máquina **limpia** de cada distribución instala su paquete con su gestor ―`apt-get install ./uxsm_….deb`, `pacman -U`, `dnf install`, `zypper install`―, comprueba que `uxsm version` es la del paquete y ejecuta las pruebas. Compilar y probar en máquinas distintas es a propósito: en la que compila están Go y todas las dependencias de compilación, y un `depends` o un `Depends:` incompleto pasaría las pruebas igual.
4. `make release` hace todo esto en todas las distribuciones y, si pasa, copia `build/release` a `releases/latest`: el tarball, `version`, `SHA256SUMS` y un directorio por distribución con sus paquetes, los de depuración incluidos.

Las máquinas de prueba empiezan la sesión de ssh antes de instalar el paquete, así que lo que éste traiga para la sesión no está en marcha, como el bus de D-Bus de `dbus-user-session`: `run.sh` arranca `dbus.socket`, lo que habría hecho el siguiente inicio de sesión.

La versión de los paquetes sale de git (`test/version.sh`): la de la última etiqueta `vX.Y.Z` (o `0.0.0`), más `.rN.gHASH` si hay N commits después, más `.dirty` si el árbol de trabajo tiene cambios. Así vale tanto como `pkgver` de Arch, que no admite guiones, como versión upstream de Debian. Los scripts de `test/package/` la ponen en la copia del PKGBUILD y en la primera línea del `changelog` antes de compilar.

### Hooks de git

`make hooks` activa los de `.githooks/` en el clon. Separan lo que cuesta segundos de lo que cuesta minutos:

- **`pre-commit`**: `gofmt`, `go vet`, `go test` y la sintaxis de los scripts. Se puede commitear lo que sea mientras compile y pase las pruebas unitarias.
- **`pre-push`**: sólo al subir a `main`, a `master` o una etiqueta `vX.Y.Z`, lo que se sube tiene que haber pasado `make release`. Exige que sea `HEAD` y que el árbol esté limpio, porque `make release` compila el árbol de trabajo; si `releases/latest/version` ya es la versión de ese commit, no repite nada, y si no, ejecuta `make release` y el push espera. Las demás ramas suben sin comprobar nada.

Los dos se saltan con `--no-verify`.

La mayor parte del tiempo se va en arrancar máquinas y en descargar Go y las dependencias en cada una, no en compilar.

`01-desktop-service.sh` comprueba el paso 1: que una entrada inexistente es un error, que sin bus de sesión de D-Bus también lo es y lo dice, que la unidad arranca, que su proceso principal es el propio bspwm, que `DISPLAY` llega al gestor, que bspwm gestiona el display y que salir de bspwm termina el proceso de la sesión. Después repite el arranque con un comando, `uxsm start -- bspwm`: que un programa que no existe es un error, que arranca `uxsm-desktop@bspwm.service` con bspwm como proceso principal, que salir de bspwm cierra la sesión y que el comando guardado se borra al final.

`02-session-shutdown.sh` comprueba el paso 2: que la sesión activa `graphical-session.target` y vigila el PID del proceso de la sesión, y que cada uno de los tres cierres ―salir de bspwm, matar el proceso de la sesión con `SIGTERM` y `uxsm stop`― deja inactivos el escritorio, la sesión, `graphical-session.target` y la vigilancia, sin ningún bspwm vivo.

`03-session-identity.sh` comprueba el paso 3a con dos entradas de sesión propias en `~/.local/share/xsessions`, una con `DesktopNames=` y otra sin él, para no depender de cómo empaquete bspwm cada distribución. Mira las variables en el entorno real del proceso del escritorio (`/proc/<PID>/environ`): sin `-e`, con un `XDG_CURRENT_DESKTOP` heredado, con `-e`, y sin `DesktopNames=`; que se guardan `env_login` y `env_identity`, y que `-e` sin `-D` termina con código 2.

El proceso de sesión simulado es una unidad de `systemd-run`, y una unidad del gestor de usuario hereda su entorno ―`systemd.exec(5)`, _Environment variables in spawned processes_―. El que lanza un display manager no: es hijo de `lightdm --session-child`, no del gestor. Por eso la prueba lanza `uxsm start` con `env -u XDG_CURRENT_DESKTOP`, para no recibir el que dejaron en el gestor las pruebas anteriores.

`04-session-environment.sh` comprueba el paso 3b: que el escritorio recibe lo que ponen `uxsm/env`, `uxsm/env.d` y `uxsm/env-testde`, que no recibe el `env-other` de otro escritorio ni un `WAYLAND_DISPLAY` viejo del gestor, y que tras cada uno de los tres cierres el entorno del gestor queda **idéntico** al de antes de la sesión, basura previa incluida.

`05-generated-entries.sh` comprueba `uxsm entry`, `uxsm check` y `uxsm setup` en una máquina sin display manager. Que `uxsm check` dice que no se puede determinar y `uxsm setup xsessions-dir lightdm` se niega sin LightDM instalado. Que `uxsm entry` sin `-i` no escribe nada. Y que las entradas generadas arrancan la sesión con el nombre del escritorio bien puesto, ejecutando su `Exec=` como lo haría el display manager, pero sin `XDG_CURRENT_DESKTOP`, el peor caso: la que apunta a `bspwm.desktop` y la del comando directo, que no pisa a la anterior sin `-f`. Y que `--plain bspwm` no tapa el `bspwm.desktop` del paquete sin `-f`.

