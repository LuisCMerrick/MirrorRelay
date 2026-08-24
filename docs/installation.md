# Installation

[English](installation.md) | [简体中文](installation.zh-CN.md)

DEB and RPM are the recommended formats for host-integrated production deployments. Published releases also provide one Docker Hub image for `linux/amd64` and `linux/arm64`. Every format contains the exact statically linked Managed Upstream Nginx binary tested by the matching release job. No Nginx package is installed from the target operating system, and nothing is compiled during package installation or container startup.

## Verify a release

Download the three packages for the required architecture and `SHA256SUMS` from the same GitHub Release, then run:

```sh
sha256sum -c SHA256SUMS
```

The package `BUILD-INFO` records the MirrorRelay version, commit, build ID, Go version, target architecture, pinned upstream library versions, configure arguments and both binary checksums. After installation, query the running binary with:

```sh
mirrorrelay version
mirrorrelay version --verbose
```

For the container image, prefer an immutable digest or a version tag and inspect
its multi-platform manifest before deployment:

```sh
export DOCKERHUB_USERNAME=<dockerhub-namespace>
docker buildx imagetools inspect \
  "${DOCKERHUB_USERNAME}/mirrorrelay:<version>"
```

## Install a DEB

```sh
sudo apt install ./mirrorrelay_<version>_amd64.deb
```

On arm64, use `sudo apt install ./mirrorrelay_<version>_arm64.deb` instead.

## Install an RPM

```sh
sudo dnf install ./mirrorrelay-<version>.x86_64.rpm
```

On arm64, use `sudo dnf install ./mirrorrelay-<version>.aarch64.rpm` instead.

## Run the Docker image

The release workflow publishes these Docker Hub tags:

- `<version>` and `v<version>` for every published release;
- `latest` only for a stable, non-prerelease release.

All tags resolve to one OCI manifest containing exactly `linux/amd64` and
`linux/arm64` application images, plus attached SBOM/provenance attestations.
Docker selects the matching host architecture automatically. Prefer the version
tag or the immutable digest reported by the release workflow instead of
`latest` for production rollouts.

The image runs as numeric UID/GID `65532`, stores its default configuration at
`/etc/mirrorrelay/config.yaml`, and uses the same binary paths as packages:

```text
/usr/bin/mirrorrelay
/usr/lib/mirrorrelay/nginx/nginx
```

Review and mount `configs/config.docker.yaml` rather than treating the bundled
example as production-ready. In particular, set the public URL, TLS paths and
exact administration CIDRs. The Docker-specific file explicitly listens on
`0.0.0.0:9081` inside the container; publish that port only on host loopback so
administrator-owned External Shared Nginx can reach it without exposing the
trusted frontend endpoint. Persist state, cache and logs. The runtime directory
is also shared for the optional zero-copy path to the private upstream socket:

```sh
docker run -d \
  --name mirrorrelay \
  --restart unless-stopped \
  --publish 127.0.0.1:9081:9081 \
  --mount type=bind,src=/absolute/config.yaml,dst=/etc/mirrorrelay/config.yaml,readonly \
  --mount type=volume,src=mirrorrelay-data,dst=/var/lib/mirrorrelay \
  --mount type=volume,src=mirrorrelay-cache,dst=/var/cache/mirrorrelay \
  --mount type=volume,src=mirrorrelay-logs,dst=/var/log/mirrorrelay \
  --mount type=bind,src=/run/mirrorrelay,dst=/run/mirrorrelay \
  "${DOCKERHUB_USERNAME}/mirrorrelay:<version>"
```

Create the bind-mounted paths with ownership and permissions suitable for UID
`65532` before starting the container. The image does not install, reconfigure
or restart host Nginx and does not alter its service user. The generated ingress
snippet connects to host `127.0.0.1:9081`; when zero-copy bypass is enabled it
also uses the shared private `upstream.sock`. Grant only the confirmed ingress
worker access through the host's normal group or ACL management, and never make
the runtime directory world-writable. If the container port is published on a
non-loopback host address, a network firewall must restrict it to trusted
ingress peers.

The packages install these fixed paths:

```text
/usr/bin/mirrorrelay
/usr/lib/mirrorrelay/nginx/nginx
/etc/mirrorrelay/config.yaml
/usr/lib/systemd/system/mirrorrelay.service
```

The DEB configuration selects `/etc/ssl/certs/ca-certificates.crt`; the RPM configuration selects the RHEL-family `/etc/pki/tls/certs/ca-bundle.crt`. Portable tar users must confirm the CA bundle path for their distribution before starting the service.

They create the `mirrorrelay` system account and private runtime, state, cache and log directories. The service is not enabled or started automatically. Review `/etc/mirrorrelay/config.yaml` first, especially `security.admin_cidrs`. Its packaged default permits only loopback clients; replace it with the exact administration networks when remote management is required. Then run:

```sh
sudo systemctl enable --now mirrorrelay.service
```

After the HTTPS ingress is available, open the administration URL. With an empty user database, the page requires one-time registration of the initial administrator instead of accepting a preset password. Registration is available only through the configured administration host/path and allowed administrator CIDRs; complete it before exposing that surface to untrusted networks. The management session cookie requires HTTPS outside loopback development access. Continue with the [Web UI guide](web-ui.md).

The systemd unit creates `/run/mirrorrelay` with mode `0750` and preserves it across MirrorRelay restarts so the version-bound Managed Upstream Nginx can remain attached when `stop_on_mirrorrelay_exit` is false. The directory is still transient and is cleared with the host's `/run` filesystem at boot.

## Connect External Shared Nginx

MirrorRelay generates a reviewed integration snippet under `ingress.snippet_path`. The package never installs, removes, edits, reloads or restarts External Shared Nginx and never claims public ports 80 or 443. By default the Go frontend listens on `127.0.0.1:9081`; both the listen IP and port are configurable with `server.local_address` and `server.local_port`.

The Go-to-Managed-Upstream-Nginx Unix socket remains enabled by default, and zero-copy bypass lets External Shared Nginx use that private endpoint after Go authorizes a request. To grant an existing Nginx worker access to `upstream.sock`—and to `frontend.sock` if the frontend Unix socket is explicitly enabled—add its confirmed service user to the `mirrorrelay` group. For example, after confirming that the worker runs as `www-data`:

```sh
sudo usermod -aG mirrorrelay www-data
```

Apply the group change and generated snippet using that ingress installation's normal maintenance procedure. Do not run this command for a guessed user. MirrorRelay does not alter an existing ingress account on your behalf.

Set `server.unix_socket_enabled: true` only to replace the default frontend TCP listener with `frontend.sock`. Every enabled Unix socket must use mode `0660`; world-writable `0666` or `0777` modes are rejected. To replace the default Go-to-Managed-Upstream-Nginx Unix socket with loopback TCP, explicitly set `upstream_nginx.upstream_unix_socket_enabled: false` and choose `upstream_nginx.upstream_local_port` distinct from the frontend port.

## Upgrade and remove

Package upgrades replace MirrorRelay, its version-bound Managed Upstream Nginx binary, the unit and built-in files while preserving `/etc/mirrorrelay/config.yaml`, `/var/lib/mirrorrelay/mirrorrelay.db`, configuration history and `/var/cache/mirrorrelay`.

Manually dispatched development builds use `0.0.1.git.<commit-epoch>.<commit>` versions so DEB and RPM package managers can order snapshots chronologically. Published releases and explicitly requested workflow versions keep their supplied version. Direct pushes do not start remote release builds. A manual Release Build does not publish a container by default; explicitly selecting `publish_container` pushes the immutable development-version tag plus `edge`, while leaving the stable-release `latest` tag unchanged.

A normal DEB or RPM removal also preserves configuration, database, cache, certificates and audit data. On Debian-family systems, only an explicit purge removes those persistent paths:

```sh
sudo apt purge mirrorrelay
```

RPM deliberately has no implicit purge script. After `dnf remove mirrorrelay`, an administrator who explicitly wants irreversible cleanup may remove only `/etc/mirrorrelay`, `/var/lib/mirrorrelay`, `/var/cache/mirrorrelay` and `/var/log/mirrorrelay`, then delete the dedicated account. Back up production state and resolve those exact paths before any purge operation.

## Install the tar archive

The portable archive is intended for manual deployment, testing and recovery. It contains the binaries, sample configuration, systemd unit, bilingual documentation, licenses, `BUILD-INFO` and an internal `SHA256SUMS` manifest:

```sh
tar -xzf mirrorrelay-<version>-linux-amd64.tar.gz
cd mirrorrelay-<version>
sha256sum -c SHA256SUMS
```

On arm64, extract `mirrorrelay-<version>-linux-arm64.tar.gz` instead.

The archive does not create users, directories or permissions. The administrator must reproduce the package layout and ownership described above, install the systemd unit, and connect External Shared Nginx explicitly.
