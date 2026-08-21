#!/usr/bin/env bash
set -euo pipefail
umask 022

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <version> <output-directory>" >&2
  exit 2
fi

version=${1#v}
output_directory=$2
project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source_date_epoch=${SOURCE_DATE_EPOCH:-}

if [[ ! ${version} =~ ^[0-9][0-9A-Za-z.+~-]{0,63}$ ]]; then
  echo "invalid source package version: ${version}" >&2
  exit 1
fi
if [[ -z ${source_date_epoch} ]]; then
  source_date_epoch=$(git -C "${project_root}" show -s --format=%ct HEAD)
fi
if [[ ! ${source_date_epoch} =~ ^[0-9]+$ ]]; then
  echo "SOURCE_DATE_EPOCH must be an integer" >&2
  exit 1
fi

archive_root="mirrorrelay-${version}-source"
archive_name="mirrorrelay-${version}-source-with-vendor.tar.gz"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "${temporary_directory}"' EXIT

mkdir -p "${temporary_directory}/${archive_root}" "${output_directory}"
git -C "${project_root}" archive --format=tar HEAD | tar -xf - -C "${temporary_directory}/${archive_root}"
(
  cd "${temporary_directory}/${archive_root}"
  go mod vendor
  test -f vendor/modules.txt
)

tar \
  --sort=name \
  --mtime="@${source_date_epoch}" \
  --owner=0 \
  --group=0 \
  --numeric-owner \
  -C "${temporary_directory}" \
  -cf - "${archive_root}" | gzip -n > "${output_directory}/${archive_name}"

echo "${output_directory}/${archive_name}"
