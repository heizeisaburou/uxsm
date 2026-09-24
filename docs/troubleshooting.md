# Cuando algo no va

Cosas que se han encontrado usando uxsm en máquinas de verdad, con lo que las
explica y lo que hay que hacer.

## La sesión no sale en la pantalla de inicio, o sale y falla al elegirla

Después de `uxsm setup sessions-dir -i` hay que **reiniciar el display manager**,
o la máquina. Su configuración se lee al arrancar, así que el que está corriendo
no conoce el directorio nuevo.

Lo peor no es que la sesión no salga, que sería evidente. Es que puede salir y no
arrancar: el greeter es un proceso nuevo en cada inicio de sesión y lee los
directorios por su cuenta, así que ofrece la sesión antes de que el display
manager sepa arrancarla. Al elegirla, LightDM se queda sin comando que ejecutar:

```
lightdm[…]: session_real_run: assertion 'priv->argv != NULL' failed
```

Eso es el aviso de «failed to launch» bajo el formulario. Y peor todavía: LightDM
ya había cerrado el greeter para dar paso a la sesión, y no lo vuelve a levantar,
así que el asiento se queda sin greeter y sin sesión ―pantalla negra― hasta
reiniciarlo desde otra consola (`Ctrl + Alt + F2`):

```sh
sudo systemctl restart lightdm
```

## Cambiar de una sesión a otra tarda, y entre medias hay pantalla negra

Salvo que el usuario tenga *lingering* activado, su gestor de `systemd --user` se
apaga al cerrarse su última sesión, y el de la sesión siguiente no arranca hasta
que el anterior termina del todo. Si alguna aplicación tarda en morir, la sesión
siguiente espera por ella.

Para saber quién es, en el diario del usuario:

```sh
journalctl --user -b | grep -E "Stopping|Stopped |SIGKILL"
```

Un caso real: una aplicación en un scope suelto aguantó treinta y cinco segundos
el `SIGTERM` antes de que systemd la matara, y la sesión siguiente tardó ese
minuto en aparecer. Las unidades de uxsm habían parado todas en el mismo segundo
en que se cerró la sesión.

Qué se puede hacer:

- **Nada**, y aceptar que cerrar sesión tarda lo que tarde la más terca.
- **Lanzar esa aplicación con `uxsm app`**, que la deja en los slices de la
  sesión: así se para con ella, en vez de sobrevivirle y estorbar al final. Y si
  aun así se hace la remolona, `-p` le pone plazo:

  ```sh
  uxsm app -p TimeoutStopSec=5 -- discord
  ```
- **Activar el lingering** (`loginctl enable-linger`): el gestor no se apaga al
  cerrar sesión, así que la siguiente entra sin esperar. A cambio, lo que no
  cuelgue de `graphical-session.target` sobrevive de una sesión a la otra, y los
  servicios de usuario siguen en marcha sin nadie delante.

## Una aplicación del autostart falla, o se lanza dos veces

Desde que uxsm activa el autostart XDG, las entradas de `~/.config/autostart` y
`/etc/xdg/autostart` arrancan también en una sesión de gestor de ventanas, donde
antes no las lanzaba nadie. Si la configuración del escritorio ya las arrancaba
―un `bspwmrc` con `picom`, por ejemplo―, ahora vienen por los dos lados.

Lo que se ve, con picom, es esto:

```
picom[…]: [ session_init FATAL ERROR ] Another composite manager is already running
```

El arreglo es quitarlo de la configuración del escritorio y dejar que lo lance el
autostart, que además así se ve como unidad y se puede parar y arrancar. Si lo
que quieres es lo contrario, que el escritorio siga mandando, genera su entrada
de sesión con `--no-autostart`, o añade esa opción al `Exec=` de la que tengas.

Ojo con un caso concreto: si tu configuración lanza algo **sin comprobar si ya
está**, como suele pasar con `sxhkd`, tendrás dos copias y cada tecla hará dos
cosas. Ahí hay que elegir uno de los dos sitios, no dejar los dos.

## Una aplicación del autostart no arranca en esta sesión

Mira si su entrada lleva `OnlyShowIn=` o `NotShowIn=`. systemd no descarta esas
entradas al crear las unidades: les pone una condición que se comprueba al
arrancarlas, comparando con `XDG_CURRENT_DESKTOP`.

```sh
systemctl --user cat app-<nombre>@autostart.service | grep ExecCondition
systemctl --user show-environment | grep XDG_CURRENT_DESKTOP
```

Los nombres que pone uxsm salen de `-D`, del `DesktopNames=` de la entrada de
sesión o de su tabla de escritorios conocidos, y se ven con
`uxsm check is-active -v` y en el entorno del escritorio.

Si la entrada no tiene unidad ninguna, el motivo suele ser otro: el generador de
systemd sí descarta al crear las que llevan `Hidden=true` o un `TryExec=` que no
existe.
