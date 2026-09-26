# Cuando algo no va

Problemas encontrados al usar uxsm en máquinas reales, sus causas y soluciones, más algunos ajustes útiles.

## Troubleshooting

### La sesión no sale en la pantalla de inicio, o sale y falla al elegirla

Después de ejecutar `uxsm setup sessions-dir -i`, hay que **reiniciar el display manager** o la máquina. El display manager solo lee su configuración al arrancar, así que el proceso actual no conoce el directorio nuevo.

La sesión puede **aparecer y, aun así, no arrancar**. El greeter es un proceso nuevo en cada inicio de sesión y lee los directorios por su cuenta; por eso puede ofrecer la sesión aunque el display manager todavía no sepa iniciarla. Al elegirla, LightDM se queda sin ningún comando que ejecutar:

```
lightdm[…]: session_real_run: assertion 'priv->argv != NULL' failed
```

Ese error produce el aviso «failed to launch» bajo el formulario. Como LightDM ya había cerrado el greeter para iniciar la sesión y no vuelve a abrirlo, el asiento queda sin greeter ni sesión —con la pantalla en negro— hasta que se reinicia LightDM desde otra consola (`Ctrl + Alt + F2`):

```sh
sudo systemctl restart lightdm
```

### Cambiar de una sesión a otra tarda, y entre medias hay pantalla negra

Salvo que el usuario tenga activado el _lingering_, su gestor de `systemd --user` se apaga al cerrar la última sesión. La siguiente no arranca hasta que el gestor anterior termina por completo, así que cualquier aplicación que tarde en cerrarse retrasa el cambio.

Para identificarla, consulta el diario del usuario:

```sh
journalctl --user -b | grep -E "Stopping|Stopped |SIGKILL"
```

En un caso real, una aplicación dentro de un scope independiente ignoró `SIGTERM` durante treinta y cinco segundos, hasta que systemd la mató. La siguiente sesión sufrió ese retraso, aunque todas las unidades de uxsm se habían detenido en el mismo segundo en que se cerró la anterior.

Las soluciones y sus consecuencias se explican en [Que cerrar sesión no se haga esperar](#que-cerrar-sesión-no-se-haga-esperar). En resumen:

- **No hacer nada** y aceptar que el cierre de sesión tarde tanto como la aplicación más lenta.
- **Lanzar la aplicación con `uxsm app`** para vincularla a los slices de la sesión. Si permanece en su unidad, se detiene con la sesión y [`-p` permite limitar cuánto se espera](#que-nada-tarde-noventa-segundos-en-morir).
- **Reducir el plazo global** para limitar también las aplicaciones que se salen de su unidad, como las basadas en Chromium. Este es [el ajuste recomendado](#que-nada-tarde-noventa-segundos-en-morir).
- **Activar el _lingering_**, que evita la espera de otra forma, aunque tiene un efecto que no siempre resulta deseable. Es [la variante no recomendada](#lingering-la-variante-que-no-se-recomienda).

### Una aplicación del autostart falla, o se lanza dos veces

Cuando uxsm activa el autostart XDG, las entradas de `~/.config/autostart` y `/etc/xdg/autostart` también se ejecutan en las sesiones de un gestor de ventanas, donde antes nadie las iniciaba. Si la configuración del escritorio ya arrancaba esas aplicaciones —por ejemplo, un `bspwmrc` que ejecuta `picom`—, ahora se lanzan por duplicado.

Con picom, el error es este:

```
picom[…]: [ session_init FATAL ERROR ] Another composite manager is already running
```

La solución es quitarlo de la configuración del escritorio y dejar que lo inicie el autostart. Así también aparece como unidad y se puede detener y arrancar. Si prefieres que lo siga controlando el escritorio, genera su entrada de sesión con `--no-autostart` o añade esa opción al `Exec=` de la entrada existente.

Presta especial atención a los programas que tu configuración lanza **sin comprobar si ya están en ejecución**, como suele ocurrir con `sxhkd`: habrá dos copias y cada tecla ejecutará dos acciones. Debes iniciarlos desde un solo sitio.

### Una aplicación del autostart no arranca en esta sesión

Comprueba si su entrada contiene `OnlyShowIn=` o `NotShowIn=`. systemd no descarta esas entradas al crear las unidades, sino que añade una condición que se evalúa al arrancarlas comparándola con `XDG_CURRENT_DESKTOP`.

```sh
systemctl --user cat app-<nombre>@autostart.service | grep ExecCondition
systemctl --user show-environment | grep XDG_CURRENT_DESKTOP
```

uxsm obtiene esos nombres de `-D`, de `DesktopNames=` en la entrada de sesión o de su tabla de escritorios conocidos. Puedes verlos con `uxsm check is-active -v` y en el entorno del escritorio.

Si no existe ninguna unidad para la entrada, la causa suele ser otra: el generador de systemd sí descarta las entradas con `Hidden=true` o con un `TryExec=` inexistente.

## Tips and tricks

### Que cerrar sesión no se haga esperar

Hay dos formas de agilizar el cambio de sesión. La primera cierra antes los procesos que siguen vivos; la segunda evita tener que esperarlos. Solo se recomienda la primera. Para identificar primero qué aplicación causa el retraso, consulta [Cambiar de una sesión a otra tarda, y entre medias hay pantalla negra](#cambiar-de-una-sesión-a-otra-tarda-y-entre-medias-hay-pantalla-negra).

#### Que nada tarde noventa segundos en morir

El plazo predeterminado de systemd para detener una unidad es de noventa segundos; después envía `SIGKILL`. Como el gestor del usuario se apaga al cerrar su última sesión, cualquier aplicación que se resista retrasa el cambio.

Para limitar una sola aplicación que permanezca en su unidad, lánzala con `uxsm app` y usa `-p`:

```sh
uxsm app -p TimeoutStopSec=5 -- discord
```

A veces no basta con controlar esa unidad, porque la aplicación se sale de ella. Las aplicaciones basadas en Chromium —Chrome, Chromium y las aplicaciones de Electron con una versión reciente— se trasladan a un scope propio nada más arrancar: se comunican con systemd mediante D-Bus y crean `app-<nombre>-<pid>.scope` en `app.slice`, fuera de los slices de la sesión. Puede comprobarse en el binario:

```sh
strings ~/.config/discord/app-1.0.159/Discord | grep -E "StartTransientUnit|app-\$1"
app-$1-$2.scope
StartTransientUnit
```

En la unidad creada por `uxsm app` solo quedan los procesos que se bifurcaron antes del traslado. El proceso principal —el que tarda en morir— queda en el scope nuevo, con el plazo predeterminado. No hay ninguna opción de línea de comandos para desactivar este comportamiento: en el binario no aparece ningún nombre de feature asociado.

En esos casos hay que reducir el plazo de todas las unidades del usuario. Diez segundos bastan para una aplicación de escritorio:

```ini
# ~/.config/systemd/user.conf
[Manager]
DefaultTimeoutStopSec=10s
```

```sh
systemctl --user daemon-reexec
```

#### Lingering, la variante que no se recomienda

`loginctl enable-linger` mantiene el gestor de `systemd --user` activo aunque no haya ninguna sesión abierta. Así, la siguiente sesión no espera a que termine el gestor anterior, porque este permanece en ejecución.

La contrapartida es que **los procesos siguen vivos sin ninguna sesión abierta**. Todo lo que no dependa de `graphical-session.target` sobrevive al cambio y al cierre completo de sesión. Por ejemplo, un Discord lanzado en un scope independiente seguirá consumiendo recursos hasta que alguien lo mate. Además, el _lingering_ se activa por usuario, no por sesión, por lo que sus efectos persisten hasta que se desactiva.

Resulta útil en un servidor con servicios de usuario que deben funcionar sin nadie conectado. En un escritorio, si el único objetivo es reducir la espera al cerrar sesión, conviene usar primero el ajuste anterior.
