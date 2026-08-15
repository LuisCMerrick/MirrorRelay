#!/bin/sh
set -eu

case "${1:-}" in
  -s)
    printf 'Linux\n'
    ;;
  -r)
    printf 'cross\n'
    ;;
  -m)
    case "${REPOGATE_TARGETARCH:-}" in
      amd64) printf 'x86_64\n' ;;
      arm64) printf 'aarch64\n' ;;
      *)
        echo "Unsupported target architecture: ${REPOGATE_TARGETARCH:-unset}" >&2
        exit 1
        ;;
    esac
    ;;
  *)
    exec /bin/uname "$@"
    ;;
esac
