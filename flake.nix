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
        default = (pkgs.callPackage ./packaging/nix/package.nix { }).overrideAttrs {
          version = "dev";
          src = self;
        };
      });
    };
}
