package sessionentry

// Known son los datos de un escritorio conocido. Sirven para dos cosas: completar
// una entrada que no los trae, donde lo que traiga la entrada manda siempre, y
// generar entradas sin entrada original, con FromTable.
type Known struct {
	Name, Comment string
	DesktopNames  []string
	// Exec es la orden que arranca el escritorio, la del Exec= de su entrada
	// pero sin ruta, para que valga en cualquier distribución. Vacío si no hay
	// una que valga en todas: entonces FromTable no la puede usar.
	Exec string
}

// known son los escritorios conocidos, por ID de entrada.
//
// Sale de las entradas que traen los paquetes de Arch, Debian 13, Ubuntu 24.04
// y Fedora 43 en septiembre de 2026 (test/xsessions.sh: cien entradas
// distintas). Muchas no traen DesktopNames= en ninguna, y otras sólo en
// algunas: bspwm sí en Arch y en Debian, pero no en Ubuntu ni en Fedora. Cada
// nombre sale, por este orden de preferencia, de:
//
//  1. El DesktopNames= de alguna de esas distribuciones.
//  2. El XDG_CURRENT_DESKTOP que pone el propio arrancador de la sesión, para
//     que uxsm no ponga en el gestor otro nombre que el que ven los procesos
//     del escritorio: cinnamon-session lo toma de DesktopName= en su .session,
//     startlxde exporta LXDE y dde-session, DDE.
//  3. Si nadie pone ninguno, que es lo que pasa con los gestores de ventanas,
//     el nombre del programa en minúsculas, que es lo que usan los que sí lo
//     declaran (i3, bspwm, dwm, qtile, cwm, spectrwm).
//
// Name= y Comment= salen de las mismas entradas.
var known = map[string]Known{
	// Entornos de escritorio.
	"budgie-desktop.desktop": {
		Name: "Budgie Desktop", Comment: "This session logs you into the Budgie Desktop",
		DesktopNames: []string{"Budgie", "GNOME"}, // Debian, Ubuntu; Fedora sólo Budgie
		Exec:         "budgie-desktop",
	},
	"cinnamon.desktop": {
		Name: "Cinnamon", Comment: "This session logs you into Cinnamon",
		DesktopNames: []string{"X-Cinnamon"}, // cinnamon.session
		Exec:         "cinnamon-session-cinnamon",
	},
	"cinnamon2d.desktop": {
		Name: "Cinnamon (Software Rendering)", Comment: "This session logs you into Cinnamon (using software rendering)",
		DesktopNames: []string{"X-Cinnamon"}, // cinnamon2d.session
		Exec:         "cinnamon-session-cinnamon2d",
	},
	"deepin.desktop": {
		Name: "deepin", Comment: "Deepin Desktop Environment",
		DesktopNames: []string{"DDE"}, // dde-session
		Exec:         "dde-session",
	},
	"enlightenment.desktop": {
		Name: "Enlightenment", Comment: "Log in using Enlightenment",
		DesktopNames: []string{"Enlightenment"}, // todas
		Exec:         "enlightenment_start",
	},
	// Sin Exec: la orden es "env GNOME_SHELL_SESSION_MODE=classic gnome-session",
	// y con un comando la instancia sería "env".
	"gnome-classic-xorg.desktop": {
		Name: "GNOME Classic on Xorg", Comment: "This session logs you into GNOME Classic",
		DesktopNames: []string{"GNOME-Classic", "GNOME"}, // todas
	},
	// Sin Exec: el programa no está en PATH y su ruta cambia con la
	// distribución (/usr/lib en Arch, /usr/libexec en Debian).
	"gnome-flashback-metacity.desktop": {
		Name: "GNOME Flashback (Metacity)", Comment: "This session logs you into GNOME Flashback with Metacity",
		DesktopNames: []string{"GNOME-Flashback", "GNOME"}, // todas
	},
	"gnome-xorg.desktop": {
		Name: "GNOME on Xorg", Comment: "This session logs you into GNOME",
		DesktopNames: []string{"GNOME"}, // todas
		Exec:         "gnome-session",
	},
	"LXDE.desktop": {
		Name: "LXDE", Comment: "LXDE - Lightweight X11 desktop environment",
		DesktopNames: []string{"LXDE"}, // startlxde
		Exec:         "startlxde",
	},
	"lxqt.desktop": {
		Name: "LXQt Desktop", Comment: "Lightweight Qt Desktop",
		DesktopNames: []string{"LXQt"}, // todas
		Exec:         "startlxqt",
	},
	"mate.desktop": {
		Name: "MATE", Comment: "This session logs you into MATE",
		DesktopNames: []string{"MATE"}, // todas
		Exec:         "mate-session",
	},
	// La sesión X11 de Plasma se llama plasmax11.desktop desde Plasma 6 y
	// plasma.desktop antes, como en Ubuntu 24.04.
	"plasma.desktop": {
		Name: "Plasma (X11)", Comment: "Plasma by KDE",
		DesktopNames: []string{"KDE"}, // Ubuntu
		Exec:         "startplasma-x11",
	},
	"plasmax11.desktop": {
		Name: "Plasma (X11)", Comment: "Plasma by KDE",
		DesktopNames: []string{"KDE"}, // Arch, Debian, Fedora
		Exec:         "startplasma-x11",
	},
	"xfce.desktop": {
		Name: "Xfce Session", Comment: "Use this session to run Xfce as your desktop environment",
		DesktopNames: []string{"XFCE"}, // todas
		Exec:         "startxfce4",
	},

	// Un gestor de ventanas dentro de un escritorio. Su Exec= es un script que
	// arranca el gestor de ventanas y después el gestor de sesión del
	// escritorio, así que el nombre es el del escritorio que corre, no el del
	// gestor de ventanas ni el del script: sawfish-mate-session acaba en
	// mate-session, y openbox-gnome-session, en una sesión de gnome-session con
	// DesktopName=GNOME.
	//
	// Faltan a propósito openbox-kde, e16-kde-session y sawfish-kde4, que
	// acaban en startkde, un programa que Plasma ya no trae; e16-gnome2-session
	// y e16-gnome3-session, que son de Fedora, donde GNOME ya no tiene sesión
	// X11; y sawfish-lumina, porque no está claro qué nombre pone Lumina.
	"openbox-gnome.desktop": {
		Name: "GNOME/Openbox", Comment: "Use the Openbox window manager inside of the GNOME desktop environment",
		DesktopNames: []string{"GNOME"}, // openbox-gnome.session
		Exec:         "openbox-gnome-session",
	},
	"sawfish-kde5.desktop": {
		Name: "Sawfish/KDE5", Comment: "Use the Sawfish window manager inside of the KDE5 desktop environment",
		DesktopNames: []string{"KDE"}, // startplasma-x11
		Exec:         "sawfish-kde5-session",
	},
	"sawfish-mate.desktop": {
		Name: "Sawfish/MATE", Comment: "Use the Sawfish window manager inside of the MATE desktop environment",
		DesktopNames: []string{"MATE"}, // mate-session
		Exec:         "sawfish-mate-session",
	},
	"sawfish-xfce.desktop": {
		Name: "Sawfish/XFCE", Comment: "Use the Sawfish window manager inside of the XFCE desktop environment",
		DesktopNames: []string{"XFCE"}, // startxfce4
		Exec:         "sawfish-xfce-session",
	},
	"xmonad-mate.desktop": {
		Name: "xmonad-mate", Comment: "Tiling window manager",
		DesktopNames: []string{"MATE"}, // mate-session
		Exec:         "xmonad-start mate-session",
	},

	// Gestores de ventanas.
	"awesome.desktop": {
		Name: "awesome", Comment: "Highly configurable framework window manager",
		DesktopNames: []string{"awesome"}, // ninguna
		Exec:         "awesome",
	},
	"blackbox.desktop": {
		Name: "blackbox", Comment: "This session logs you into Blackbox",
		DesktopNames: []string{"blackbox"}, // ninguna
		Exec:         "blackbox",
	},
	"bspwm.desktop": {
		Name: "bspwm", Comment: "Binary space partitioning window manager",
		DesktopNames: []string{"bspwm"}, // Arch, Debian
		Exec:         "bspwm",
	},
	"cwm.desktop": {
		Name: "cwm", Comment: "Lightweight window manager for X11",
		DesktopNames: []string{"cwm"}, // Fedora
		Exec:         "cwm",
	},
	"dwm.desktop": {
		Name: "dwm", Comment: "dynamic window manager",
		DesktopNames: []string{"dwm"}, // Debian
		Exec:         "dwm",
	},
	"fluxbox.desktop": {
		Name: "fluxbox", Comment: "Highly configurable and low resource X11 Window manager",
		DesktopNames: []string{"fluxbox"}, // ninguna
		Exec:         "startfluxbox",
	},
	"fvwm3.desktop": {
		Name: "FVWM3", Comment: "F? Virtual Window Manager",
		DesktopNames: []string{"FVWM3", "FVWM"}, // Arch, Debian
		Exec:         "fvwm3",
	},
	"herbstluftwm.desktop": {
		Name: "herbstluftwm", Comment: "Manual tiling window manager",
		DesktopNames: []string{"herbstluftwm"}, // ninguna
		Exec:         "herbstluftwm",
	},
	"i3.desktop": {
		Name: "i3", Comment: "improved dynamic tiling window manager",
		DesktopNames: []string{"i3"}, // todas
		Exec:         "i3",
	},
	"icewm.desktop": {
		Name: "IceWM", Comment: "Simple and fast window manager",
		DesktopNames: []string{"ICEWM"}, // todas
		Exec:         "icewm",
	},
	"icewm-session.desktop": {
		Name: "IceWM Session", Comment: "This session logs you into IceWM",
		DesktopNames: []string{"ICEWM"}, // todas
		Exec:         "icewm-session",
	},
	"jwm.desktop": {
		Name: "JWM", Comment: "Minimalistic pure X11 window manager with menu/tray support",
		DesktopNames: []string{"jwm"}, // ninguna
		Exec:         "jwm",
	},
	"openbox.desktop": {
		Name: "Openbox", Comment: "Log in using the Openbox window manager (without a session manager)",
		DesktopNames: []string{"openbox"}, // ninguna
		Exec:         "openbox-session",
	},
	"pekwm.desktop": {
		Name: "PekWM", Comment: "Tabbed X11 window manager",
		DesktopNames: []string{"pekwm"}, // ninguna
		Exec:         "pekwm",
	},
	"qtile.desktop": {
		Name: "Qtile", Comment: "Qtile Session",
		DesktopNames: []string{"qtile"}, // Arch
		Exec:         "qtile start",
	},
	"spectrwm.desktop": {
		Name: "spectrwm", Comment: "The spectrwm window manager",
		DesktopNames: []string{"spectrwm"}, // Debian
		Exec:         "spectrwm",
	},
	"stumpwm.desktop": {
		Name: "Stumpwm", Comment: "Tiling, keyboard driven Common Lisp window manager",
		DesktopNames: []string{"stumpwm"}, // ninguna
		Exec:         "stumpwm",
	},
	"wmaker.desktop": {
		Name: "Window Maker", Comment: "This session logs you into Window Maker",
		DesktopNames: []string{"WindowMaker"}, // todas
		Exec:         "wmaker",
	},
	"xmonad.desktop": {
		Name: "Xmonad", Comment: "Lightweight X11 tiled window manager written in Haskell",
		DesktopNames: []string{"xmonad"}, // ninguna
		Exec:         "xmonad",
	},
}
