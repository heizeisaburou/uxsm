# Cómo funciona uxsm por dentro

Documentación técnica: qué hace cada parte y por qué está hecha así. Crece con cada paso del
desarrollo; lo que todavía no existe no aparece aquí.

## La idea

Un display manager como LightDM arranca una sesión X11 ejecutando el `Exec=` de una entrada de
`/usr/share/xsessions/`, y da la sesión por terminada cuando ese proceso termina. Normalmente el
proceso es el propio escritorio: `bspwm`, `startxfce4`.

uxsm se mete en medio. La entrada de sesión ejecuta `uxsm start bspwm.desktop`, y uxsm lanza el
escritorio como **servicio de `systemd --user`** en lugar de como hijo suyo. Es lo que hace uwsm
con los compositores de Wayland, y lo que permite que el resto ―entorno, `graphical-session.target`,
autostart, limpieza al salir― lo resuelvan las dependencias entre unidades en vez de código.

Antes de escribir código se comprobó a mano que un escritorio X11 aguanta así: bspwm y Xfce
funcionan como servicio, polkit sigue permitiendo apagar y suspender aunque el proceso quede fuera
de la sesión de logind, y al salir no queda nada.

## Línea de órdenes

`cmd/uxsm/main.go` reparte por el primer argumento: `start`, `version` y `aux`, esta última para
las unidades de systemd y fuera de la ayuda general. Cada suborden lee sus opciones con su propio
`flag.FlagSet`, así que cada una tiene su ayuda.

**La ayuda se pide en cualquier posición** (`parseFlags`). `flag` deja de leer opciones en el
primer argumento que no lo es, y por sí solo no vería el `-h` de `uxsm start bspwm.desktop -h`.
Antes de leer opciones se mira toda la línea hasta `--`, y si aparece `-h`, `-help`, `--h` o
`--help`, se enseña la ayuda de esa suborden. Así se puede añadir `-h` al final de una orden a medio
escribir, ver la ayuda y seguir escribiendo. Lo que va detrás de `--` es del programa que se lanza,
no de uxsm.

**Códigos de salida**: 0 si va bien o se ha pedido ayuda, 2 si los argumentos están mal y 1 con
cualquier otro error. La ayuda pedida sale por la salida estándar; la que acompaña a un error de
argumentos, por la de error.

## Arranque de una sesión

```text
LightDM
 └─ uxsm start bspwm.desktop              (1) busca la entrada, importa DISPLAY
     └─ exec systemctl --user start --wait uxsm-desktop@bspwm.desktop.service
                                           (2) mismo PID: LightDM vigila este proceso

systemd --user
 └─ uxsm-desktop@bspwm.desktop.service
     └─ uxsm aux exec bspwm.desktop        (3) vuelve a leer la entrada
         └─ exec bspwm                     (4) mismo PID: el servicio es el escritorio
```

1. **`uxsm start`** (`cmd/uxsm/start.go`) comprueba que la entrada existe y que su ID sirve como
   instancia de unidad. Lo hace antes de tocar systemd, para que un error salga en el acto y no
   dentro de un servicio.
2. **Importa `DISPLAY` y `XAUTHORITY`** al gestor de systemd. Un servicio no hereda el entorno de
   quien lo arranca, sino el del gestor, y sin esas dos variables el escritorio no encuentra el
   servidor X que ha arrancado LightDM.
3. **Se sustituye por `systemctl --user start --wait`** con `exec`. El PID no cambia, así que el
   proceso que vigila LightDM pasa a ser ese `systemctl`, que no vuelve hasta que la unidad termina.
   La sesión dura, por construcción, lo mismo que el escritorio.
4. **La unidad `uxsm-desktop@.service`** ejecuta `uxsm aux exec %i`. La instancia es el ID de la
   entrada, así que la unidad no necesita saber el comando: `aux exec` vuelve a leer la entrada y se
   sustituye por su `Exec=`. Con `exec` otra vez, el proceso principal del servicio es el propio
   escritorio, y cuando el escritorio termina, termina el servicio.

Pasar sólo el ID tiene dos ventajas: la unidad es un fichero fijo que instala el paquete, sin
generar nada en tiempo de ejecución, y `systemctl --user status` enseña
`uxsm-desktop@bspwm.desktop.service`, que se entiende de un vistazo.

## Entradas de sesión

`internal/desktopentry` lee las entradas según la *Desktop Entry Specification*, pero sólo lo que
uxsm usa.

- **Dónde se buscan** (`Find`): en el subdirectorio `xsessions` de cada directorio de datos XDG,
  por orden de preferencia (`internal/xdg`): primero `XDG_DATA_HOME`, después cada uno de
  `XDG_DATA_DIRS`. La primera que aparece gana, así que una entrada del usuario tapa a la del
  sistema con el mismo ID.
- **Qué es un ID válido**: el nombre de un fichero terminado en `.desktop`, sin barras. Sin barras
  para que un ID no pueda salirse del directorio de entradas.
- **Qué se lee**: el grupo `[Desktop Entry]` y, de él, `Name=`, `Exec=` y `DesktopNames=`. Las
  traducciones (`Name[es]=`) y los grupos de acciones se ignoran. Una entrada sin `Exec=` es un error.

### Exec=

`SplitExec` convierte el valor de `Exec=` en la lista de argumentos, sin pasar por ninguna shell:

1. Se deshacen los escapes generales de las cadenas (`\s`, `\n`, `\t`, `\r`, `\\`). La
   especificación los aplica **antes** de separar argumentos, así que `a\sb` da dos argumentos.
2. Se separa por espacios. Las comillas dobles agrupan un argumento con espacios, y dentro de ellas
   la comilla doble, la comilla invertida, el dólar y la barra invertida van escapados.
3. Los códigos de campo (`%f`, `%U`, `%i`…) desaparecen, porque una sesión no recibe ficheros ni
   URLs; `%%` queda como `%`. Un código desconocido es un error.

## Instancias de unidad

`systemd.CheckInstance` rechaza los IDs con caracteres que systemd no admite en un nombre de
unidad: cualquier cosa que no sea letra, número, `:`, `-`, `_` o `.`. Se podrían escapar como hace
`systemd-escape`, pero las entradas de sesión reales no los usan, y rechazarlos es más fácil de
seguir que un nombre escapado.

## Hablar con systemd

De momento `internal/systemd` llama a `systemctl`, como las pruebas a mano. Es lo más sencillo de
leer y no necesita un cliente de D-Bus, que Go no trae. Si más adelante hace falta algo que
`systemctl` no dé bien, se decidirá entonces.

## La unidad

`data/systemd/user/uxsm-desktop@.service.in`:

- **`Type=exec`**: systemd da el servicio por arrancado cuando el programa se ha ejecutado, así que
  un `Exec=` que no existe hace fallar el arranque en vez de parecer que funciona.
- **`Slice=session.slice`**: la porción de systemd para los procesos esenciales de la sesión, la
  misma que usa uwsm.
- **`CollectMode=inactive-or-failed`**: la instancia desaparece al terminar aunque haya fallado, y
  la siguiente sesión empieza sin restos.
- **`@BINDIR@`** se sustituye al instalar (`make install`), no al compilar, porque los paquetes
  compilan sin `PREFIX` y lo pasan sólo al instalar.

## Pruebas

- **Unitarias** (`go test ./...`): lectura de entradas, `Exec=`, orden de búsqueda y nombres de
  instancia. No necesitan systemd ni X.
- **De integración** (`make test-vm`): compilan uxsm sin cgo, lo instalan en un árbol aparte y lo
  prueban en máquinas virtuales desechables (`test/vm.sh`), con las mismas pruebas en todas
  (`test/integration/`). El display manager se sustituye por una unidad pasajera que ejecuta
  `uxsm start` con `DISPLAY` puesto, y el servidor X, por Xvfb. `DISTROS=quick` usa Ubuntu 24.04,
  `pair` añade Arch y `all` prueba todas.

`01-desktop-service.sh` comprueba el paso 1: que una entrada inexistente es un error, que la unidad
arranca, que su proceso principal es el propio bspwm, que `DISPLAY` llega al gestor, que bspwm
gestiona el display y que salir de bspwm termina el proceso de la sesión.
