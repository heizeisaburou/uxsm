#!/bin/sh
# Bring a VM image up to date and give it what the tests need, so that the
# build and test VMs started from it download little. test/vm.sh runs this as
# root inside the VM when it makes or updates a prepared image.
#
# What the build needs stays out: the test VM starts from this image too, and
# a build dependency there could hide a runtime dependency the package forgot
# to declare, which is what testing on a clean machine is for.
#
# LightDM is installed but masked, so that for systemd, which is where uxsm
# looks, it is neither enabled nor installed: test 05 checks a machine without
# a display manager, and test 08 unmasks it instead of installing it on every
# run. Without recommended packages: on Ubuntu they are most of a Unity
# desktop, with autostart entries that every earlier test would then start.

set -eu
. "$(dirname "$0")/mirror.sh"

if command -v apt-get >/dev/null 2>&1; then
  apt() {
    DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1 apt-get -y -qq \
      -o Dpkg::Options::=--force-confold "$@" >/dev/null
  }
  # Installing a display manager starts it, and upgrading a masked one may fail
  # to restart it: policy-rc.d keeps dpkg from starting anything, which a
  # machine about to be powered off does not need.
  printf '#!/bin/sh\nexit 101\n' >/usr/sbin/policy-rc.d
  chmod 755 /usr/sbin/policy-rc.d
  echo "== updating the system"
  # apt-get update exits with 0 when it cannot download an index, keeping the
  # old one; Error-Mode=any makes that an error, so mirror sees it and retries.
  mirror apt -o APT::Update::Error-Mode=any update
  mirror apt full-upgrade
  echo "== installing test dependencies"
  mirror apt install xvfb x11-utils bspwm
  mirror apt install --no-install-recommends lightdm
  rm -f /usr/sbin/policy-rc.d
elif command -v pacman >/dev/null 2>&1; then
  echo "== updating the system"
  # An old image may contain a keyring that no longer signs current packages.
  mirror pacman -Sy --noconfirm --needed archlinux-keyring >/dev/null
  mirror pacman -Su --noconfirm >/dev/null
  echo "== installing test dependencies"
  mirror pacman -S --noconfirm --needed xorg-server-xvfb xorg-xprop bspwm >/dev/null
  mirror pacman -S --noconfirm --needed lightdm >/dev/null
elif command -v dnf >/dev/null 2>&1; then
  echo "== updating the system"
  mirror dnf -y -q upgrade >/dev/null
  echo "== installing test dependencies"
  mirror dnf -y -q install xorg-x11-server-Xvfb xprop bspwm >/dev/null
  mirror dnf -y -q install --setopt=install_weak_deps=False lightdm >/dev/null
elif command -v zypper >/dev/null 2>&1; then
  zypper() { command zypper --non-interactive --quiet "$@" >/dev/null; }
  echo "== updating the system"
  mirror zypper refresh
  # Tumbleweed is updated with dup, not update: each snapshot may replace
  # packages, not only upgrade them.
  mirror zypper dup --no-recommends
  echo "== installing test dependencies"
  mirror zypper install --no-recommends xorg-x11-server-Xvfb xprop bspwm
  mirror zypper install --no-recommends lightdm
else
  echo "prepare.sh: no supported package manager" >&2
  exit 1
fi
# Debian writes display-manager.service itself rather than through the unit's
# Alias=, so disable alone may leave it.
systemctl disable --quiet lightdm.service 2>/dev/null || true
rm -f /etc/systemd/system/display-manager.service
systemctl mask --quiet lightdm.service
# Arch waits up to a minute and a half at every boot for NTP before starting
# sshd. The guest takes its clock from the host (kvmclock), so there is
# nothing to wait for; masking a unit a distribution lacks is harmless.
systemctl mask --quiet systemd-time-wait-sync.service
echo "== image ready"
