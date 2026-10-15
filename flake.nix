{
  description = "X11 session manager for systemd --user, the X11 counterpart of uwsm";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      # The nixpkgs package, built from this checkout instead of the release tag.
      packages = forAllSystems (pkgs: {
        default = (pkgs.callPackage ./packaging/nix/package.nix { }).overrideAttrs (old: {
          version = "dev";
          src = self;
          # Its maintainer entry exists only in nixpkgs, once the package is
          # there; outside it, lib.maintainers has no such name.
          meta = old.meta // {
            maintainers = [ ];
          };
        });
      });

      # A NixOS machine that logs into a uxsm session through LightDM. Run it
      # with `make test-nixos`, or `nix build .#checks.<system>.session -L`.
      checks = forAllSystems (pkgs: {
        session = import ./test/nixos/session.nix {
          inherit pkgs;
          uxsm = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        };
      });
    };
}
