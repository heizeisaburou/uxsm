package appunit

import (
	"regexp"
	"strings"
	"testing"
)

func TestSlice(t *testing.T) {
	cases := []struct {
		in, want string
		bad      bool
	}{
		{in: "", want: AppSlice},
		{in: "a", want: AppSlice},
		{in: "b", want: BackgroundSlice},
		{in: "s", want: SessionSlice},
		{in: "my.slice", want: "my.slice"},
		{in: "app-graphical.slice", want: "app-graphical.slice"}, // el de uwsm, si alguien lo quiere
		{in: "x", bad: true},
		{in: "app", bad: true},
	}
	for _, c := range cases {
		got, err := Slice(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("Slice(%q) = %q, want an error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("Slice(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestName(t *testing.T) {
	scope := regexp.MustCompile(`^app-uxsm-([^@]+)-[0-9a-f]{8}\.scope$`)
	service := regexp.MustCompile(`^app-uxsm-(.+)@[0-9a-f]{8}\.service$`)

	// De una entrada sale su ID sin .desktop; de un comando, el programa.
	for _, c := range []struct {
		o    Options
		want string
	}{
		{Options{Argv: []string{"/usr/bin/kitty"}}, "kitty"},
		{Options{Argv: []string{"kitty"}, Entry: "org.gnome.Nautilus"}, "org.gnome.Nautilus"},
		{Options{Argv: []string{"kitty"}, Entry: "firefox", AppName: "mine"}, "mine"},
		// Lo que systemd no admite en un nombre de unidad se sustituye.
		{Options{Argv: []string{"some app!"}}, "some_app_"},
	} {
		got, err := c.o.Name()
		if err != nil {
			t.Fatalf("Name(%+v): %v", c.o, err)
		}
		m := scope.FindStringSubmatch(got)
		if m == nil {
			t.Errorf("Name(%+v) = %q, which is not a scope name", c.o, got)
			continue
		}
		if m[1] != c.want {
			t.Errorf("Name(%+v) = %q, want the app part to be %q", c.o, got, c.want)
		}
	}

	// Un servicio lleva la parte de azar como instancia, detrás de "@".
	got, err := Options{Argv: []string{"steam"}, Service: true}.Name()
	if err != nil || !service.MatchString(got) {
		t.Errorf("the name of a service is %q, %v", got, err)
	}

	// Dos lanzamientos de lo mismo no pueden compartir unidad.
	a, _ := Options{Argv: []string{"kitty"}}.Name()
	b, _ := Options{Argv: []string{"kitty"}}.Name()
	if a == b {
		t.Errorf("two launches got the same unit name: %q", a)
	}

	// Un nombre larguísimo se recorta hasta lo que admite systemd.
	long, err := Options{Argv: []string{strings.Repeat("x", 400)}}.Name()
	if err != nil || len(long) > 255 {
		t.Errorf("a long name gave %d characters, %v", len(long), err)
	}
}

func TestNameGiven(t *testing.T) {
	// Con -u manda quien llama, pero el sufijo tiene que cuadrar con el tipo.
	got, err := Options{Argv: []string{"kitty"}, UnitName: "mine.scope"}.Name()
	if err != nil || got != "mine.scope" {
		t.Errorf("Name with -u = %q, %v", got, err)
	}
	if _, err := (Options{Argv: []string{"kitty"}, UnitName: "mine.scope", Service: true}).Name(); err == nil {
		t.Error("a .scope name was accepted for a service")
	}
	if _, err := (Options{Argv: []string{"kitty"}, UnitName: strings.Repeat("x", 300) + ".scope"}).Name(); err == nil {
		t.Error("a unit name longer than 255 was accepted")
	}
}

func TestRunArgs(t *testing.T) {
	o := Options{
		Argv:        []string{"kitty", "-e", "htop"},
		Slice:       AppSlice,
		Description: "una terminal",
		WorkingDir:  "/tmp",
	}
	args, err := o.RunArgs()
	if err != nil {
		t.Fatalf("RunArgs: %v", err)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{
		"systemd-run --user", "--slice=" + AppSlice, "--scope",
		"--description=una terminal", "--working-directory=/tmp", "-- kitty -e htop",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("RunArgs gave %q, without %q", line, want)
		}
	}
	// Lo del escritorio va detrás de "--", para que sus opciones no las lea
	// systemd-run.
	if i := indexOf(args, "--"); i < 0 || args[i+1] != "kitty" {
		t.Errorf("the command does not come right after --: %q", args)
	}

	// Un servicio se lanza de otra manera, y sólo él puede callar la salida.
	args, err = Options{Argv: []string{"steam"}, Slice: AppSlice, Service: true, Silent: "both"}.RunArgs()
	if err != nil {
		t.Fatalf("RunArgs for a service: %v", err)
	}
	line = strings.Join(args, " ")
	for _, want := range []string{"--property=Type=exec", "--property=ExitType=cgroup",
		"--property=StandardOutput=null", "--property=StandardError=null"} {
		if !strings.Contains(line, want) {
			t.Errorf("RunArgs for a service gave %q, without %q", line, want)
		}
	}
	if strings.Contains(line, "--scope") {
		t.Errorf("RunArgs for a service asked for a scope: %q", line)
	}

	if _, err := (Options{Argv: []string{"kitty"}, Silent: "yes"}).RunArgs(); err == nil {
		t.Error("an invalid -S value was accepted")
	}
	if _, err := (Options{}).RunArgs(); err == nil {
		t.Error("RunArgs accepted an empty command")
	}
}

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}
