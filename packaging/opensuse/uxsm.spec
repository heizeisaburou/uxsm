#
# spec file for package uxsm
#
# Copyright (c) 2026 平生三郎 <heizeisaburou@gmail.com>
#
# All modifications and additions to the file contributed by third parties
# remain the property of their copyright owners, unless otherwise agreed
# upon. The license for this file, and modifications and additions to the
# file, is the same license as for the pristine package itself (unless the
# license for the pristine package is not an Open Source License, in which
# case the license is the MIT License). An "Open Source License" is a
# license that conforms to the Open Source Definition (Version 1.9)
# published by the Open Source Initiative.

# Please submit bugfixes or comments via https://github.com/heizeisaburou/uxsm/issues
#


Name:           uxsm
Version:        0.0.0
Release:        0
Summary:        X11 session manager for systemd --user
License:        Apache-2.0
Group:          System/X11/Utilities
URL:            https://github.com/heizeisaburou/uxsm
Source0:        %{url}/archive/refs/tags/v%{version}/%{name}-%{version}.tar.gz
# openSUSE's go packages provide golang(API); any of them >= 1.22 will do.
BuildRequires:  golang(API) >= 1.22
BuildRequires:  make
Requires:       systemd

%description
uxsm starts X11 graphical sessions under systemd --user the way uwsm does
for Wayland: it prepares the session environment, binds the session to
graphical-session.target and cleans up when the session ends.

%prep
%autosetup -n %{name}-%{version}

%build
%make_build build VERSION=%{version}

%check
make test

%install
%make_install PREFIX=%{_prefix} VERSION=%{version}

%files
%license LICENSE
%{_bindir}/uxsm
%{_prefix}/lib/systemd/user/uxsm-desktop@.service

# openSUSE keeps the changelog in uxsm.changes, not here.
%changelog
