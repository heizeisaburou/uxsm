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
  version = "0.1.0";

  src = fetchFromGitHub {
    owner = "heizeisaburou";
    repo = "uxsm";
    tag = "v${finalAttrs.version}";
    hash = "sha256-PHODySJV5Z2dRs6ImQBO6IIgMe4XKNgrv6Yj8q3Dm7I=";
  };

  # Standard library only: no vendor directory to hash.
  vendorHash = null;

  subPackages = [ "cmd/uxsm" ];

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${finalAttrs.version}"
  ];

  # What the Makefile installs besides the binary.
  postInstall = ''
    for f in data/systemd/user/*.in; do
      unit=$out/lib/systemd/user/$(basename "$f" .in)
      install -Dm644 "$f" "$unit"
      substituteInPlace "$unit" --replace-quiet @BINDIR@ $out/bin
    done
    for f in data/man/*.1.in; do
      page=$out/share/man/man1/$(basename "$f" .in)
      install -Dm644 "$f" "$page"
      substituteInPlace "$page" \
        --replace-quiet @BINDIR@ $out/bin \
        --replace-quiet @VERSION@ "${finalAttrs.version}"
    done
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
