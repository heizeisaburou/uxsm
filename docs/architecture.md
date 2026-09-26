# How uxsm works

uxsm turns an X11 session into a set of `systemd --user` units. Instead of remaining an opaque child of the display manager, the desktop becomes the main process of a service. It receives an explicitly prepared environment and activates the standard graphical-session targets. When the desktop exits or the display manager's session process disappears, systemd stops the whole session and uxsm restores the previous environment.

This document is for users who want to understand what uxsm changes on their system: which units it creates, how it names them, which environment the desktop receives, when the session becomes ready, and how display-manager integration works. The diagrams show representative paths rather than every error and option combination. Their DOT sources live beside the SVG files in `docs/flows`

For build, test, and packaging details, see [Development](development.md).

## Starting a session

`uxsm start` accepts either a session entry or a command:

```sh
uxsm start bspwm.desktop       # read Exec= and DesktopNames= from the entry
uxsm start -D bspwm -- bspwm   # run this command directly
```

A single argument ending in `.desktop`, without `--`, is treated as an entry ID. uxsm searches the `xsessions` subdirectory of each XDG data directory and uses the first match. Every other form is a command, whose executable must be available in `PATH`.

In either case, uxsm:

1. chooses the instance ID: `bspwm.desktop` for an entry or `bspwm` for a command;
2. builds the session's XDG identity;
3. waits for any previous uxsm or uwsm session to finish cleaning up;
4. saves the login environment, identity, and direct command, when present, under `$XDG_RUNTIME_DIR/uxsm`
5. starts a unit that watches the PID owned by the display manager; and
6. replaces itself with `systemctl --user start --wait uxsm-desktop@ID.service`.

The wait in step 3 checks uxsm and uwsm units by name, not `graphical-session.target`. Any desktop may activate that standard target; NixOS's session wrapper does so before running the entry's `Exec=`. Two graphical sessions for one user cannot safely coexist because they would share both the user manager and its environment.

![Session startup](flows/session-start.svg)

[DOT source](flows/session-start.dot)

The service template contains an instance ID rather than a desktop command. It runs `uxsm aux exec %i`, which:

- reopens the entry and reads its `Exec=` when the ID ends in `.desktop`; or
- reads the argument vector saved by `uxsm start` for a direct command.

Finally, `aux exec` calls `exec(2)`. The desktop itself becomes the service's main process, so systemd detects its exit without an intermediate process.

### Session identity

Without `-e`, uxsm combines desktop names in this order:

1. `XDG_CURRENT_DESKTOP` inherited from the display manager;
2. `DesktopNames=` from the session entry; and
3. names supplied with `-D`.

Duplicates are removed. If the result is empty, uxsm falls back to the executable name. With `-e`, the other sources are discarded and `-D` is required.

The result determines `XDG_CURRENT_DESKTOP`, `XDG_SESSION_DESKTOP`, `XDG_MENU_PREFIX`, and `XDG_SESSION_TYPE=x11`.

## Unit graph and lifecycle

![Units and shutdown](flows/systemd-lifecycle.svg)

[DOT source](flows/systemd-lifecycle.dot)

While a session is running, `systemctl --user list-units 'uxsm*'` shows its units and `uxsm check is-active -v` lists the active ones.

| Unit | Responsibility |
| --- | --- |
| `uxsm-bindpid@PID.service` | Watches the display manager's session process through `pidfd`. |
| `uxsm-env@ID.service` | Prepares the environment before startup and restores it on shutdown. |
| `uxsm-desktop@ID.service` | Runs the desktop as its main process and waits for readiness. |
| `uxsm-session@ID.target` | Represents the uxsm session and activates `graphical-session.target`. |
| `uxsm-autostart@ID.target` | Activates `xdg-desktop-autostart.target` when uxsm owns autostart. |
| `app-uxsm.slice` and peers | Contain applications launched through `uxsm app`. |
| `uxsm-shutdown.target` | Conflicts with active units and coordinates shutdown. |

The same shutdown path runs when:

- the desktop exits or fails;
- the display manager kills the process waiting for the session; or
- someone runs `uxsm stop`.

The first two cases activate `uxsm-shutdown.target` through `OnSuccess=` or `OnFailure=`. `uxsm stop` activates it directly. Its conflicts stop the desktop, graphical targets, and environment service; that service's `ExecStopPost=` always attempts to restore the previous state.

### Readiness

Starting the desktop process does not immediately make the session ready. `uxsm-desktop@ID.service` runs `uxsm aux wait-ready` from `ExecStartPost=`, and systemd keeps the service in its starting state until that command returns. The session targets are ordered after the service, so applications that start with `graphical-session.target` see a working desktop.

Either of two events marks the session ready:

- **uxsm detects the window manager.** It verifies the EWMH `_NET_SUPPORTING_WM_CHECK` property on the root window and the referenced window's matching self-reference. The second check rejects a stale marker left by a crashed window manager. uxsm speaks the small required part of the X11 protocol directly, avoiding a runtime dependency on libX11 or `xprop`.
- **The desktop calls `uxsm finalize`.** This mirrors `uwsm finalize` and supports desktops that do not publish the EWMH marker or deliberately want to signal readiness later.

Both paths create the same file under `$XDG_RUNTIME_DIR/uxsm/ready` with `O_EXCL`, so only the first event changes state. Calling `uxsm finalize` after that is successful and reports that the session was already ready. A new session removes any stale signal before startup.

If neither event arrives within `TimeoutStartSec=30`, the desktop service fails, `OnFailure=` shuts the session down, and the display manager returns to the login screen. A session that intentionally runs a single X11 program without a window manager can disable the readiness check with a unit override:

```ini
# ~/.config/systemd/user/uxsm-desktop@.service.d/no-wait-ready.conf
[Service]
ExecStartPost=
```

### XDG autostart

After readiness, the desktop's second `ExecStartPost=` starts `uxsm-autostart@ID.target`, which pulls in `xdg-desktop-autostart.target`. `systemd-xdg-autostart-generator` creates one `app-<name>@autostart.service` per entry. At startup, each generated unit evaluates `OnlyShowIn=` and `NotShowIn=` against the user manager's `XDG_CURRENT_DESKTOP`. Autostart applications therefore start after the desktop is visible and stop with `graphical-session.target`.

Like uwsm, uxsm enables XDG autostart by default. Disable it when the desktop starts those entries itself:

```sh
uxsm start --no-autostart bspwm.desktop
```

Generated entries get this option automatically when the [known desktop table](#the-known-desktop-table) says that the session runs its own XDG autostart. This describes behavior, not desktop type: session managers such as Xfce, GNOME, Plasma, and MATE do it, while window managers and sessions with unrelated startup scripts generally do not. For example, `icewm-session` runs `~/.icewm/startup` but does not process the XDG autostart directories.

```ini
Exec=uxsm start --no-autostart -D XFCE -- startxfce4
```

The distinction matters because neither a desktop session manager nor systemd checks whether the other has already started an entry. With both enabled under Xfce, every entry runs twice. For an unknown desktop, uxsm makes no assumption: it enables autostart, and users who observe duplicates can add `--no-autostart`.

The generator normally places its units in `app.slice`. Before starting the target, uxsm writes the runtime drop-in `$XDG_RUNTIME_DIR/systemd/user/app-@autostart.service.d/uxsm-tweaks.conf`, moving them to `app-uxsm.slice` and adding the lifecycle relationships needed to restart autostart independently. uxsm removes the drop-in during shutdown. Installing it system-wide would also alter non-uxsm sessions, including uwsm and full desktop sessions, so it must exist only at runtime.

The startup decision is stored in `$XDG_RUNTIME_DIR/uxsm/autostart` and read by `uxsm aux autostart`. A systemd `Condition*=` cannot replace that helper because dependencies are scheduled before conditions are evaluated. uxsm also cannot start `xdg-desktop-autostart.target` directly because that target has `RefuseManualStart=`; its own target must pull it in.

## Applications

```sh
uxsm app -- kitty                             # command
uxsm app firefox.desktop                      # application entry
uxsm app firefox.desktop:new-private-window   # entry action
uxsm app -s b -t service -- fcitx5            # background service
uxsm app -p TimeoutStopSec=5 -- discord       # custom stop timeout
```

`uxsm app` gives each application its own unit in one of the session's slices. The application is visible separately in `systemctl --user`, can receive resource limits, logs under its unit name, and stops with the session. This is the X11 equivalent of `uwsm app`.

Application units follow systemd's naming convention: `app-<launcher>-<application>-<unique-part>`. For example, a scope may be named `app-uxsm-kitty-3f2a1b0c.scope`; a service uses `@` before its unique part. A scope, the default, attaches an existing process to the unit. With `-t service`, the user manager starts the process. The uwsm-compatible options include `-s` for the slice, `-a`, `-u`, and `-d` for naming and description, and `-S` to discard service output.

Repeatable `-p Key=Value` arguments pass systemd properties through in the same form accepted by `systemd-run`. Examples include `TimeoutStopSec=5`, `MemoryMax=2G`, and `CPUQuota=50%`. Scopes accept resource controls and timeouts; service-only settings such as `Restart=` also require `-t service`. uxsm validates the `Key=Value` shape and leaves property semantics to systemd.

The three application classes map to these slices:

| Slice                   | Use                                         |
| ----------------------- | ------------------------------------------- |
| `app-uxsm.slice`        | Regular applications; the default.          |
| `background-uxsm.slice` | Background processes such as input methods. |
| `session-uxsm.slice`    | Session components such as panels.          |

In systemd unit names, a hyphen represents hierarchy. These slices therefore belong below the standard `app.slice`, `background.slice`, and `session.slice`. Their names include `uxsm` because the package installs them and cannot own the same files as uwsm, whose equivalents use `-graphical`.

For an application entry, uxsm reads `Exec=` from the entry or requested action, expands field codes using the supplied files or URLs, and honors `Path=`. It currently rejects `Terminal=true` instead of starting the program without a terminal.

`uxsm check is-active` uses its exit status to report whether a session is running; `-v` also prints the units. This differs from `uxsm check`, which inspects whether the system is configured for uxsm and prints a report.

## Session environment

User services inherit the environment of `systemd --user`, not that of the process requesting them. `uxsm start` therefore saves the environment received from the display manager, and `uxsm-env@.service` applies it to the user manager before starting the desktop.

![Preparing and restoring the environment](flows/environment.svg)

[DOT source](flows/environment.dot)

During preparation, uxsm:

1. stores a filtered snapshot of the user-manager environment in `env_pre`;
2. overlays the login environment on that snapshot;
3. runs an embedded `/bin/sh` loader for `/etc/profile`, `~/.profile`, the XDG identity, and uxsm environment files;
4. computes which variables to set and unset, recording session-owned names in `env_cleanup`; and
5. updates the systemd environment and, for `dbus-daemon`, the D-Bus activation environment. `dbus-broker` delegates activation to systemd already.

uxsm loads environment files from low to high priority while walking `XDG_DATA_DIRS`, `XDG_CONFIG_DIRS`, and `XDG_CONFIG_HOME`. In each location it loads `uxsm/env`, then one `uxsm/env-<desktop>` file for every `XDG_CURRENT_DESKTOP` name. Each file's `.d` directory follows in lexical order. Backup and example suffixes such as `*.bak`, `*.disabled`, and `*.sample` are ignored.

During shutdown, uxsm removes variables created for the session, restores every value from `env_pre`, and deletes its runtime files. SSH agent variables are preserved explicitly.

## Session entries and display managers

### Generating an entry

`uxsm entry` generates three kinds of entry:

```sh
uxsm entry bspwm                       # bspwm-uxsm.desktop wraps bspwm.desktop
uxsm entry --exec bspwm                # bspwm-uxsm.desktop runs bspwm.desktop's command
uxsm entry --plain --from-table bspwm  # plain bspwm.desktop from the built-in table
uxsm entry --exec -- mywm --flag       # uxsm entry for an explicit command
```

![Session entry generation](flows/session-entries.svg)

[DOT source](flows/session-entries.dot)

The declared source may be an installed entry, an explicit command, or the known desktop table. The generator rejects entries that already invoke uxsm, sessions that already start through `systemd --user`, and meta-sessions that only run a user's personal session script.

### The known desktop table

uxsm includes data for 38 desktop and window-manager sessions collected from Arch Linux, Debian 13, Ubuntu 24.04, and Fedora 43 packages. Each record may contain the login-screen name and description, `DesktopNames=`, a portable startup command, and whether the session starts XDG autostart itself. Eighteen of the 38 sessions own their autostart.

The table has two purposes:

- fill metadata omitted by an installed entry; and
- decide whether a generated uxsm entry needs `--no-autostart`.

Installed-entry values always win over the table. Command-line values win over both: `-N` sets the name, `-C` the comment, and `-D` the desktop names.

The entry source is always explicit:

```sh
uxsm entry bspwm                            # installed bspwm.desktop
uxsm entry --exec --from-table bspwm        # command and metadata from the table
uxsm entry --plain --from-table bspwm       # plain entry from the table
uxsm entry --exec -- mywm --flag            # explicit command only
```

`--from-table` is useful when the machine has no installed entry. Without it, `uxsm entry --exec bspwm` requires `bspwm.desktop`; if the entry is missing, uxsm reports the problem and suggests `--from-table`. It never changes sources silently. A plain entry cannot sensibly wrap an already installed plain entry, so `--plain` requires either `--from-table` or an explicit command.

Table-based completion is independent of the source. Unless `--no-table` is used, the table fills missing desktop names, display name, and comment:

```sh
uxsm entry --no-table bspwm  # use only the installed entry and command-line metadata
```

If no desktop name remains, uxsm requests `-D` instead of inventing one. Contradictory combinations, such as `--from-table` with `--no-table` or with an explicit command, are rejected with an explanation.

With `-e`, only names supplied through `-D` are kept, just as with `uxsm start`:

```sh
uxsm entry -e -D MyWM bspwm  # DesktopNames=MyWM, and nothing else
```

These names become `XDG_CURRENT_DESKTOP`; dropping the desktop's normal identity also excludes matching `OnlyShowIn=` autostart entries and can change portal selection. Run `uxsm entry` without `-i` to review the generated file first.

The table never invents a startup command. If a session is absent or has no command valid across all supported distributions, `--from-table` fails without writing anything:

```text
uxsm: notawm: not in uxsm's table of known desktops, or no command known for it
```

The default `uxsm entry <name>` form does not need a table command: its generated entry runs `uxsm start <name>.desktop`, and that original entry contains the command.

### Installing an entry

Without `-i`, `uxsm entry` is a preview. With `-i`, it writes to `/usr/local/share/xsessions` and requires root privileges. It will not overwrite or shadow an entry with the same ID unless `-f` is also given.

Not every display manager reads that directory or its Wayland counterpart, `/usr/local/share/wayland-sessions`. uxsm configures both because LightDM uses one combined directory list; changing only one would leave the system inconsistent.

- `uxsm check` identifies the active display manager and reports its session search path.
- `uxsm setup sessions-dir` calculates the required LightDM or SDDM change and applies it only with `-i`.
- For GDM, uxsm explains the required `XDG_DATA_DIRS` change but never edits the unit.

After `uxsm setup sessions-dir -i`, restart the display manager or the machine. Display managers read this configuration only at startup. uxsm prints that instruction but does not restart the service, because doing so would terminate the graphical session in which the command was run. See [Troubleshooting](troubleshooting.md#the-session-is-missing-from-the-login-screen-or-fails-to-start) for the failure mode.
