# Installation

[English](installation.md) | [简体中文](installation.zh-CN.md)

DEB and RPM are the recommended production formats. Each architecture-specific package contains MirrorRelay and the exact statically linked Managed Upstream Nginx binary tested by the release workflow. No Nginx package is installed from the target operating system, and nothing is compiled on the target host.

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

The packages install these fixed paths:

```text
/usr/bin/mirrorrelay
/usr/lib/mirrorrelay/nginx/nginx
/etc/mirrorrelay/config.yaml
/usr/lib/systemd/system/mirrorrelay.service
```

The DEB configuration selects `/etc/ssl/certs/ca-certificates.crt`; the RPM configuration selects the RHEL-family `/etc/pki/tls/certs/ca-bundle.crt`. Portable tar users must confirm the CA bundle path for their distribution before starting the service.

They create the `mirrorrelay` system account and private runtime, state, cache and log directories. The service is not enabled or started automatically. Create `/etc/mirrorrelay/environment` with mode `0600`, then edit it without placing the password in shell history:

```sh
sudo touch /etc/mirrorrelay/environment
sudo chmod 0600 /etc/mirrorrelay/environment
sudoedit /etc/mirrorrelay/environment
```

The file must contain a strong bootstrap password and may override the initial username:

```text
MIRRORRELAY_ADMIN_USERNAME=admin
MIRRORRELAY_ADMIN_PASSWORD=replace-with-a-long-random-password
```

Review `/etc/mirrorrelay/config.yaml`, then run:

```sh
sudo systemctl enable --now mirrorrelay.service
```

After the first administrator exists and sign-in succeeds, remove `MIRRORRELAY_ADMIN_PASSWORD` from the environment file and restart MirrorRelay. Existing users are stored in SQLite and do not require the bootstrap secret on later starts.

After the HTTPS ingress is available, continue with the [Web UI guide](web-ui.md). The management session cookie requires HTTPS outside loopback development access.

The systemd unit creates `/run/mirrorrelay` with mode `0750` and preserves it across MirrorRelay restarts so the version-bound Managed Upstream Nginx can remain attached when `stop_on_mirrorrelay_exit` is false. The directory is still transient and is cleared with the host's `/run` filesystem at boot.

## Connect External Shared Nginx

MirrorRelay generates a reviewed integration snippet under `ingress.snippet_path`. The package never installs, removes, edits, reloads or restarts External Shared Nginx and never claims public ports 80 or 443.

Both local sockets default to mode `0660`; world-writable `0666` or `0777` modes are rejected. To let an existing Nginx worker read `frontend.sock`, explicitly grant its service user membership in the `mirrorrelay` group. For example, after confirming that the worker runs as `www-data`:

```sh
sudo usermod -aG mirrorrelay www-data
```

Apply the group change and generated snippet using that ingress installation's normal maintenance procedure. Do not run this command for a guessed user. MirrorRelay does not alter an existing ingress account on your behalf.

If Unix sockets cannot be used, explicitly set `server.unix_socket_enabled: false` and/or `upstream_nginx.upstream_unix_socket_enabled: false`, then choose distinct loopback ports with `server.local_port` and `upstream_nginx.upstream_local_port`.

## Upgrade and remove

Package upgrades replace MirrorRelay, its version-bound Managed Upstream Nginx binary, the unit and built-in files while preserving `/etc/mirrorrelay/config.yaml`, `/var/lib/mirrorrelay/mirrorrelay.db`, configuration history and `/var/cache/mirrorrelay`.

Manually dispatched development builds use `0.0.1.git.<commit-epoch>.<commit>` versions so DEB and RPM package managers can order snapshots chronologically. Published releases and explicitly requested workflow versions keep their supplied version. Direct pushes do not start remote builds.

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
