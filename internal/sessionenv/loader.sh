#!/bin/sh
# Session environment loader. `uxsm aux prepare-env` runs it with the session's
# login environment and auxiliary __UXSM_*__ variables. It is a copy of uwsm's
# prepare-env.sh without plugins:
#
#  1. Load /etc/profile and the ~/.profile file.
#  2. Set default XDG directories and the session identity.
#  3. Load uxsm environment files from lowest to highest priority.
#  4. Write the __UXSM_MARK__ marker followed by the null-separated resulting
#     environment for uxsm to read.
#
# Anything written before the marker is a message and ends up in the journal.

# reverse LIST: reverse a colon-separated list, omitting empty elements.
reverse() {
	__reverse_out__=''
	IFS=':'
	for __item__ in $1; do
		if [ -n "${__item__}" ]; then
			__reverse_out__="${__item__}${__reverse_out__:+:}${__reverse_out__}"
		fi
	done
	IFS="${__UXSM_OIFS__}"
	printf '%s' "${__reverse_out__}"
	unset __reverse_out__ __item__
}

lowercase() {
	printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

# source_file FILE: load it if it exists, is readable, and has valid syntax.
source_file() {
	if [ -f "$1" ]; then
		if [ ! -r "$1" ]; then
			printf '%s\n' "Environment file $1 is not readable" >&2
			return
		fi
		if ! sh -n "$1"; then
			return
		fi
		printf '%s\n' "Sourcing environment file \"$1\"."
		. "$1"
	fi
}

# source_dir DIRECTORY: load each file in name order except backups and examples.
source_dir() {
	if [ -d "$1" ]; then
		for __env_file__ in "$1/"*; do
			case "${__env_file__}" in
			*.bak | *~ | *.disabled | *.example | *.sample | *.broken) continue ;;
			esac
			source_file "${__env_file__}"
		done
		unset __env_file__
	fi
}

__UXSM_OIFS__=$IFS

# 1. Shell profile.
printf '%s\n' "Loading shell profile."
[ -f /etc/profile ] && . /etc/profile
[ -f "${HOME}/.profile" ] && . "${HOME}/.profile"
export PATH

# 2. XDG directories and session identity.
export XDG_CONFIG_DIRS="${XDG_CONFIG_DIRS:-/etc/xdg}"
export XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-${HOME}/.config}"
export XDG_DATA_DIRS="${XDG_DATA_DIRS:-/usr/local/share:/usr/share}"
export XDG_DATA_HOME="${XDG_DATA_HOME:-${HOME}/.local/share}"
export XDG_CACHE_HOME="${XDG_CACHE_HOME:-${HOME}/.cache}"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
export XDG_STATE_HOME="${XDG_STATE_HOME:-${HOME}/.local/state}"

export XDG_CURRENT_DESKTOP="${__UXSM_XDG_CURRENT_DESKTOP__}"
export XDG_SESSION_DESKTOP="${__UXSM_XDG_SESSION_DESKTOP__}"
export XDG_MENU_PREFIX="${__UXSM_XDG_MENU_PREFIX__}"
export XDG_SESSION_TYPE="${__UXSM_XDG_SESSION_TYPE__}"
export XDG_BACKEND="x11"

# 3. Environment files: in each configuration and data directory, from lowest
# to highest priority, uxsm/env, then one uxsm/env-<desktop> for each lowercased
# XDG_CURRENT_DESKTOP name in order, followed by each file's .d directory.
__env_files__='uxsm/env'
IFS=':'
for __name__ in $(lowercase "${XDG_CURRENT_DESKTOP}"); do
	__env_files__="${__env_files__}:uxsm/env-${__name__}"
done
for __dir__ in $(reverse "${XDG_CONFIG_HOME}:${XDG_CONFIG_DIRS}:${XDG_DATA_DIRS}"); do
	for __env_file__ in ${__env_files__}; do
		IFS="${__UXSM_OIFS__}"
		source_file "${__dir__}/${__env_file__}"
		source_dir "${__dir__}/${__env_file__}.d"
		IFS=':'
	done
done
IFS="${__UXSM_OIFS__}"
unset __env_files__ __env_file__ __name__ __dir__

# 4. Marker and resulting environment.
printf '%s' "${__UXSM_MARK__}"
exec env -0
