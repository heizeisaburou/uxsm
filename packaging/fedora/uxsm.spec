# The Makefile builds with -trimpath, which leaves no source paths in the DWARF
# data, so find-debuginfo has nothing to put in -debugsource and fails on the
# empty package. -debuginfo, with the symbols, is still built. rpm checks
# whether the macro is defined, not its value, so it has to be undefined.
%undefine _debugsource_packages

Name:           uxsm
Version:        0.0.0
Release:        1%{?dist}
Summary:        X11 session manager for systemd --user

License:        Apache-2.0
URL:            https://github.com/heizeisaburou/uxsm
Source0:        %{url}/archive/refs/tags/v%{version}/%{name}-%{version}.tar.gz

# gcc links the binary: -linkmode=external, for Fedora's hardening flags.
BuildRequires:  gcc
BuildRequires:  golang >= 1.22
BuildRequires:  make
Requires:       systemd

%description
uxsm starts X11 graphical sessions under systemd --user the way uwsm does
for Wayland: it prepares the session environment, binds the session to
graphical-session.target and cleans up when the session ends.

%prep
%autosetup -n %{name}-%{version}

%build
# Fedora's debuginfo extraction needs a GNU build ID in the binary; gobuildid
# derives it from Go's own build ID, so the build stays reproducible.
%make_build build VERSION=%{version} GO_LDFLAGS="-linkmode=external -B gobuildid"

%check
make test

%install
%make_install PREFIX=%{_prefix} VERSION=%{version}

%files
%license LICENSE
%{_bindir}/uxsm
%{_prefix}/lib/systemd/user/uxsm-*
%{_prefix}/lib/systemd/user/*-uxsm.slice

%changelog
* Thu Sep 17 2026 平生三郎 <heizeisaburou@gmail.com> - 0.0.0-1
- Initial packaging skeleton.
