# Package for nixpkgs (pkgs/by-name/ux/uxsm/package.nix) and for the flake at
# the repository root, which builds it from the local tree instead of the tag.
#
# Nix does not call the Makefile: buildGoModule builds and installs the binary
# itself, so any other installed file has to be added in postInstall too.
{
  lib,
  buildGoModule,
  fetchFromGitHub,
}:

buildGoModule (finalAttrs: {
  pname = "uxsm";
  version = "0.0.0";

  src = fetchFromGitHub {
    owner = "heizeisaburou";
    repo = "uxsm";
    tag = "v${finalAttrs.version}";
    hash = lib.fakeHash;
  };

  # Standard library only: no vendor directory to hash.
  vendorHash = null;

  subPackages = [ "cmd/uxsm" ];

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${finalAttrs.version}"
  ];

  # Lo que el Makefile instala además del binario.
  postInstall = ''
    install -Dm644 data/systemd/user/uxsm-desktop@.service.in \
      $out/lib/systemd/user/uxsm-desktop@.service
    substituteInPlace $out/lib/systemd/user/uxsm-desktop@.service \
      --replace-fail @BINDIR@ $out/bin
  '';

  meta = {
    description = "X11 session manager for systemd --user, the X11 counterpart of uwsm";
    homepage = "https://github.com/heizeisaburou/uxsm";
    license = lib.licenses.asl20;
    # nixpkgs needs an entry in maintainers/maintainer-list.nix first.
    maintainers = [ ];
    mainProgram = "uxsm";
    platforms = lib.platforms.linux;
  };
})
