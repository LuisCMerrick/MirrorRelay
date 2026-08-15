#!/bin/sh
set -eu

project_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
architecture=${1:-}

case "$architecture" in
  amd64|x86_64) architecture=amd64 ;;
  arm64|aarch64) architecture=arm64 ;;
  *)
    echo "Usage: $0 <amd64|arm64> [output-directory]" >&2
    exit 2
    ;;
esac

output_dir=${2:-"$project_root/dist/upstream-nginx-linux-$architecture"}
artifact_dir=$(mktemp -d)
build_network=${REPOGATE_DOCKER_BUILD_NETWORK:-default}

cleanup() {
  rm -rf -- "$artifact_dir"
}
trap cleanup EXIT INT TERM

docker buildx build \
  --platform "linux/$architecture" \
  --network "$build_network" \
  --progress plain \
  --file "$project_root/build/upstream-nginx-musl.Dockerfile" \
  --output "type=local,dest=$artifact_dir" \
  "$project_root"

mkdir -p "$output_dir"
install -m 0755 "$artifact_dir/nginx" "$output_dir/nginx"
install -m 0644 "$artifact_dir/BUILD-INFO.upstream-nginx" "$output_dir/BUILD-INFO.upstream-nginx"
(
  cd "$output_dir"
  sha256sum nginx > nginx.sha256
  sha256sum -c nginx.sha256
)

case "$architecture" in
  amd64) file "$output_dir/nginx" | grep -F 'x86-64' ;;
  arm64) file "$output_dir/nginx" | grep -F 'ARM aarch64' ;;
esac

ldd "$output_dir/nginx" > "$artifact_dir/ldd.txt" 2>&1 || true
if grep -Eq '=>|ld-linux|ld-musl|libc\.so|libssl\.so|libcrypto\.so|libpcre[^ ]*\.so|libz\.so' "$artifact_dir/ldd.txt"; then
  cat "$artifact_dir/ldd.txt" >&2
  echo "Managed Upstream Nginx has an unexpected runtime dependency." >&2
  exit 1
fi
if readelf -l "$output_dir/nginx" | grep -q 'INTERP'; then
  echo "Managed Upstream Nginx contains a dynamic interpreter." >&2
  exit 1
fi

printf 'Managed Upstream Nginx linux/%s written to %s\n' "$architecture" "$output_dir"
