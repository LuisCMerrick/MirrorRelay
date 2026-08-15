#!/bin/sh
set -eu

usage() {
  echo "Usage: $0 <version> <amd64|arm64> <repogate-binary> <upstream-nginx-directory> [output-directory]" >&2
  exit 2
}

if [ "$#" -lt 4 ] || [ "$#" -gt 5 ]; then
  usage
fi

version=$1
architecture=$2
repogate_binary=$3
upstream_directory=$4

project_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
output_directory=${5:-"$project_root/dist/release"}

case "$version" in
  [0-9]*) ;;
  *)
    echo "Release versions must start with a digit: $version" >&2
    exit 2
    ;;
esac
case "$version" in
  *[!0-9A-Za-z.+~-]*)
    echo "Invalid release version: $version" >&2
    exit 2
    ;;
esac

case "$architecture" in
  amd64)
    deb_architecture=amd64
    rpm_architecture=x86_64
    elf_pattern='x86-64'
    ;;
  arm64)
    deb_architecture=arm64
    rpm_architecture=aarch64
    elf_pattern='ARM aarch64'
    ;;
  *) usage ;;
esac

for command_name in dpkg-deb file readelf rpmbuild sed sha256sum tar; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Required packaging command is unavailable: $command_name" >&2
    exit 1
  fi
done

upstream_binary="$upstream_directory/nginx"
upstream_build_info="$upstream_directory/BUILD-INFO.upstream-nginx"
for required_file in \
  "$repogate_binary" \
  "$upstream_binary" \
  "$upstream_build_info" \
  "$project_root/configs/config.example.yaml" \
  "$project_root/deploy/repogate.service" \
  "$project_root/LICENSE" \
  "$project_root/README.md" \
  "$project_root/README.zh-CN.md" \
  "$project_root/docs/installation.md" \
  "$project_root/docs/installation.zh-CN.md" \
  "$project_root/docs/configuration.md" \
  "$project_root/docs/configuration.zh-CN.md" \
  "$project_root/docs/verification.md" \
  "$project_root/docs/verification.zh-CN.md" \
  "$project_root/docs/web-ui.md" \
  "$project_root/docs/web-ui.zh-CN.md" \
  "$project_root/nginx/NOTICE.md"; do
  if [ ! -f "$required_file" ]; then
    echo "Required release input is unavailable: $required_file" >&2
    exit 1
  fi
done

file "$repogate_binary" | grep -F "$elf_pattern" >/dev/null
file "$upstream_binary" | grep -F "$elf_pattern" >/dev/null

temporary_directory=$(mktemp -d)
cleanup() {
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT INT TERM

mkdir -p "$output_directory"
output_directory=$(CDPATH='' cd -- "$output_directory" && pwd)

source_date_epoch=${SOURCE_DATE_EPOCH:-}
if [ -z "$source_date_epoch" ]; then
  source_date_epoch=$(git -C "$project_root" show -s --format=%ct HEAD 2>/dev/null || date +%s)
fi
case "$source_date_epoch" in
  ""|*[!0-9]*)
    echo "SOURCE_DATE_EPOCH must be an integer." >&2
    exit 2
    ;;
esac

git_commit=${GIT_COMMIT:-$(git -C "$project_root" rev-parse HEAD 2>/dev/null || printf unknown)}
build_timestamp=${BUILD_TIMESTAMP:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
go_version=${GO_VERSION:-$(go version 2>/dev/null | awk '{print $3}' || printf unknown)}
git_commit_short=$(printf '%s' "$git_commit" | cut -c 1-12)
repogate_build_id=${REPOGATE_BUILD_ID:-"repogate-${version}-linux-${architecture}-${git_commit_short}"}
repogate_checksum=$(sha256sum "$repogate_binary" | awk '{print $1}')
upstream_checksum=$(sha256sum "$upstream_binary" | awk '{print $1}')

build_info="$temporary_directory/BUILD-INFO"
{
  printf 'RepoGate Version: %s\n' "$version"
  printf 'Git Commit: %s\n' "$git_commit"
  printf 'Build Timestamp: %s\n' "$build_timestamp"
  printf 'Go Version: %s\n' "$go_version"
  printf 'Target OS: linux\n'
  printf 'Target Architecture: %s\n' "$architecture"
  printf 'RepoGate Build ID: %s\n' "$repogate_build_id"
  printf 'RepoGate SHA256: %s\n' "$repogate_checksum"
  cat "$upstream_build_info"
} > "$build_info"

tar_root_name="repogate-$version"
tar_root="$temporary_directory/$tar_root_name"
install -d \
  "$tar_root/nginx" \
  "$tar_root/config" \
  "$tar_root/systemd" \
  "$tar_root/LICENSES" \
  "$tar_root/docs"
install -m 0755 "$repogate_binary" "$tar_root/repogate"
install -m 0755 "$upstream_binary" "$tar_root/nginx/nginx"
install -m 0644 "$project_root/configs/config.example.yaml" "$tar_root/config/config.example.yaml"
install -m 0644 "$project_root/deploy/repogate.service" "$tar_root/systemd/repogate.service"
install -m 0644 "$build_info" "$tar_root/BUILD-INFO"
install -m 0644 "$project_root/LICENSE" "$tar_root/LICENSE"
install -m 0644 "$project_root/nginx/NOTICE.md" "$tar_root/LICENSES/managed-upstream-nginx.md"
install -m 0644 "$project_root/README.md" "$tar_root/README.md"
install -m 0644 "$project_root/README.zh-CN.md" "$tar_root/README.zh-CN.md"
install -m 0644 "$project_root/docs/installation.md" "$tar_root/INSTALL.md"
install -m 0644 "$project_root/docs/installation.zh-CN.md" "$tar_root/INSTALL.zh-CN.md"
install -m 0644 "$project_root/docs/configuration.md" "$tar_root/docs/configuration.md"
install -m 0644 "$project_root/docs/configuration.zh-CN.md" "$tar_root/docs/configuration.zh-CN.md"
install -m 0644 "$project_root/docs/verification.md" "$tar_root/docs/verification.md"
install -m 0644 "$project_root/docs/verification.zh-CN.md" "$tar_root/docs/verification.zh-CN.md"
install -m 0644 "$project_root/docs/web-ui.md" "$tar_root/docs/web-ui.md"
install -m 0644 "$project_root/docs/web-ui.zh-CN.md" "$tar_root/docs/web-ui.zh-CN.md"
tar_manifest="$temporary_directory/tar-SHA256SUMS"
(
  cd "$tar_root"
  find . -type f -print | LC_ALL=C sort | sed 's#^./##' | xargs sha256sum > "$tar_manifest"
)
install -m 0644 "$tar_manifest" "$tar_root/SHA256SUMS"
(cd "$tar_root" && sha256sum -c SHA256SUMS)
tar_archive="$output_directory/repogate-$version-linux-$architecture.tar.gz"
tar \
  --sort=name \
  --mtime="@$source_date_epoch" \
  --owner=0 \
  --group=0 \
  --numeric-owner \
  -C "$temporary_directory" \
  -czf "$tar_archive" \
  "$tar_root_name"

deb_root="$temporary_directory/deb-root"
install -d \
  "$deb_root/DEBIAN" \
  "$deb_root/usr/bin" \
  "$deb_root/usr/lib/repogate/nginx" \
  "$deb_root/etc/repogate" \
  "$deb_root/usr/lib/systemd/system" \
  "$deb_root/usr/share/doc/repogate/LICENSES"
install -m 0755 "$repogate_binary" "$deb_root/usr/bin/repogate"
install -m 0755 "$upstream_binary" "$deb_root/usr/lib/repogate/nginx/nginx"
install -m 0640 "$project_root/configs/config.example.yaml" "$deb_root/etc/repogate/config.yaml"
install -m 0644 "$project_root/deploy/repogate.service" "$deb_root/usr/lib/systemd/system/repogate.service"
install -m 0644 "$build_info" "$deb_root/usr/share/doc/repogate/BUILD-INFO"
install -m 0644 "$project_root/LICENSE" "$deb_root/usr/share/doc/repogate/LICENSE"
install -m 0644 "$project_root/nginx/NOTICE.md" "$deb_root/usr/share/doc/repogate/LICENSES/managed-upstream-nginx.md"
install -m 0644 "$project_root/README.md" "$deb_root/usr/share/doc/repogate/README.md"
install -m 0644 "$project_root/README.zh-CN.md" "$deb_root/usr/share/doc/repogate/README.zh-CN.md"
install -m 0644 "$project_root/docs/installation.md" "$deb_root/usr/share/doc/repogate/INSTALL.md"
install -m 0644 "$project_root/docs/installation.zh-CN.md" "$deb_root/usr/share/doc/repogate/INSTALL.zh-CN.md"
install -m 0644 "$project_root/docs/configuration.md" "$deb_root/usr/share/doc/repogate/configuration.md"
install -m 0644 "$project_root/docs/configuration.zh-CN.md" "$deb_root/usr/share/doc/repogate/configuration.zh-CN.md"
install -m 0644 "$project_root/docs/verification.md" "$deb_root/usr/share/doc/repogate/verification.md"
install -m 0644 "$project_root/docs/verification.zh-CN.md" "$deb_root/usr/share/doc/repogate/verification.zh-CN.md"
install -m 0644 "$project_root/docs/web-ui.md" "$deb_root/usr/share/doc/repogate/web-ui.md"
install -m 0644 "$project_root/docs/web-ui.zh-CN.md" "$deb_root/usr/share/doc/repogate/web-ui.zh-CN.md"
for maintainer_script in postinst prerm postrm; do
  install -m 0755 "$project_root/packaging/debian/$maintainer_script" "$deb_root/DEBIAN/$maintainer_script"
done
install -m 0644 "$project_root/packaging/debian/conffiles" "$deb_root/DEBIAN/conffiles"
installed_size=$(du -sk "$deb_root/usr" "$deb_root/etc" | awk '{total += $1} END {print total}')
{
  printf 'Package: repogate\n'
  printf 'Version: %s\n' "$version"
  printf 'Architecture: %s\n' "$deb_architecture"
  printf 'Maintainer: RepoGate Release Pipeline <noreply@github.com>\n'
  printf 'Installed-Size: %s\n' "$installed_size"
  printf 'Depends: ca-certificates, passwd, systemd | systemd-sysv\n'
  printf 'Section: net\n'
  printf 'Priority: optional\n'
  printf 'Homepage: https://github.com/LuisCMerrick/RepoGate\n'
  printf 'Description: pull-through repository and registry gateway\n'
  printf ' Includes a version-bound static Managed Upstream Nginx data plane.\n'
} > "$deb_root/DEBIAN/control"
deb_archive="$output_directory/repogate_${version}_${deb_architecture}.deb"
dpkg-deb --build --root-owner-group "$deb_root" "$deb_archive" >/dev/null

rpm_top="$temporary_directory/rpmbuild"
install -d "$rpm_top/BUILD" "$rpm_top/BUILDROOT" "$rpm_top/RPMS" "$rpm_top/SOURCES" "$rpm_top/SPECS" "$rpm_top/SRPMS"
install -m 0755 "$repogate_binary" "$rpm_top/SOURCES/repogate"
install -m 0755 "$upstream_binary" "$rpm_top/SOURCES/nginx"
sed 's#/etc/ssl/certs/ca-certificates.crt#/etc/pki/tls/certs/ca-bundle.crt#' \
  "$project_root/configs/config.example.yaml" > "$rpm_top/SOURCES/config.yaml"
chmod 0644 "$rpm_top/SOURCES/config.yaml"
install -m 0644 "$project_root/deploy/repogate.service" "$rpm_top/SOURCES/repogate.service"
install -m 0644 "$build_info" "$rpm_top/SOURCES/BUILD-INFO"
install -m 0644 "$project_root/nginx/NOTICE.md" "$rpm_top/SOURCES/managed-upstream-nginx.md"
install -m 0644 "$project_root/LICENSE" "$rpm_top/SOURCES/LICENSE"
install -m 0644 "$project_root/README.md" "$rpm_top/SOURCES/README.md"
install -m 0644 "$project_root/README.zh-CN.md" "$rpm_top/SOURCES/README.zh-CN.md"
install -m 0644 "$project_root/docs/installation.md" "$rpm_top/SOURCES/INSTALL.md"
install -m 0644 "$project_root/docs/installation.zh-CN.md" "$rpm_top/SOURCES/INSTALL.zh-CN.md"
install -m 0644 "$project_root/docs/web-ui.md" "$rpm_top/SOURCES/web-ui.md"
install -m 0644 "$project_root/docs/web-ui.zh-CN.md" "$rpm_top/SOURCES/web-ui.zh-CN.md"
install -m 0644 "$project_root/docs/configuration.md" "$rpm_top/SOURCES/configuration.md"
install -m 0644 "$project_root/docs/configuration.zh-CN.md" "$rpm_top/SOURCES/configuration.zh-CN.md"
install -m 0644 "$project_root/docs/verification.md" "$rpm_top/SOURCES/verification.md"
install -m 0644 "$project_root/docs/verification.zh-CN.md" "$rpm_top/SOURCES/verification.zh-CN.md"
install -m 0644 "$project_root/packaging/rpm/repogate.spec" "$rpm_top/SPECS/repogate.spec"
rpm_version=$(printf '%s' "$version" | tr -- '-+~' '...')
rpmbuild -bb \
  --define "_topdir $rpm_top" \
  --define "package_version $rpm_version" \
  --target "$rpm_architecture" \
  "$rpm_top/SPECS/repogate.spec" >/dev/null
rpm_built=$(find "$rpm_top/RPMS" -type f -name '*.rpm' -print | head -n 1)
if [ -z "$rpm_built" ]; then
  echo "rpmbuild did not create an RPM package." >&2
  exit 1
fi
rpm_archive="$output_directory/repogate-${version}.${rpm_architecture}.rpm"
install -m 0644 "$rpm_built" "$rpm_archive"

architecture_manifest="$output_directory/SHA256SUMS-$architecture"
(
  cd "$output_directory"
  sha256sum \
    "$(basename "$deb_archive")" \
    "$(basename "$rpm_archive")" \
    "$(basename "$tar_archive")" > "$(basename "$architecture_manifest")"
)
{
  printf '# Managed Upstream Nginx linux/%s\n' "$architecture"
  printf '%s  /usr/lib/repogate/nginx/nginx\n' "$upstream_checksum"
} > "$output_directory/Managed-Upstream-Nginx-SHA256-$architecture"

printf 'Release packages for linux/%s written to %s\n' "$architecture" "$output_directory"
