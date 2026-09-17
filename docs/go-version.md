# Versión de Go

## Por qué 1.22

Primero pusimos 1.24, la de Debian 13, y la bajamos a 1.22 por **Ubuntu 24.04 LTS**, que trae Go 1.22 y no compilaría nada con un mínimo más alto.

Versiones de Go de cada distribución, consultado el 2026-09-17:

| Distribución                              | Go        |
| ----------------------------------------- | --------- |
| Ubuntu 24.04 LTS                          | 1.22      |
| Debian 13 (trixie)                        | 1.24      |
| Ubuntu 26.04 LTS                          | 1.26      |
| Fedora 43                                 | 1.26      |
| Arch, openSUSE Tumbleweed, NixOS unstable | la última |

`.0` detrás no es capricho: `go 1.22` a secas es una versión del lenguaje, no una versión publicada de Go, y una Go 1.21 que intente descargar la toolchain que pide `go.mod` fallaría.

## go vet

`make build` pasa `go vet` antes de compilar. Con una Go más nueva que la de `go.mod`, `go build` acepta sin avisar funciones de la biblioteca estándar posteriores, y `go vet` las detecta.

## No disponible en 1.22

| Desde | Qué |
| --- | --- |
| 1.23 | iteradores: `range` sobre funciones, paquete `iter`, `slices.Collect`, `slices.Sorted`, `maps.Keys`, `maps.Values`; paquete `unique` |
| 1.24 | alias de tipos genéricos, `os.Root`, `strings.Lines`, `strings.SplitSeq`, `testing.B.Loop`, `omitzero` en `encoding/json` |
| 1.25 | `sync.WaitGroup.Go`, `testing/synctest` |

Lo que sí hay en 1.22 y apetece usar: `for i := range 10`, la variable del `for` nueva en cada vuelta, `min` y `max`, `slices` y `maps` sin iteradores, `math/rand/v2`, `log/slog`.
