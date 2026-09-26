# Troubleshooting

Problems observed on real uxsm systems, their causes, and the relevant fixes.

## The session is missing from the login screen or fails to start

After running `uxsm setup sessions-dir -i`, **restart the display manager or the machine**. A display manager reads this configuration only at startup, so the running process does not know about the new directory.

The session may appear in the greeter and still fail to start. The greeter is a new process for each login attempt and reads the session directories itself, so it can offer an entry that the display manager daemon does not yet know how to launch. LightDM then has no command to execute:

```text
lightdm[…]: session_real_run: assertion 'priv->argv != NULL' failed
```

This produces the “failed to launch” message below the login form. LightDM has already closed the greeter and does not reopen it, leaving the seat with a black screen and no session. Switch to another virtual console with `Ctrl + Alt + F2` and restart LightDM:

```sh
sudo systemctl restart lightdm
```

## Switching sessions is slow and shows a black screen

Unless lingering is enabled, a user's `systemd --user` manager stops after their last session closes. The next session cannot start until the previous manager has stopped completely, so an application that is slow to exit delays the switch.

Use the user journal to identify it:

```sh
journalctl --user -b | grep -E "Stopping|Stopped |SIGKILL"
```

In one real case, an application in an independent scope ignored `SIGTERM` for 35 seconds before systemd killed it. The next session inherited that delay even though every uxsm unit had stopped in the same second that the previous session closed.

The available approaches and their tradeoffs are explained under [Make logout finish promptly](#make-logout-finish-promptly). In summary:

- **Do nothing** and accept that logout takes as long as the slowest application.
- **Launch the application through `uxsm app`** so it belongs to the session slices. If it remains in that unit, it stops with the session and [`-p` can limit the wait](#shorten-the-stop-timeout).
- **Reduce the global stop timeout** to cover applications that leave their original unit, including Chromium-based applications. This is the [recommended workaround](#shorten-the-stop-timeout).
- **Enable lingering** to avoid waiting by keeping the user manager alive. This is the [non-recommended alternative](#lingering-the-non-recommended-alternative).

## An autostart application fails or starts twice

When uxsm enables XDG autostart, entries from `~/.config/autostart` and `/etc/xdg/autostart` also run in window-manager sessions that did not previously start them. If the desktop configuration already launches the same programs—for example, `picom` from `bspwmrc`—they now start twice.

picom reports:

```text
picom[…]: [ session_init FATAL ERROR ] Another composite manager is already running
```

Remove the command from the desktop configuration and let XDG autostart own it. The application then also appears as a unit that can be stopped and restarted. If the desktop configuration should remain authoritative, generate its session entry with `--no-autostart` or add that option to the existing entry's `Exec=`.

Programs that start **without checking for an existing process**, as often happens with `sxhkd`, are especially important: two copies make every key binding run twice. Start them from exactly one place.

## An autostart application does not run in this session

Check whether its entry contains `OnlyShowIn=` or `NotShowIn=`. systemd does not discard these entries while generating units. Instead, it adds a condition evaluated at startup against the user manager's `XDG_CURRENT_DESKTOP`:

```sh
systemctl --user cat app-<name>@autostart.service | grep ExecCondition
systemctl --user show-environment | grep XDG_CURRENT_DESKTOP
```

uxsm derives the desktop names from `-D`, `DesktopNames=` in the session entry, or its known desktop table. Inspect them with `uxsm check is-active -v` and in the desktop environment.

If no unit exists for the entry, the cause is usually different: the systemd generator does discard entries with `Hidden=true` or a missing `TryExec=` executable.

## Tips and workarounds

### Make logout finish promptly

There are two ways to speed up a session switch. The first closes surviving processes sooner; the second avoids waiting for them at all. Only the first is recommended. To identify the application before changing anything, see [Switching sessions is slow and shows a black screen](#switching-sessions-is-slow-and-shows-a-black-screen).

#### Shorten the stop timeout

systemd's default timeout for stopping a unit is 90 seconds, after which it sends `SIGKILL`. Because the user manager stops after the last session closes, every uncooperative application delays the next session.

For one application that stays in its assigned unit, launch it through `uxsm app` and set `-p`:

```sh
uxsm app -p TimeoutStopSec=5 -- discord
```

This does not cover every Chromium-based application. Chrome, Chromium, and applications built with recent Electron versions move themselves into a new scope immediately after startup. They ask systemd over D-Bus to create `app-<name>-<pid>.scope` under `app.slice`, outside the session slices. The behavior is visible in the binary:

```sh
strings ~/.config/discord/app-1.0.159/Discord | grep -E "StartTransientUnit|app-\$1"
app-$1-$2.scope
StartTransientUnit
```

Processes forked before the move remain in the unit created by `uxsm app`, but the main process—the one that delays shutdown—uses the new scope and its default timeout. No command-line feature name associated with disabling this behavior appears in the binary.

For these applications, reduce the timeout for every user unit. Ten seconds is enough for a desktop application:

```ini
# ~/.config/systemd/user.conf
[Manager]
DefaultTimeoutStopSec=10s
```

Apply the manager setting without logging out:

```sh
systemctl --user daemon-reexec
```

#### Lingering: the non-recommended alternative

`loginctl enable-linger` keeps `systemd --user` running when no login session is open. The next session does not wait for the previous user manager because that manager never stops.

The cost is that **processes remain alive with no open session**. Anything not tied to `graphical-session.target` survives both the session switch and logout. For example, Discord in an independent scope keeps consuming resources until something kills it. Lingering is enabled per user, not per session, and remains in effect until explicitly disabled.

This is useful on a server whose user services must run without an interactive login. On a desktop, when the only goal is faster logout, use the timeout adjustment above instead.
