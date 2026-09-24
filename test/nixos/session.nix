# La sesión de uxsm dentro de NixOS, arrancada por LightDM de verdad.
#
# Es la misma idea que test/integration/08-display-manager.sh, pero aquí la
# máquina se declara entera ―display manager, autologin, escritorio y la entrada
# de sesión― y la levanta el sistema de pruebas de nixpkgs. Sirve para dos cosas:
# comprobar que el paquete de Nix instala lo que hace falta, empezando por las
# unidades de systemd, y que uxsm funciona en una distribución donde nada está
# donde lo ponen las demás.
{ pkgs, uxsm }:

let
  # La entrada de sesión que verá LightDM. En NixOS no se genera con
  # `uxsm entry`: no hay /usr/local/share/xsessions, y las sesiones se declaran
  # con services.displayManager.sessionPackages.
  session = pkgs.writeTextFile {
    name = "bspwm-uxsm-session";
    # services.displayManager.sessionPackages exige que el paquete declare qué
    # sesiones trae; es lo que el display manager usa para nombrarlas.
    derivationArgs.passthru.providedSessions = [ "bspwm-uxsm" ];
    destination = "/share/xsessions/bspwm-uxsm.desktop";
    text = ''
      [Desktop Entry]
      Type=Application
      Name=bspwm (uxsm)
      Comment=bspwm as a systemd --user session
      Exec=${uxsm}/bin/uxsm start -D bspwm -- ${pkgs.bspwm}/bin/bspwm
      DesktopNames=bspwm
    '';
  };
in
pkgs.testers.runNixOSTest {
  name = "uxsm-session";

  nodes.machine =
    { ... }:
    {
      users.users.alice = {
        isNormalUser = true;
        uid = 1000;
      };

      services.xserver.enable = true;
      services.xserver.displayManager.lightdm = {
        enable = true;
        # Sin greeter: con autologin y espera cero, LightDM entra directo.
        greeter.enable = false;
      };
      services.displayManager = {
        autoLogin = {
          enable = true;
          user = "alice";
        };
        defaultSession = "bspwm-uxsm";
        sessionPackages = [ session ];
      };

      # systemd.packages es lo que pone las plantillas de unidades de uxsm donde
      # las encuentra el gestor de cada usuario.
      systemd.packages = [ uxsm ];
      environment.systemPackages = [
        uxsm
        pkgs.bspwm
      ];

      virtualisation.memorySize = 2048;
    };

  testScript = ''
    machine.wait_for_unit("display-manager.service")
    # Hasta que LightDM no entra no hay gestor de systemd de alice, y sin él no
    # se le puede preguntar nada.
    machine.wait_for_unit("user@1000.service")

    def user_unit(unit):
        return f"systemctl --user -M alice@ is-active {unit}"

    def dump(what):
        # Lo que hace falta cuando la sesión no arranca: qué dijo el sistema, qué
        # dijo la sesión al morir y qué unidades de usuario hay instaladas.
        print(f"=== {what} ===")
        print(machine.execute("journalctl -b -n 120 --no-pager")[1])
        print(machine.execute("cat /home/alice/.xsession-errors 2>/dev/null")[1])
        print(machine.execute("ls /etc/systemd/user/ 2>/dev/null")[1])
        print(machine.execute("tail -n 40 /var/log/lightdm/lightdm.log 2>/dev/null")[1])

    # La sesión entera, tal como la monta uxsm: el escritorio como servicio y,
    # detrás de la espera al gestor de ventanas, la sesión gráfica.
    try:
        machine.wait_until_succeeds(user_unit("uxsm-desktop@bspwm.service"), timeout=90)
    except Exception:
        dump("the uxsm session did not start")
        raise
    machine.wait_until_succeeds(user_unit("graphical-session.target"), timeout=60)

    # El proceso principal del servicio es el escritorio, sin intermediarios.
    pid = machine.succeed(
        "systemctl --user -M alice@ show -p MainPID --value uxsm-desktop@bspwm.service"
    ).strip()
    comm = machine.succeed(f"cat /proc/{pid}/comm").strip()
    assert comm == "bspwm", f"the main process of the desktop service is {comm}, not bspwm"

    # La identidad que uxsm calcula llega al escritorio.
    environ = machine.succeed(f"tr '\\0' '\\n' < /proc/{pid}/environ")
    assert "XDG_CURRENT_DESKTOP=bspwm" in environ, environ
    assert "XDG_SESSION_TYPE=x11" in environ, environ

    # Y el cierre desde el display manager se lleva la sesión por delante.
    machine.succeed("systemctl stop display-manager.service")
    machine.wait_until_fails(user_unit("uxsm-desktop@bspwm.service"))
    machine.wait_until_fails(user_unit("graphical-session.target"))
  '';
}
