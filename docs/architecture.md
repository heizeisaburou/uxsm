# Cómo funciona uxsm

uxsm convierte una sesión X11 en un conjunto de unidades de `systemd --user`. El escritorio deja de ser un hijo opaco del display manager: pasa a ser el proceso principal de un servicio, comparte un entorno de sesión preparado de forma explícita y arrastra targets estándar de sesión gráfica. Cuando termina el escritorio o desaparece el proceso que abrió la sesión, systemd detiene el conjunto completo y uxsm restaura el entorno anterior.

Este documento explica qué hace uxsm en la máquina de quien lo usa: qué unidades aparecen, con qué nombres, qué entorno recibe el escritorio, cuándo se da la sesión por lista y qué se toca del display manager. Los diagramas muestran un recorrido representativo; no enumeran cada error ni cada combinación de opciones. Sus fuentes DOT están junto a los SVG en `docs/flows`.

Si lo que buscas es compilar, probar o empaquetar uxsm, eso está en [`development.md`](development.md).

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
3. espera a que otra sesión gráfica haya terminado también su limpieza, mirando las unidades de uxsm y las de uwsm por su nombre, y no `graphical-session.target`, que es de systemd y lo enciende cualquiera ―el envoltorio de sesión de NixOS lo activa antes de ejecutar el `Exec=` de la entrada―. Dos sesiones gráficas de un mismo usuario no encajan: el gestor de systemd es uno por usuario, así que compartirían ese target y el entorno;
4. guarda el entorno de login, la identidad y, si procede, el comando en `$XDG_RUNTIME_DIR/uxsm`;
5. arranca una unidad que vigila el PID entregado al display manager;
6. se sustituye por `systemctl --user start --wait uxsm-desktop@ID.service`.

![Flujo de arranque](flows/session-start.svg)

[Fuente DOT](flows/session-start.dot).

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

Del resultado salen `XDG_CURRENT_DESKTOP`, `XDG_SESSION_DESKTOP`, `XDG_MENU_PREFIX` y `XDG_SESSION_TYPE=x11`.

## Grafo de unidades y ciclo de vida

![Unidades y cierre](flows/systemd-lifecycle.svg)

[Fuente DOT](flows/systemd-lifecycle.dot).

Todas se ven con `systemctl --user list-units 'uxsm*'` mientras la sesión está en marcha, y `uxsm check is-active -v` las enumera.

| Unidad | Responsabilidad |
| --- | --- |
| `uxsm-bindpid@PID.service` | Espera con `pidfd` al proceso que abrió la sesión. |
| `uxsm-env@ID.service` | Prepara el entorno antes del escritorio y lo restaura al parar. |
| `uxsm-desktop@ID.service` | Ejecuta el escritorio como proceso principal y espera a que la sesión esté lista. |
| `uxsm-session@ID.target` | Representa la sesión uxsm y arrastra `graphical-session.target`. |
| `uxsm-autostart@ID.target` | Arrastra `xdg-desktop-autostart.target` cuando el autostart le toca a uxsm. |
| `app-uxsm.slice` y sus hermanas | Donde van las aplicaciones que lanza `uxsm app`. |
| `uxsm-shutdown.target` | Entra en conflicto con las unidades activas y coordina su cierre. |

La sesión se cierra por el mismo camino si:

- termina o falla el escritorio;
- el display manager mata el proceso que estaba esperando la sesión;
- alguien ejecuta `uxsm stop`.

Los dos primeros casos activan `uxsm-shutdown.target` mediante `OnSuccess=` y `OnFailure=`. `uxsm stop` activa ese target directamente. Sus conflictos paran el escritorio, los targets gráficos y el servicio de entorno; el `ExecStopPost=` de este último siempre intenta restaurar el estado previo.

### Cuándo la sesión está lista

El escritorio no cuenta como arrancado en cuanto empieza a ejecutarse. `uxsm-desktop@ID.service` lleva un `ExecStartPost=` que ejecuta `uxsm aux wait-ready`, y systemd no da el servicio por arrancado hasta que esa orden termina. Como los targets de la sesión van detrás del servicio, `uxsm-session@ID.target` y `graphical-session.target` esperan con él, y lo que arranque con la sesión gráfica encuentra un escritorio donde colocarse.

Hay dos formas de que la sesión se dé por lista, y valen lo mismo:

- **uxsm lo ve.** Es la comprobación de EWMH: la ventana raíz tiene `_NET_SUPPORTING_WM_CHECK` apuntando a una ventana del gestor de ventanas, y esa ventana tiene la misma propiedad apuntando a sí misma. Lo segundo distingue al gestor que está gobernando la pantalla de la marca que deja uno que murió de golpe. uxsm se lo pregunta al servidor X hablando el protocolo X11 por su cuenta, sin libX11 ni `xprop`: es el saludo inicial y dos propiedades, y así la sesión no depende en tiempo de ejecución de ningún paquete de Xorg.
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

Quien decide poner esa opción en una entrada es `uxsm entry`, apoyándose en [la tabla de escritorios conocidos](#la-tabla-de-escritorios-conocidos), que dice de cada sesión si lanza ella misma sus entradas de autostart. Es una pregunta sobre lo que hace, no sobre lo que es: la lanzan Xfce, GNOME, Plasma o MATE, por su gestor de sesión, y no la lanzan ni un gestor de ventanas ni una sesión con su propio fichero de arranque, como `icewm-session` con `~/.icewm/startup`, que es cosa aparte y no toca estas entradas. Cuando la tabla dice que sí, la opción va en el `Exec=` de la entrada generada:

```ini
Exec=uxsm start --no-autostart -D XFCE -- startxfce4
```

Hace falta porque ni el gestor de sesión del escritorio ni systemd comprueban si el otro ya ha lanzado una entrada: en Xfce, con los dos, cada una arranca dos veces. De un escritorio que la tabla no conoce, uxsm no supone nada: la entrada sale sin la opción, el autostart se lanza, y quien vea entradas duplicadas la añade.

Las unidades que crea el generador van a `app.slice`, el estándar, y uxsm se las lleva a `app-uxsm.slice` como todo lo que lanza: escribe un añadido sobre la plantilla que comparten todas, `$XDG_RUNTIME_DIR/systemd/user/app-@autostart.service.d/uxsm-tweaks.conf`, justo antes de arrancar el target, y lo borra al cerrar la sesión. No puede venir en el paquete: un añadido en `/usr/lib` se aplicaría también a las sesiones que no son de uxsm ―las de uwsm, las de un escritorio completo― y les cambiaría el slice. Es lo mismo que hace uwsm con el suyo, y lleva además `PartOf=` y `After=xdg-desktop-autostart.target`, que es lo que permite parar y arrancar el autostart con su target en vez de con la sesión entera.

La decisión de lanzarlo o no se escribe en `$XDG_RUNTIME_DIR/uxsm/autostart`, y quien la mira es `uxsm aux autostart`: con la marca arranca el target, y sin ella no hace nada. Los dos rodeos tienen motivo. La decisión no puede ir en un `Condition*=` de la unidad, porque las dependencias de una unidad se resuelven al montar el trabajo, antes de comprobar sus condiciones: el autostart arrancaría igual en las sesiones en las que la unidad se salta. Y el target estándar no se puede arrancar directamente, porque lleva `RefuseManualStart=`; tiene que arrastrarlo una unidad propia.

## Aplicaciones

```sh
uxsm app -- kitty                             # un comando
uxsm app firefox.desktop                      # una entrada de aplicación
uxsm app firefox.desktop:new-private-window   # una de sus acciones
uxsm app -s b -t service -- fcitx5            # en segundo plano y como servicio
uxsm app -p TimeoutStopSec=5 -- discord       # con un plazo para morir
```

Sin esto, todo lo que arranca un escritorio cuelga del escritorio y se ve junto. `uxsm app` le da a cada aplicación su propia unidad, dentro de uno de los slices de la sesión: se ve por separado en `systemctl --user`, se le pueden poner límites, su registro va al diario con su nombre, y se para con la sesión. Es lo mismo que hace `uwsm app` en Wayland.

El nombre de la unidad es el que pide systemd para las aplicaciones, `app-<quien la lanza>-<qué aplicación>-<algo que la distingue>`: `app-uxsm-kitty-3f2a1b0c.scope`, o con `@` antes de la parte de azar si es un servicio. De serie es un scope ―la aplicación cuelga de quien la lanzó― y con `-t service` la arranca el gestor. Las opciones son las de uwsm: `-s` elige el slice, `-a`, `-u` y `-d` los nombres y la descripción, y `-S` tira la salida de un servicio.

Y `-p Clave=Valor`, que también es de uwsm ―misma letra y mismo sentido―, pasa propiedades de systemd a la unidad tal como las toma `systemd-run` y se puede repetir: `-p TimeoutStopSec=5` para lo que tarda en morir, `-p MemoryMax=2G` o `-p CPUQuota=50%` para ponerle límites. Un scope admite las de control de recursos y los plazos; las que son propias de un servicio, como `Restart=`, necesitan además `-t service`. uxsm sólo comprueba que la propiedad tenga la forma `Clave=Valor`; de lo demás se queja systemd, que es quien sabe.

Los slices son tres, uno por clase de aplicación, y llevan `PartOf=graphical-session.target`, que es lo que las para con la sesión:

| Slice                   | Para qué                                        |
| ----------------------- | ----------------------------------------------- |
| `app-uxsm.slice`        | Las aplicaciones, lo de serie.                  |
| `background-uxsm.slice` | Lo que corre detrás, como un método de entrada. |
| `session-uxsm.slice`    | Lo que forma parte de la sesión, como un panel. |

El guion es jerarquía en systemd, así que cuelgan de los `app.slice`, `background.slice` y `session.slice` estándar. Llevan `uxsm` en el nombre porque los instala el paquete y dos paquetes no pueden traer el mismo fichero: los de uwsm, que hace esto mismo en Wayland, se llaman `app-graphical.slice` y compañía. Como dos sesiones gráficas de un mismo usuario no conviven, compartir los nombres no aportaba nada.

De una entrada de aplicación se lee su `Exec=` ―o el de la acción que se pida detrás de `:`―, se sustituyen los códigos de campo con los ficheros o URLs que se le pasen, y se respeta su `Path=`. Una entrada con `Terminal=true` se rechaza por ahora, en vez de lanzarla fuera de un terminal.

`uxsm check is-active` contesta con el código de salida si hay una sesión de uxsm en marcha, y con `-v` dice qué unidades la forman. Es lo que permite a un script saber dónde está. No va dentro de `uxsm check`, que es otra cosa: aquélla comprueba si el sistema está listo para uxsm y escribe un informe.

## Entorno de la sesión

Los servicios de usuario heredan el entorno de `systemd --user`, no el del proceso que los arranca. Por eso `uxsm start` no puede limitarse a llamar a systemd: primero conserva lo que recibió del display manager y `uxsm-env@.service` lo monta en el gestor.

![Preparación y restauración del entorno](flows/environment.svg)

[Fuente DOT](flows/environment.dot).

Durante la preparación:

1. se guarda en `env_pre` una foto filtrada del entorno de `systemd --user`;
2. el entorno de login se superpone a esa foto;
3. un cargador `/bin/sh`, empotrado en el binario, carga `/etc/profile`, `~/.profile`, la identidad y los ficheros de entorno de uxsm;
4. se calcula qué variables poner y quitar, y se guarda en `env_cleanup` qué pertenece a la sesión;
5. se actualiza el entorno de systemd y, si el bus usa `dbus-daemon`, también el de activación de D-Bus. Con `dbus-broker`, la activación ya se delega en systemd.

Los ficheros de uxsm se cargan de menor a mayor prioridad recorriendo `XDG_DATA_DIRS`, `XDG_CONFIG_DIRS` y `XDG_CONFIG_HOME`. En cada directorio se carga primero `uxsm/env`, después `uxsm/env-<escritorio>` por cada nombre de `XDG_CURRENT_DESKTOP`, y después de cada fichero su directorio `.d` en orden alfabético. Se ignoran copias y ejemplos como `*.bak`, `*.disabled` o `*.sample`.

Al cerrar, uxsm borra las variables creadas para la sesión, restaura todos los valores de `env_pre` y elimina los ficheros de trabajo. Variables de agentes SSH se conservan expresamente.

## Entradas de sesión y display managers

### Generar la entrada

`uxsm entry` crea tres tipos de entrada:

```sh
uxsm entry bspwm                    # bspwm-uxsm.desktop → bspwm.desktop
uxsm entry --exec bspwm             # bspwm-uxsm.desktop → comando bspwm
uxsm entry --plain bspwm            # bspwm.desktop sin uxsm
uxsm entry --exec -- mywm --flag    # variante uxsm para un comando explícito
```

![Generación de entradas](flows/session-entries.svg)

[Fuente DOT](flows/session-entries.dot).

Una fuente puede ser una entrada existente, un comando o la tabla de escritorios conocidos. El generador rechaza entradas que ya usan uxsm, sesiones que ya arrancan el escritorio mediante `systemd --user` y metasesiones que sólo ejecutan el script personal del usuario.

### La tabla de escritorios conocidos

Es una lista que viene dentro de uxsm, con 38 sesiones de escritorio y de gestores de ventanas ―los de los paquetes de Arch, Debian 13, Ubuntu 24.04 y Fedora 43―. De cada una guarda cuatro cosas: el nombre y el comentario que se ven en la pantalla de inicio, sus `DesktopNames=`, el comando que la arranca ―sólo si es el mismo en todas las distribuciones― y si lanza ella misma las entradas de autostart XDG, que son 18 de las 38.

**Para qué se usa.** Para rellenar lo que la entrada original no dice. Muchas entradas no traen `DesktopNames=` en ninguna distribución, o lo traen en unas y no en otras ―`bspwm` sí en Arch y en Debian, no en Ubuntu ni en Fedora―, y sin esos nombres el escritorio recibe un `XDG_CURRENT_DESKTOP` distinto del que esperan sus propias aplicaciones. Y para saber si la entrada generada tiene que llevar `--no-autostart`, que es lo que evita que un escritorio con gestor de sesión lance cada entrada de autostart dos veces.

**Quién manda.** Lo que trae la entrada original, siempre; la tabla sólo aporta lo que falta. Y por encima de las dos, lo que se pida en la línea de órdenes: `-N` el nombre, `-C` el comentario, `-D` los nombres de escritorio.

**Cuándo no se usa.** Con `-e`, los nombres de escritorio son sólo los de `-D`: se descartan los de la entrada y los de la tabla. Pero si eso tirara algún nombre conocido, uxsm no obedece callando, dice cuál y para:

```sh
uxsm entry -e -D MiWM bspwm                 # falla: tiraría el nombre conocido, bspwm
uxsm entry -e -D bspwm:MiWM bspwm           # bien: el conocido va en -D, y se añade MiWM
uxsm entry -e -D MiWM -force-names bspwm    # bien: tirarlo es lo que se quiere, y se insiste
uxsm entry --exec -- mywm --flag            # ni entrada ni tabla: sólo el comando que se da
```

```
uxsm: bspwm.desktop: -e would drop desktop names that are known: bspwm; add them to -D, or use --force-names to drop them anyway
```

`-e` es «sólo estos nombres», y en `uxsm start` hace exactamente eso, sin avisar de nada. Aquí avisa, y ésa es la única diferencia entre los dos: lo que `start` decide vale para la sesión que empieza, y el efecto se ve en el momento ―si el escritorio recibe un `XDG_CURRENT_DESKTOP` raro, se nota al entrar―, mientras que `uxsm entry -i` escribe un fichero en `/usr/local/share/xsessions`, como root, que el display manager va a usar en cada inicio de sesión a partir de entonces. Un nombre de menos ahí no se ve al escribirlo: se ve semanas después, cuando algo del autostart ha dejado de arrancar.

Y el nombre que se pierde es, casi siempre, el propio del escritorio: es el que acaban mirando las entradas de autostart con `OnlyShowIn=bspwm`, los portales y todo lo que se configura por escritorio. Así que la orden que se ejecuta una vez obedece, y la que deja algo escrito para siempre pide que se lo confirmen. Cuando tirarlo es justo lo que se busca ―una sesión que quiere pasar por otra cosa―, `-force-names` lo hace sin preguntar, y `uxsm entry` sin `-i` enseña antes lo que escribiría.

Y `--exec -- comando` es el camino de quien no quiere nada de esto: genera la entrada a partir del comando y ya está.

Lo que la tabla nunca hace es inventar. Si una sesión no está en ella, o no tiene un comando que valga en todas las distribuciones, `uxsm entry --exec <nombre>` y `--plain <nombre>` lo dicen y no escriben nada:

```
uxsm: notawm: not in uxsm's table of known desktops, or no command known for it
```

La forma normal, `uxsm entry <nombre>`, no necesita la tabla para el comando: la entrada generada arranca la entrada original con `uxsm start <nombre>.desktop`, y es esa entrada la que lleva el comando.

### Instalarla y que el display manager la lea

Sin `-i`, la orden es una previsualización. Con `-i` escribe en `/usr/local/share/xsessions`; hace falta ejecutarla con permisos de root. No sobrescribe ni oculta otra entrada con el mismo ID sin `-f`.

No todos los display managers leen ese directorio, ni el de Wayland que le hace pareja, `/usr/local/share/wayland-sessions`, donde van las entradas que se escriben a mano ―la de uwsm para su compositor, por ejemplo―. uxsm deja leídos los dos: en LightDM son la misma lista, y arreglar sólo uno dejaría la máquina a medias.

- `uxsm check` identifica el display manager activo y enseña de dónde obtiene su lista;
- `uxsm setup sessions-dir` calcula el cambio para LightDM y SDDM, y sólo lo aplica con `-i`;
- para GDM explica el cambio necesario en `XDG_DATA_DIRS`, pero no modifica su unidad.

Después de `-i` hay que reiniciar el display manager, o la máquina: su configuración se lee al arrancar. uxsm lo dice al terminar y no lo reinicia por su cuenta, porque eso se llevaría por delante la sesión gráfica desde la que se está ejecutando. Lo que pasa si no se hace está en [`troubleshooting.md`](troubleshooting.md).
