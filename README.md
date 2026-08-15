# RepoGate

[English](README.md) | [简体中文](README.zh-CN.md)

[![CI](https://github.com/LuisCMerrick/RepoGate/actions/workflows/ci.yml/badge.svg)](https://github.com/LuisCMerrick/RepoGate/actions/workflows/ci.yml)
[![Release Build](https://github.com/LuisCMerrick/RepoGate/actions/workflows/build.yml/badge.svg)](https://github.com/LuisCMerrick/RepoGate/actions/workflows/build.yml)

RepoGate is a pull-through gateway for Linux software repositories and Docker/OCI registries. It provides repository routing, caching, health checks, configuration history and a bilingual administration UI.

```text
Client -> External Shared Nginx -> RepoGate -> Managed Upstream Nginx -> Original Upstream
```

Managed Upstream Nginx is the only component that connects to Original Upstreams. RepoGate validates every data-plane change before atomically publishing and gracefully reloading it.

## Highlights

- APT, RPM, APK, OPKG, PyPI, npm, Maven, NuGet, Cargo, Go Proxy, Conda and generic repositories.
- Pull-only Docker/OCI Registry proxying with secured token and redirect handling.
- Shared-domain path routing and dedicated-host routing.
- A repository index at `/`, with each configured path behaving like a local mirror tree.
- Disk cache, health-aware upstream failover, cache purge and traffic statistics.
- Embedded English/Chinese Web UI with repository actions and validated operational settings.
- Unix sockets with mode `0660` by default; explicit loopback TCP fallback is available.

## Packages

Release builds produce DEB, RPM and tar.gz packages for both architectures:

| Architecture | DEB | RPM | Archive |
|---|---|---|---|
| amd64 | `repogate_<version>_amd64.deb` | `repogate-<version>.x86_64.rpm` | `repogate-<version>-linux-amd64.tar.gz` |
| arm64 | `repogate_<version>_arm64.deb` | `repogate-<version>.aarch64.rpm` | `repogate-<version>-linux-arm64.tar.gz` |

Installed locations:

```text
/usr/bin/repogate
/usr/lib/repogate/nginx/nginx
/etc/repogate/config.yaml
/usr/lib/systemd/system/repogate.service
```

The embedded Nginx is built statically against pinned musl, OpenSSL, PCRE2 and zlib sources. Packages never modify or reload an existing External Shared Nginx.

## Quick development start

```sh
go run ./cmd/repogate -dev
```

Open `https://127.0.0.1:8443/admin/` and sign in with `admin` / `adminadmin`. Development mode stores its data and generated certificate under `dev-data/`; never use its password in production.

Useful checks:

```sh
make check
make upstream-nginx-musl ARCH=amd64
make upstream-nginx-musl ARCH=arm64
```

## Documentation

- [Installation](docs/installation.md) ([中文](docs/installation.zh-CN.md))
- [Web UI guide](docs/web-ui.md) ([中文](docs/web-ui.zh-CN.md))
- [Configuration reference](docs/configuration.md) ([中文](docs/configuration.zh-CN.md))
- [Verification](docs/verification.md) ([中文](docs/verification.zh-CN.md))
- [Example configuration](configs/config.example.yaml)

RepoGate starts at version `0.0.1` and is licensed under [GNU GPL v3.0 only](LICENSE). Bundled third-party notices are in [nginx/NOTICE.md](nginx/NOTICE.md).
