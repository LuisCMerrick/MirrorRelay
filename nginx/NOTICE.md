# Bundled Managed Upstream Nginx notice

Formal release packages contain an architecture-specific Linux binary built
against musl and statically linked with pinned upstream sources:

- Nginx 1.30.2 — BSD-2-Clause license: <https://nginx.org/LICENSE>
- OpenSSL 3.5.7 — Apache-2.0 license: <https://www.openssl.org/source/license.html>
- PCRE2 10.47 — BSD-3-Clause license: <https://github.com/PCRE2Project/pcre2/blob/pcre2-10.47/LICENCE.md>
- zlib 1.3.2 — zlib license: <https://zlib.net/zlib_license.html>

The exact source URLs and SHA-256 values are pinned in
`build/upstream-nginx-musl.Dockerfile`. Build either supported artifact with:

```sh
./scripts/build-upstream-nginx-musl.sh amd64
./scripts/build-upstream-nginx-musl.sh arm64
```

The build definitions cover `linux/amd64` and `linux/arm64`. Hosted release jobs
use a pinned xx/Clang musl cross toolchain and emit both architectures. Arm64
compilation runs natively on the build platform; QEMU is limited to configure
probes and executable tests. The checked-in `sbin/nginx` file is an amd64
development and CI fixture; the release pipeline always rebuilds and tests the
version-bound binary for every enabled architecture.
