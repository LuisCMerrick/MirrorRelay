#!/bin/sh
set -eu

find internal/web/dist -type f -name '*.js' -print0 |
  sort -z |
  xargs -0 -n 1 node --check

node scripts/verify-web-locales.mjs
