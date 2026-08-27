# Bundled Managed Upstream Nginx notice

Formal release packages contain an architecture-specific Linux binary built
against musl and statically linked with pinned upstream sources:

- Nginx 1.30.4 — BSD-2-Clause license: <https://nginx.org/LICENSE>
- musl libc from the pinned Alpine 3.22.2 build image — MIT license: <https://musl.libc.org/doc/COPYRIGHT.html>
- OpenSSL 3.5.7 — Apache-2.0 license: <https://www.openssl.org/source/license.html>
- PCRE2 10.47 — BSD-3-Clause license: <https://github.com/PCRE2Project/pcre2/blob/pcre2-10.47/LICENCE.md>
- zlib 1.3.2 — zlib license: <https://zlib.net/zlib_license.html>

The OpenSSL build explicitly disables QUIC. Managed Upstream Nginx does not
enable HTTP/3, so the unused QUIC server implementation is excluded from the
formal binary.

The exact source URLs and SHA-256 values are pinned in
`build/upstream-nginx-musl.Dockerfile`. Build either supported artifact with:

```sh
./scripts/build-upstream-nginx-musl.sh amd64
./scripts/build-upstream-nginx-musl.sh arm64
```

The build definitions cover `linux/amd64` and `linux/arm64`. Hosted release jobs
use a pinned xx/Clang musl cross toolchain and emit both architectures. Nginx's
runtime configure probes are replaced by explicit cross-build results, while
type sizes, byte order and `sys_nerr` are determined at compile time. Neither
the build nor its configure phase uses QEMU or another target emulator. The
exact patch-set checksum is recorded in `BUILD-INFO`; the patch files preserve
their nginx-devel/Buildroot contributor attribution. The checked-in
`sbin/nginx` file is an amd64 development and CI fixture; the release pipeline
always rebuilds and tests the version-bound binary for every enabled
architecture.
