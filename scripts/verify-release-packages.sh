#!/bin/sh
set -eu

usage() {
  echo "Usage: $0 <version> <amd64|arm64> <release-directory> [expected-upstream-nginx]" >&2
  exit 2
}

if [ "$#" -lt 3 ] || [ "$#" -gt 4 ]; then
  usage
fi

version=$1
architecture=$2
release_directory=$3
expected_upstream=${4:-}

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

deb_archive="$release_directory/mirrorrelay_${version}_${deb_architecture}.deb"
rpm_archive="$release_directory/mirrorrelay-${version}.${rpm_architecture}.rpm"
tar_archive="$release_directory/mirrorrelay-${version}-linux-${architecture}.tar.gz"
manifest="$release_directory/SHA256SUMS-$architecture"

for required_file in "$deb_archive" "$rpm_archive" "$tar_archive" "$manifest"; do
  if [ ! -f "$required_file" ]; then
    echo "Missing release artifact: $required_file" >&2
    exit 1
  fi
done

(
  cd "$release_directory"
  sha256sum -c "$(basename "$manifest")"
)

temporary_directory=$(mktemp -d)
cleanup() {
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT INT TERM

deb_root="$temporary_directory/deb"
rpm_root="$temporary_directory/rpm"
tar_root="$temporary_directory/tar"
mkdir -p "$deb_root" "$rpm_root" "$tar_root"
dpkg-deb -x "$deb_archive" "$deb_root"
rpm2cpio "$rpm_archive" | (cd "$rpm_root" && cpio -idm --quiet)
tar -xzf "$tar_archive" -C "$tar_root"
tar_payload="$tar_root/mirrorrelay-$version"

check_payload() {
  payload_root=$1
  nginx_path=$2
  mirrorrelay_path=$3
  config_path=$4
  service_path=$5
  build_info_path=$6
  license_path=$7

  for required_path in "$nginx_path" "$mirrorrelay_path" "$config_path" "$service_path" "$build_info_path" "$license_path"; do
    if [ ! -f "$payload_root/$required_path" ]; then
      echo "Package content is missing: $required_path" >&2
      exit 1
    fi
  done
  [ "$(stat -c %a "$payload_root/$nginx_path")" = 755 ]
  [ "$(stat -c %a "$payload_root/$mirrorrelay_path")" = 755 ]
  file "$payload_root/$nginx_path" | grep -F "$elf_pattern" >/dev/null
  file "$payload_root/$mirrorrelay_path" | grep -F "$elf_pattern" >/dev/null
  grep -F "MirrorRelay Version: $version" "$payload_root/$build_info_path" >/dev/null
  grep -F "Target Architecture: $architecture" "$payload_root/$build_info_path" >/dev/null
  grep -F "with xx/Clang for linux/$architecture" "$payload_root/$build_info_path" >/dev/null
  printf '%s  %s\n' \
    '3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986' \
    "$payload_root/$license_path" | sha256sum -c - >/dev/null
  for metadata_label in \
    'Git Commit:' \
    'Build Timestamp:' \
    'Go Version:' \
    'MirrorRelay Build ID:' \
    'Managed Upstream Nginx Version:' \
    'Nginx Source SHA256:' \
    'Configure Arguments:' \
    'musl Version:' \
    'TLS Library Version:' \
    'PCRE2 Version:' \
    'Compression Library Version:' \
    'Build Method:' \
    'Managed Upstream Nginx SHA256:'; do
    grep -F "$metadata_label" "$payload_root/$build_info_path" >/dev/null
  done
  mirrorrelay_checksum=$(sha256sum "$payload_root/$mirrorrelay_path" | awk '{print $1}')
  upstream_checksum=$(sha256sum "$payload_root/$nginx_path" | awk '{print $1}')
  grep -F "MirrorRelay SHA256: $mirrorrelay_checksum" "$payload_root/$build_info_path" >/dev/null
  grep -F "Managed Upstream Nginx SHA256: $upstream_checksum" "$payload_root/$build_info_path" >/dev/null

  ldd "$payload_root/$nginx_path" > "$temporary_directory/ldd.txt" 2>&1 || true
  if grep -Eq '=>|ld-linux|ld-musl|libc\.so|libssl\.so|libcrypto\.so|libpcre[^ ]*\.so|libz\.so' "$temporary_directory/ldd.txt"; then
    cat "$temporary_directory/ldd.txt" >&2
    echo "Packaged Managed Upstream Nginx has an unexpected runtime dependency." >&2
    exit 1
  fi
  if readelf -l "$payload_root/$nginx_path" | grep -q INTERP; then
    echo "Packaged Managed Upstream Nginx has a dynamic interpreter." >&2
    exit 1
  fi
  if readelf -l "$payload_root/$mirrorrelay_path" | grep -q INTERP; then
    echo "Packaged MirrorRelay has a dynamic interpreter." >&2
    exit 1
  fi
  if [ -n "$expected_upstream" ]; then
    cmp "$expected_upstream" "$payload_root/$nginx_path"
  fi
}

check_payload "$deb_root" \
  usr/lib/mirrorrelay/nginx/nginx \
  usr/bin/mirrorrelay \
  etc/mirrorrelay/config.yaml \
  usr/lib/systemd/system/mirrorrelay.service \
  usr/share/doc/mirrorrelay/BUILD-INFO \
  usr/share/doc/mirrorrelay/LICENSE

check_payload "$rpm_root" \
  usr/lib/mirrorrelay/nginx/nginx \
  usr/bin/mirrorrelay \
  etc/mirrorrelay/config.yaml \
  usr/lib/systemd/system/mirrorrelay.service \
  usr/share/doc/mirrorrelay/BUILD-INFO \
  usr/share/licenses/mirrorrelay/LICENSE

check_payload "$tar_payload" \
  nginx/nginx \
  mirrorrelay \
  config/config.example.yaml \
  systemd/mirrorrelay.service \
  BUILD-INFO \
  LICENSE

previous_subject=mirror
previous_role=manager
previous_subject_upper=MIRROR
previous_role_upper=MANAGER
previous_short=mm
previous_product_name="${previous_subject}-${previous_role}"
previous_environment_prefix="${previous_subject_upper}_${previous_role_upper}"
previous_cookie_name="${previous_short}_session"
previous_metric_prefix="${previous_subject}_${previous_role}_"
previous_exit_key="stop_on_${previous_role}_exit"
previous_identity_pattern="${previous_product_name}|${previous_environment_prefix}|${previous_cookie_name}|${previous_metric_prefix}|${previous_exit_key}"
if grep -R -a -E "$previous_identity_pattern" \
  "$deb_root" "$rpm_root" "$tar_payload"; then
  echo "A release payload contains a previous product identifier." >&2
  exit 1
fi

for package_root in "$deb_root" "$rpm_root"; do
  for documentation in \
    configuration.md \
    configuration.zh-CN.md \
    verification.md \
    verification.zh-CN.md \
    web-ui.md \
    web-ui.zh-CN.md; do
    test -f "$package_root/usr/share/doc/mirrorrelay/$documentation"
  done
done
for documentation in \
  configuration.md \
  configuration.zh-CN.md \
  verification.md \
  verification.zh-CN.md \
  web-ui.md \
  web-ui.zh-CN.md; do
  test -f "$tar_payload/docs/$documentation"
done

(
  cd "$tar_payload"
  sha256sum -c SHA256SUMS
)

dpkg-deb -f "$deb_archive" Architecture | grep -Fx "$deb_architecture" >/dev/null
dpkg-deb -f "$deb_archive" Depends | grep -F 'passwd' >/dev/null
rpm -qp --qf '%{ARCH}\n' "$rpm_archive" | grep -Fx "$rpm_architecture" >/dev/null
dpkg_contents="$temporary_directory/deb-contents.txt"
dpkg-deb --contents "$deb_archive" > "$dpkg_contents"
grep -E '^-rwxr-xr-x root/root .* \./usr/bin/mirrorrelay$' "$dpkg_contents" >/dev/null
grep -E '^-rwxr-xr-x root/root .* \./usr/lib/mirrorrelay/nginx/nginx$' "$dpkg_contents" >/dev/null
grep -E '^-rw-r----- root/root .* \./etc/mirrorrelay/config.yaml$' "$dpkg_contents" >/dev/null
grep -E '^-rw-r--r-- root/root .* \./usr/lib/systemd/system/mirrorrelay.service$' "$dpkg_contents" >/dev/null
rpm -qpl "$rpm_archive" | grep -Fx '/usr/lib/mirrorrelay/nginx/nginx' >/dev/null
tar -tzf "$tar_archive" | grep -Fx "mirrorrelay-$version/nginx/nginx" >/dev/null

rpm_metadata="$temporary_directory/rpm-metadata.txt"
rpm -qp --qf '[%{FILENAMES}|%{FILEMODES:perms}|%{FILEUSERNAME}|%{FILEGROUPNAME}\n]' "$rpm_archive" > "$rpm_metadata"
grep -Fx '/usr/bin/mirrorrelay|-rwxr-xr-x|root|root' "$rpm_metadata" >/dev/null
grep -Fx '/usr/lib/mirrorrelay/nginx/nginx|-rwxr-xr-x|root|root' "$rpm_metadata" >/dev/null
grep -Fx '/etc/mirrorrelay/config.yaml|-rw-r-----|root|mirrorrelay' "$rpm_metadata" >/dev/null
grep -Fx '/usr/lib/systemd/system/mirrorrelay.service|-rw-r--r--|root|root' "$rpm_metadata" >/dev/null

for service_root in "$deb_root" "$rpm_root"; do
  service_file="$service_root/usr/lib/systemd/system/mirrorrelay.service"
  grep -Fx 'User=mirrorrelay' "$service_file" >/dev/null
  grep -Fx 'Group=mirrorrelay' "$service_file" >/dev/null
  grep -Fx 'RuntimeDirectory=mirrorrelay' "$service_file" >/dev/null
  grep -Fx 'RuntimeDirectoryMode=0750' "$service_file" >/dev/null
  grep -Fx 'RuntimeDirectoryPreserve=yes' "$service_file" >/dev/null
  grep -Fx 'UMask=0007' "$service_file" >/dev/null
  grep -Fx 'ExecStart=/usr/bin/mirrorrelay -config /etc/mirrorrelay/config.yaml' "$service_file" >/dev/null
  package_config="$service_root/etc/mirrorrelay/config.yaml"
  grep -Fx '  frontend_socket_mode: "0660"' "$package_config" >/dev/null
  grep -Fx '  upstream_socket_mode: "0600"' "$package_config" >/dev/null
  grep -Fx '  binary: /usr/lib/mirrorrelay/nginx/nginx' "$package_config" >/dev/null
  grep -Fx '  prefix: /var/lib/mirrorrelay/runtime/upstream-nginx' "$package_config" >/dev/null
  grep -Fx '  pid: /run/mirrorrelay/upstream-nginx.pid' "$package_config" >/dev/null
  grep -Fx '  path: /admin/' "$package_config" >/dev/null
done
grep -Fx '  ca_bundle: /etc/ssl/certs/ca-certificates.crt' "$deb_root/etc/mirrorrelay/config.yaml" >/dev/null
grep -Fx '  ca_bundle: /etc/pki/tls/certs/ca-bundle.crt' "$rpm_root/etc/mirrorrelay/config.yaml" >/dev/null
grep -Fx '  ca_bundle: /etc/ssl/certs/ca-certificates.crt' "$tar_payload/config/config.example.yaml" >/dev/null

if ! dpkg-deb --ctrl-tarfile "$deb_archive" | tar -tf - | grep -Fx './conffiles' >/dev/null; then
  echo "DEB conffiles metadata is missing." >&2
  exit 1
fi
dpkg-deb --ctrl-tarfile "$deb_archive" | tar -xOf - ./postinst | grep -F 'chown root:mirrorrelay /etc/mirrorrelay/config.yaml' >/dev/null
rpm -qp --configfiles "$rpm_archive" | grep -Fx '/etc/mirrorrelay/config.yaml' >/dev/null

deb_postinst="$temporary_directory/deb-postinst.txt"
deb_prerm="$temporary_directory/deb-prerm.txt"
deb_postrm="$temporary_directory/deb-postrm.txt"
rpm_scripts="$temporary_directory/rpm-scripts.txt"
dpkg-deb --ctrl-tarfile "$deb_archive" | tar -xOf - ./postinst > "$deb_postinst"
dpkg-deb --ctrl-tarfile "$deb_archive" | tar -xOf - ./prerm > "$deb_prerm"
dpkg-deb --ctrl-tarfile "$deb_archive" | tar -xOf - ./postrm > "$deb_postrm"
grep -F '/var/lib/mirrorrelay/.package-active' "$deb_prerm" >/dev/null
grep -F '/var/lib/mirrorrelay/.package-active' "$deb_postinst" >/dev/null
grep -F 'stop_managed_upstream_nginx' "$deb_prerm" >/dev/null
# shellcheck disable=SC2016
grep -F 'kill -QUIT "$managed_pid"' "$deb_prerm" >/dev/null
# shellcheck disable=SC2016
grep -F 'readlink "/proc/$managed_pid/exe"' "$deb_prerm" >/dev/null
grep -F ' = "purge" ]; then' "$deb_postrm" >/dev/null
rpm -qp --scripts "$rpm_archive" > "$rpm_scripts"
# shellcheck disable=SC2016
grep -F 'kill -QUIT "$managed_pid"' "$rpm_scripts" >/dev/null
# shellcheck disable=SC2016
grep -F 'readlink "/proc/$managed_pid/exe"' "$rpm_scripts" >/dev/null
if grep -Eq '/etc/nginx|systemctl[[:space:]]+(reload|restart|stop|start)[[:space:]]+nginx' "$deb_postinst" "$deb_prerm" "$deb_postrm" "$rpm_scripts"; then
  echo "A package script attempts to manage External Shared Nginx." >&2
  exit 1
fi

printf 'Verified DEB, RPM and tar.gz packages for linux/%s\n' "$architecture"
