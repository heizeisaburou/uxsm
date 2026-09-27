#!/bin/sh
# Publish a version: the tag, the push, and the GitHub release.
#
#   test/publish.sh [--dry-run] [--yes] vX.Y.Z
#   test/publish.sh --hashes vX.Y.Z
#
# `make release` already builds and tests the distribution packages. This script
# adds the tag and binary tarball, verifies the complete artifact set, and then
# publishes it. It stops at the first failed check or command and follows this
# order:
#
#   1. Check the version, tree, branch, local and remote tags, GitHub releases,
#      and gh authentication.
#   2. Create the tag locally. The tag must precede the build because
#      test/version.sh reads `git describe`; only then do packages and tarballs
#      contain the final X.Y.Z version. Run `make release` now if
#      releases/latest contains another version.
#   3. Build and test the self-installing binary tarball, then recompute
#      SHA256SUMS for the complete artifact set.
#   4. Push the commit and tag, then create the GitHub release with all five
#      packages, both tarballs, and SHA256SUMS.
#
# Steps 1 through 3 change nothing outside this clone, and the local tag can be
# removed with `git tag -d`. Step 4 is public and cannot be undone, so it asks
# for confirmation unless --yes is used. With --dry-run, no state-changing
# command runs; the script prints what it would do after completing its checks.
#
# --hashes runs afterwards, once GitHub serves the tag archive. It prints that
# archive's checksum and writes it to the Arch and Nix recipes consumed by the
# AUR and nixpkgs. This cannot happen before the tag exists because GitHub
# creates and compresses the archive itself.

set -eu
cd "$(dirname "$0")/.."

dry=
yes=
hashes=
while [ $# -gt 1 ]; do
    case $1 in
    --dry-run) dry=1 ;;
    --yes) yes=1 ;;
    --hashes) hashes=1 ;;
    *) echo "publish.sh: unknown option $1" >&2; exit 2 ;;
    esac
    shift
done
tag=${1:-}

die() { echo "publish.sh: $*" >&2; exit 1; }
run() {
    if [ -n "$dry" ]; then
        echo "would run: $*"
        return 0
    fi
    "$@"
}

case $tag in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) die "the version must have the form vX.Y.Z; got ${tag:-nothing}" ;;
esac
version=${tag#v}
repo=heizeisaburou/uxsm

command -v gh >/dev/null 2>&1 || die "gh is required to publish a GitHub release but is not installed"
gh auth status >/dev/null 2>&1 || die "gh is not authenticated; run gh auth login"

if [ -n "$hashes" ]; then
    url="https://github.com/$repo/archive/refs/tags/$tag.tar.gz"
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    echo "== downloading $url"
    curl -fsSL "$url" -o "$tmp/src.tar.gz" || die "GitHub does not serve $tag yet"
    sum=$(sha256sum "$tmp/src.tar.gz" | cut -d' ' -f1)
    echo "SHA-256 of the tag archive: $sum"
    run sed -i "s/^pkgver=.*/pkgver=$version/;s/^sha256sums=.*/sha256sums=('$sum')/" packaging/arch/PKGBUILD
    if command -v nix >/dev/null 2>&1; then
        sri=$(nix hash convert --hash-algo sha256 "$sum" 2>/dev/null ||
            nix hash to-sri --type sha256 "$sum" 2>/dev/null || echo)
    fi
    if [ -n "${sri:-}" ]; then
        run sed -i "s|hash = lib.fakeHash;|hash = \"$sri\";|;s|hash = \".*\";|hash = \"$sri\";|;s/^  version = \".*\";/  version = \"$version\";/" packaging/nix/package.nix
    else
        echo "nix is not installed; put sha256-<base64> in packaging/nix/package.nix manually" >&2
    fi
    echo "Updated the Arch and Nix recipes; review and commit them for the AUR and nixpkgs."
    exit 0
fi

# 1. Checks.
[ -z "$(git status --porcelain)" ] || die "the working tree is not clean; commit or stash its changes first"
branch=$(git rev-parse --abbrev-ref HEAD)
case $branch in
main | master) ;;
*) die "releases can be published only from main or master, not $branch" ;;
esac
! git rev-parse -q --verify "refs/tags/$tag" >/dev/null || die "tag $tag already exists locally; remove it with git tag -d $tag if it is incorrect"
[ -z "$(git ls-remote --tags origin "refs/tags/$tag")" ] || die "tag $tag already exists on origin, so this version has been published"
! gh release view "$tag" --repo "$repo" >/dev/null 2>&1 || die "GitHub release $tag already exists"
[ -z "$(git log "@{upstream}..HEAD" --oneline 2>/dev/null || true)" ] ||
    echo "publish.sh: note: the current commit is not on its upstream branch; it will be pushed with the tag" >&2

echo "== uxsm $version from $(git rev-parse --short HEAD) on $branch"

# 2. The tag, and the release build with the tag in place: test/version.sh reads
# the tag, so the packages only carry X.Y.Z once it exists.
run git tag -a "$tag" -m "uxsm $version"
if [ -n "$dry" ]; then
    echo "would run: make release, unless releases/latest is already uxsm $version"
else
    if [ "$(cat releases/latest/version 2>/dev/null)" = "$version" ]; then
        echo "== releases/latest already contains tested packages for uxsm $version"
    else
        echo "== releases/latest does not contain uxsm $version; building and testing every distribution"
        make release || die "make release failed; the tag remains local and can be removed with git tag -d $tag"
    fi
fi

# The binary tarball goes next to the packages, and into the checksums with them.
run make bindist VERSION="$version" DISTDIR=releases/latest
if [ -z "$dry" ]; then
    (cd releases/latest &&
        find . -type f ! -name version ! -name SHA256SUMS -printf '%P\n' |
        LC_ALL=C sort | xargs sha256sum >SHA256SUMS)
    # What is about to be published, installed from the tarball into a temporary
    # root: if the tarball does not install, nobody should be downloading it.
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    tar xzf "releases/latest/uxsm-$version-linux-$(uname -m).tar.gz" -C "$tmp"
    (cd "$tmp"/uxsm-* && ./install.sh --destdir "$tmp/root" --prefix /usr >/dev/null)
    [ -x "$tmp/root/usr/bin/uxsm" ] || die "the binary tarball does not install its binary"
    [ -f "$tmp/root/usr/share/man/man1/uxsm.1" ] || die "the binary tarball does not install the manual page"
    got=$("$tmp/root/usr/bin/uxsm" version)
    [ "$got" = "$version" ] || die "the binary in the tarball says it is $got, not $version"
    echo "== binary tarball installs successfully and reports uxsm $version"
fi

if [ ! -d releases/latest ]; then
    [ -n "$dry" ] || die "releases/latest is not there after make release"
    echo "== would publish what make release leaves in releases/latest"
    artifacts=
else
    # Every distribution has to have left its package: a release with four is not
    # a release.
    for d in ubuntu debian arch fedora opensuse; do
        [ -n "$(find "releases/latest/$d" -type f 2>/dev/null)" ] ||
            die "releases/latest has no package for $d"
    done
    artifacts=$(find releases/latest -type f ! -name version | LC_ALL=C sort)
    echo "== would publish:"
    echo "$artifacts" | sed 's|^|  |'
fi

# 4. Public, and it cannot be undone.
if [ -z "$dry" ] && [ -z "$yes" ]; then
    printf 'Push %s and publish uxsm %s on GitHub? [y/N] ' "$tag" "$version"
    read -r answer
    case $answer in
    y | Y | yes) ;;
    *) die "nothing was pushed; the tag remains local and can be removed with git tag -d $tag" ;;
    esac
fi

run git push origin "$branch"
run git push origin "$tag"
# shellcheck disable=SC2086
run gh release create "$tag" --repo "$repo" --title "uxsm $version" \
    --notes "Packages for Ubuntu, Debian, Arch, Fedora, and openSUSE; the source tarball; and a self-installing binary tarball for machines without an uxsm package. SHA-256 checksums are provided in SHA256SUMS" \
    $artifacts

echo "== published https://github.com/$repo/releases/tag/$tag"
echo "Next, once GitHub serves the tag archive, run test/publish.sh --hashes $tag"
