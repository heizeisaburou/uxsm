# The nixpkgs package

`package.nix` is the package as submitted to nixpkgs, as `pkgs/by-name/ux/uxsm/package.nix`. The flake at the repository root builds it from the local tree instead of the release tag.

## History

[NixOS/nixpkgs#574216](https://github.com/NixOS/nixpkgs/pull/574216) was approved by two reviewers and passed nixpkgs CI, and was then closed on 2026-10-16: [pkgs/README.md](https://github.com/NixOS/nixpkgs/blob/master/pkgs/README.md) asks for projects that are mature, likely to last, and used by other people, and uxsm was a month old. The reviewer suggested the [NUR](https://github.com/nix-community/NUR) meanwhile and invited a new PR once the project grows. An earlier PR, #567642, was closed for not following the PR template.

## Submitting it again

1. Copy `package.nix` to `pkgs/by-name/ux/uxsm/package.nix` in nixpkgs, without the comment at its top, and add the maintainer entry to `maintainers/maintainer-list.nix` in a commit of its own.
2. Check it there: `nix-build -A uxsm`, `nix-build ci -A fmt.check`, and `nixpkgs-vet`.
3. Keep the PR template whole, and follow the nixpkgs AI policy in `CONTRIBUTING.md`.

`test/publish.sh --hashes vX.Y.Z` writes the version and hash of each release here, so the file is always ready for the latest version.

## Installing it once it is in nixpkgs

After the package reaches your channel, starting with nixos-unstable, no flake is needed:

```nix
{ pkgs, ... }:
{
  environment.systemPackages = [ pkgs.uxsm ];
  systemd.packages = [ pkgs.uxsm ];
}
```

`systemd.packages` is what places the unit templates where each user's systemd finds them.
