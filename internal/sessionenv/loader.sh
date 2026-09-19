#!/bin/sh
# Cargador del entorno de la sesión. Lo ejecuta `uxsm aux prepare-env` con el
# entorno de login de la sesión y unas variables auxiliares __UXSM_*__, y es una
# copia del prepare-env.sh de uwsm sin plugins:
#
#  1. Carga /etc/profile y ~/.profile.
#  2. Pone los directorios XDG por defecto y la identidad de la sesión.
#  3. Carga los ficheros de entorno de uxsm de menos a más prioridad.
#  4. Escribe la marca __UXSM_MARK__ y detrás el entorno resultante, separado
#     por caracteres nulos, para que uxsm lo lea.
#
# Lo que escriba antes de la marca son mensajes, y acaban en el journal.

# reverse LISTA: la lista separada por ":" al revés, sin elementos vacíos.
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

# source_file FICHERO: lo carga si existe, se puede leer y su sintaxis es válida.
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

# source_dir DIRECTORIO: carga cada fichero del directorio por orden de nombre,
# salvo copias y ejemplos.
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

# 1. Perfil de la shell.
printf '%s\n' "Loading shell profile."
[ -f /etc/profile ] && . /etc/profile
[ -f "${HOME}/.profile" ] && . "${HOME}/.profile"
export PATH

# 2. Directorios XDG e identidad de la sesión.
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

# 3. Ficheros de entorno: en cada directorio de configuración y de datos, de
# menos a más prioridad, uxsm/env, un uxsm/env-<escritorio> por cada nombre de
# XDG_CURRENT_DESKTOP en minúsculas y en su orden, y detrás de cada fichero su
# directorio .d.
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

# 4. Marca y entorno resultante.
printf '%s' "${__UXSM_MARK__}"
exec env -0
