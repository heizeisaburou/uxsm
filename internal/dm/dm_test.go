package dm

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeSystem crea un árbol de configuración con files y un systemctl falso
// que responde con answers, por argumentos.
func fakeSystem(t *testing.T, files map[string]string, answers map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for p, text := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldRoot, oldCtl := root, systemctl
	root = dir
	systemctl = func(args ...string) (string, error) {
		if out, ok := answers[strings.Join(args, " ")]; ok {
			return out, nil
		}
		// Cualquier unidad por la que no se pregunte no existe.
		if args[0] == "show" {
			return "LoadState=not-found\n", nil
		}
		return "", nil
	}
	t.Cleanup(func() { root, systemctl = oldRoot, oldCtl })
}

const activeLightDM = "show -p Id -p LoadState display-manager.service"

func TestActiveLightDM(t *testing.T) {
	answers := map[string]string{activeLightDM: "Id=lightdm.service\nLoadState=loaded\n"}

	// Sin nada que ponga sessions-directory: el valor de serie, sin /usr/local.
	fakeSystem(t, map[string]string{
		"/usr/share/lightdm/lightdm.conf.d/01_debian.conf": "[Seat:*]\ngreeter-session=lightdm-greeter\n",
	}, answers)
	r, err := Active()
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "LightDM" || r.Origin != "compiled default" || r.Reads(LocalXSessions) {
		t.Errorf("default LightDM: %+v", r)
	}

	// El orden de lectura comprobado con `lightdm --show-config`: gana el
	// último, lightdm.conf; sin él, el de /etc/lightdm/lightdm.conf.d.
	files := map[string]string{
		"/usr/share/lightdm/lightdm.conf.d/10-a.conf": "[LightDM]\nsessions-directory=/A\n",
		"/etc/xdg/lightdm/lightdm.conf.d/10-b.conf":   "[LightDM]\nsessions-directory=/B\n",
		"/etc/lightdm/lightdm.conf.d/10-c.conf":       "[LightDM]\nsessions-directory=/C\n",
		"/etc/lightdm/lightdm.conf":                   "[Seat:*]\nsessions-directory=/ignored\n",
	}
	fakeSystem(t, files, answers)
	if r, _ := Active(); r.Origin != "/etc/lightdm/lightdm.conf.d/10-c.conf" || !reflect.DeepEqual(r.Dirs, []string{"/C"}) {
		t.Errorf("drop-ins: %+v", r)
	}
	files["/etc/lightdm/lightdm.conf"] = "[LightDM]\nsessions-directory=/D:/usr/local/share/xsessions/\n"
	fakeSystem(t, files, answers)
	if r, _ := Active(); r.Origin != "/etc/lightdm/lightdm.conf" || !r.Reads(LocalXSessions) {
		t.Errorf("lightdm.conf: %+v", r)
	}
}

func TestActiveOthers(t *testing.T) {
	fakeSystem(t, nil, map[string]string{activeLightDM: "Id=greetd.service\nLoadState=loaded\n"})
	var unknown *UnknownError
	if _, err := Active(); !errors.As(err, &unknown) || unknown.Unit != "greetd.service" {
		t.Errorf("greetd: %v", err)
	}

	fakeSystem(t, nil, map[string]string{activeLightDM: "Id=display-manager.service\nLoadState=not-found\n"})
	if _, err := Active(); !errors.Is(err, ErrNoDisplayManager) {
		t.Errorf("no display manager: %v", err)
	}

	// SDDM de serie ya lee /usr/local/share/xsessions.
	fakeSystem(t, nil, map[string]string{activeLightDM: "Id=sddm.service\nLoadState=loaded\n"})
	if r, err := Active(); err != nil || !r.Reads(LocalXSessions) || r.Origin != "compiled default" {
		t.Errorf("default SDDM: %+v, %v", r, err)
	}
}

func TestGDM(t *testing.T) {
	answers := map[string]string{
		activeLightDM:                    "Id=gdm3.service\nLoadState=loaded\n",
		"show -p LoadState gdm3.service": "LoadState=loaded\n",
		"show-environment":               "LANG=C.UTF-8\nPATH=/usr/bin\n",
		"show -p Environment -p EnvironmentFiles gdm3.service": "Environment=\nEnvironmentFiles=\n",
	}
	fakeSystem(t, nil, answers)
	r, err := Active()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/local/share/xsessions", "/usr/share/xsessions"}
	if !reflect.DeepEqual(r.Dirs, want) || r.Origin != "as systemd launches gdm3.service" {
		t.Errorf("default GDM: %+v", r)
	}

	// EnvironmentFile= pisa a Environment=.
	answers["show -p Environment -p EnvironmentFiles gdm3.service"] =
		"Environment=\"XDG_DATA_DIRS=/opt/a:/usr/share\" FOO=1\nEnvironmentFiles=/etc/default/gdm (ignore_errors=yes)\n"
	fakeSystem(t, map[string]string{"/etc/default/gdm": "# comment\nXDG_DATA_DIRS=\"/opt/b:/usr/share\"\n"}, answers)
	if r, _ := Active(); !reflect.DeepEqual(r.Dirs, []string{"/opt/b/xsessions", "/usr/share/xsessions"}) || r.Reads(LocalXSessions) {
		t.Errorf("GDM with XDG_DATA_DIRS: %+v", r)
	}
}

func TestSetupSessionsDir(t *testing.T) {
	// Nadie pone la opción: se crea el fichero propio, con la lista de serie y
	// cada directorio local delante de su hermano de /usr/share.
	fakeSystem(t, nil, nil)
	changes, err := SetupSessionsDir("lightdm")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("LightDM gave %d changes, want one", len(changes))
	}
	c := changes[0]
	want := "[LightDM]\nsessions-directory=/usr/share/lightdm/sessions:/usr/local/share/xsessions:" +
		"/usr/share/xsessions:/usr/local/share/wayland-sessions:/usr/share/wayland-sessions\n"
	if c.File != "/etc/lightdm/lightdm.conf.d/99-uxsm.conf" || c.Old != "" || !strings.Contains(c.New, want) {
		t.Errorf("new file: %+v", c)
	}
	if err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	r, _ := lightdm.read()
	if len(r.Missing()) != 0 {
		t.Errorf("after Apply LightDM still misses %v: %+v", r.Missing(), r)
	}
	if changes, _ := SetupSessionsDir("lightdm"); len(changes) != 0 {
		t.Errorf("second run should change nothing: %+v", changes)
	}

	// SDDM tiene una opción por tipo de sesión, y sus valores de serie ya
	// traen los dos directorios locales.
	fakeSystem(t, nil, nil)
	if changes, _ := SetupSessionsDir("sddm"); len(changes) != 0 {
		t.Errorf("SDDM with its defaults should change nothing: %+v", changes)
	}

	// Un fichero del usuario pone una de las dos: se cambia en ese mismo
	// fichero, sin tocar las demás líneas, y la otra se queda como está.
	user := "/etc/sddm.conf.d/10-mine.conf"
	fakeSystem(t, map[string]string{user: "[Theme]\nCurrent=x\n\n[X11]\nSessionDir=/opt/sessions\nMinimumVT=1\n"}, nil)
	changes, err = SetupSessionsDir("sddm")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].File != user ||
		changes[0].New != "[Theme]\nCurrent=x\n\n[X11]\nSessionDir=/opt/sessions,/usr/local/share/xsessions\nMinimumVT=1\n" {
		t.Errorf("user file: %+v", changes)
	}

	// Un fichero de un paquete la pone: no se toca, se escribe el propio.
	fakeSystem(t, map[string]string{"/usr/lib/sddm/sddm.conf.d/10-distro.conf": "[X11]\nSessionDir=/usr/share/xsessions\n"}, nil)
	changes, _ = SetupSessionsDir("sddm")
	if len(changes) != 1 || changes[0].File != "/etc/sddm.conf.d/99-uxsm.conf" ||
		!strings.Contains(changes[0].New, "SessionDir=/usr/local/share/xsessions,/usr/share/xsessions\n") {
		t.Errorf("package file: %+v", changes)
	}

	// Y si faltan las dos, salen en el mismo fichero, cada una en su grupo.
	fakeSystem(t, map[string]string{
		"/usr/lib/sddm/sddm.conf.d/10-distro.conf": "[X11]\nSessionDir=/usr/share/xsessions\n" +
			"[Wayland]\nWaylandSessionDir=/usr/share/wayland-sessions\n",
	}, nil)
	changes, _ = SetupSessionsDir("sddm")
	if len(changes) != 1 {
		t.Fatalf("SDDM missing both gave %d changes, want one file: %+v", len(changes), changes)
	}
	for _, want := range []string{"[X11]", "SessionDir=/usr/local/share/xsessions,/usr/share/xsessions",
		"[Wayland]", "WaylandSessionDir=/usr/local/share/wayland-sessions,/usr/share/wayland-sessions"} {
		if !strings.Contains(changes[0].New, want) {
			t.Errorf("the file written does not have %q:\n%s", want, changes[0].New)
		}
	}

	if _, err := SetupSessionsDir("gdm"); err == nil {
		t.Error("GDM cannot be set up")
	}
}
