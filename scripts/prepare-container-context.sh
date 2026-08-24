#!/bin/sh
set -eu

usage() {
  echo "Usage: $0 <version> <release-directory> <empty-output-directory>" >&2
  exit 2
}

if [ "$#" -ne 3 ]; then
  usage
fi

version=${1#v}
release_directory=$2
output_directory=$3
project_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

case "$version" in
  [0-9]*) ;;
  *)
    echo "Container image versions must start with a digit: $version" >&2
    exit 2
    ;;
esac
case "$version" in
  *[!0-9A-Za-z.+~-]*)
    echo "Invalid container image version: $version" >&2
    exit 2
    ;;
esac

if [ ! -d "$release_directory" ]; then
  echo "Release directory is unavailable: $release_directory" >&2
  exit 1
fi
if [ -e "$output_directory" ] && [ ! -d "$output_directory" ]; then
  echo "Container context path is not a directory: $output_directory" >&2
  exit 1
fi
if [ -d "$output_directory" ] && find "$output_directory" -mindepth 1 -print -quit | grep -q .; then
  echo "Container context directory must be empty: $output_directory" >&2
  exit 1
fi
if [ ! -f "$project_root/configs/config.docker.yaml" ]; then
  echo "Docker configuration template is unavailable." >&2
  exit 1
fi

temporary_directory=$(mktemp -d)
cleanup() {
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT INT TERM

mkdir -p "$output_directory"
release_directory=$(CDPATH='' cd -- "$release_directory" && pwd)
output_directory=$(CDPATH='' cd -- "$output_directory" && pwd)

for architecture in amd64 arm64; do
  archive="$release_directory/mirrorrelay-$version-linux-$architecture.tar.gz"
  archive_root="mirrorrelay-$version"
  extraction_root="$temporary_directory/$architecture"
  payload_root="$extraction_root/$archive_root"
  destination="$output_directory/linux-$architecture"

  if [ ! -f "$archive" ]; then
    echo "Release archive is unavailable: $archive" >&2
    exit 1
  fi
  mkdir -p "$extraction_root"
  tar -xzf "$archive" -C "$extraction_root"
  for required_file in \
    "$payload_root/mirrorrelay" \
    "$payload_root/nginx/nginx" \
    "$payload_root/BUILD-INFO" \
    "$payload_root/SHA256SUMS" \
    "$payload_root/LICENSE" \
    "$payload_root/LICENSES/managed-upstream-nginx.md"; do
    if [ ! -f "$required_file" ]; then
      echo "Release archive content is unavailable: $required_file" >&2
      exit 1
    fi
  done
  (
    cd "$payload_root"
    sha256sum -c SHA256SUMS
  )
  grep -Fx "MirrorRelay Version: $version" "$payload_root/BUILD-INFO" >/dev/null
  grep -Fx "Target Architecture: $architecture" "$payload_root/BUILD-INFO" >/dev/null
  test -x "$payload_root/mirrorrelay"
  test -x "$payload_root/nginx/nginx"

  install -d "$destination"
  install -m 0755 "$payload_root/mirrorrelay" "$destination/mirrorrelay"
  install -m 0755 "$payload_root/nginx/nginx" "$destination/nginx"
  install -m 0644 "$payload_root/BUILD-INFO" "$destination/BUILD-INFO"
  install -m 0644 "$payload_root/LICENSE" "$destination/LICENSE"
  install -m 0644 \
    "$payload_root/LICENSES/managed-upstream-nginx.md" \
    "$destination/managed-upstream-nginx.md"
done

install -d \
  "$output_directory/rootfs/etc/mirrorrelay" \
  "$output_directory/rootfs/usr/lib/mirrorrelay/nginx" \
  "$output_directory/rootfs/usr/share/doc/mirrorrelay" \
  "$output_directory/rootfs/usr/share/licenses/mirrorrelay" \
  "$output_directory/rootfs/var/lib/mirrorrelay/runtime" \
  "$output_directory/rootfs/var/lib/mirrorrelay/integration/external-nginx" \
  "$output_directory/rootfs/var/cache/mirrorrelay" \
  "$output_directory/rootfs/var/log/mirrorrelay/upstream-nginx" \
  "$output_directory/rootfs/run/mirrorrelay"
install -m 0644 "$project_root/configs/config.docker.yaml" "$output_directory/config.yaml"
printf '%s\n' "$output_directory"
