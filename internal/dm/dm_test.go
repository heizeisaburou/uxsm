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

func TestSetupXSessionsDir(t *testing.T) {
	// Nadie pone la opción: se crea el fichero propio, con la lista de serie
	// y /usr/local delante de /usr/share.
	fakeSystem(t, nil, nil)
	c, err := SetupXSessionsDir("lightdm")
	if err != nil {
		t.Fatal(err)
	}
	if c.File != "/etc/lightdm/lightdm.conf.d/99-uxsm.conf" || c.Old != "" ||
		!strings.Contains(c.New, "[LightDM]\nsessions-directory=/usr/share/lightdm/sessions:/usr/local/share/xsessions:/usr/share/xsessions:/usr/share/wayland-sessions\n") {
		t.Errorf("new file: %+v", c)
	}
	if err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	if r, _ := lightdm.read(); !r.Reads(LocalXSessions) {
		t.Errorf("after Apply LightDM does not read %s: %+v", LocalXSessions, r)
	}
	if c, _ := SetupXSessionsDir("lightdm"); c != nil {
		t.Errorf("second run should change nothing: %+v", c)
	}

	// Un fichero del usuario la pone: se cambia en ese mismo fichero, sin
	// tocar las demás líneas.
	user := "/etc/sddm.conf.d/10-mine.conf"
	fakeSystem(t, map[string]string{user: "[Theme]\nCurrent=x\n\n[X11]\nSessionDir=/opt/sessions\nMinimumVT=1\n"}, nil)
	c, err = SetupXSessionsDir("sddm")
	if err != nil {
		t.Fatal(err)
	}
	if c.File != user || c.New != "[Theme]\nCurrent=x\n\n[X11]\nSessionDir=/opt/sessions,/usr/local/share/xsessions\nMinimumVT=1\n" {
		t.Errorf("user file: %+v", c)
	}

	// Un fichero de un paquete la pone: no se toca, se crea el propio.
	fakeSystem(t, map[string]string{"/usr/lib/sddm/sddm.conf.d/10-distro.conf": "[X11]\nSessionDir=/usr/share/xsessions\n"}, nil)
	if c, _ := SetupXSessionsDir("sddm"); c == nil || c.File != "/etc/sddm.conf.d/99-uxsm.conf" ||
		!strings.Contains(c.New, "SessionDir=/usr/local/share/xsessions,/usr/share/xsessions\n") {
		t.Errorf("package file: %+v", c)
	}

	if _, err := SetupXSessionsDir("gdm"); err == nil {
		t.Error("GDM cannot be set up")
	}
}
