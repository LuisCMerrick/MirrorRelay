# Quick Start Guide

[English](quick-start.md) | [简体中文](quick-start.zh-CN.md)

This guide walks you through setting up MirrorRelay, creating your first upstream repository mirrors (including Debian, Debian Security, PyPI, and Docker Hub), and configuring client package managers in under 5 minutes.

---

## 1. Install & Launch MirrorRelay

### Option A: Local Development & Evaluation (Zero Setup)
```bash
git clone https://github.com/LuisCMerrick/MirrorRelay.git
cd MirrorRelay
go run ./cmd/mirrorrelay -dev
```
- Development mode listens on `https://127.0.0.1:8443`.
- On the first visit, register the initial administrator; there are no default credentials.

### Option B: Production Package Deployment (DEB / RPM)
```bash
# Debian / Ubuntu:
sudo apt-get install --yes ./mirrorrelay_0.0.20_amd64.deb

# RHEL / Rocky Linux / Fedora:
sudo dnf install --yes ./mirrorrelay-0.0.20.x86_64.rpm

# Review security.admin_cidrs, then enable the service:
sudoedit /etc/mirrorrelay/config.yaml
sudo systemctl enable --now mirrorrelay.service

# The frontend uses 127.0.0.1:9081 by default. Authorize the confirmed ingress
# worker for the private upstream socket used by zero-copy bypass (and for the
# frontend socket only if you explicitly enable it):
sudo usermod -aG mirrorrelay www-data
```
Include `/var/lib/mirrorrelay/integration/external-nginx/mirrorrelay.conf` in your External Shared Nginx `server` configuration block (serving e.g. `https://mirror.example.com`) and reload Nginx. See [Installation Guide](installation.md) for full production details.

---

## 2. Access the Web Management UI

1. Open your browser and navigate to:
   - **Production**: `https://mirror.example.com/admin/`
   - **Development**: `https://127.0.0.1:8443/admin/`
2. If the database is empty, register the initial administrator. Otherwise, sign in with an existing account.
3. You will land on the **Dashboard** displaying live system status and service health.

---

## 3. Add Your First Repositories

### Example A: Debian 12 Bookworm + Debian Security

#### 1. Add `debian` main repository
In Web UI (**Repositories** -> **Add repository**):
- **Name**: `debian`
- **Slug**: `debian`
- **Type**: `apt`
- **Public Mode**: `Path prefix`
- **Public Path**: `/debian`
- **Upstream URLs**: `https://deb.debian.org/debian`
- **Cache**: Enabled (Default Profile)

#### 2. Add `debian-security` repository
In Web UI (**Repositories** -> **Add repository**):
- **Name**: `debian-security`
- **Slug**: `debian-security`
- **Type**: `apt`
- **Public Mode**: `Path prefix`
- **Public Path**: `/debian-security`
- **Upstream URLs**: `https://security.debian.org/debian-security`
- **Cache**: Enabled (Default Profile)

#### Configure APT Client

Choose exactly one of the following formats. For modern DEB822, create `/etc/apt/sources.list.d/mirrorrelay.sources`:

```text
Types: deb
URIs: https://mirror.example.com/debian/
Suites: bookworm bookworm-updates
Components: main contrib non-free non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg

Types: deb
URIs: https://mirror.example.com/debian-security/
Suites: bookworm-security
Components: main contrib non-free non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
```

For the traditional one-line format, create `/etc/apt/sources.list.d/mirrorrelay.list` (or edit `/etc/apt/sources.list`):

```text
deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://mirror.example.com/debian/ bookworm main contrib non-free non-free-firmware
deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://mirror.example.com/debian/ bookworm-updates main contrib non-free non-free-firmware
deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://mirror.example.com/debian-security/ bookworm-security main contrib non-free non-free-firmware
```
Run:
```bash
sudo apt-get update
```

---

### Example B: PyPI Repository (Python Packages)
In Web UI (**Repositories** -> **Add repository**):
- **Name**: `pypi`
- **Slug**: `pypi`
- **Type**: `pypi`
- **Public Path**: `/pypi`
- **Upstream URLs**: `https://pypi.org`
- **HTML URL Rewrite**: Enabled
- **Cache**: Enabled (Default Profile)

#### Configure pip Client
```bash
pip install requests -i https://mirror.example.com/pypi/simple/
```
Or set persistently in `~/.pip/pip.conf`:
```ini
[global]
index-url = https://mirror.example.com/pypi/simple/
```

---

### Example C: Docker / OCI Container Registry (Docker Hub Proxy)
In Web UI (**Repositories** -> **Add repository**):
- **Name**: `docker-hub`
- **Slug**: `docker-hub`
- **Type**: `docker-registry`
- **Public Path**: `/v2`
- **Upstream URLs**: `https://registry-1.docker.io`
- **Auth Mode**: `anonymous` (or `upstream_credentials` for private access)
- **Cache**: Enabled

#### Configure Docker Daemon
Edit `/etc/docker/daemon.json`:
```json
{
  "registry-mirrors": [
    "https://mirror.example.com"
  ]
}
```
Restart Docker daemon and pull an image:
```bash
sudo systemctl restart docker
docker pull ubuntu:latest
```

---

## 4. Next Steps

- Explore [Web UI Guide](web-ui.md) for configuring cache retention policies and rate limits.
- Learn about multi-node clustering in [Distributed Deployment](distributed.md).
- Understand two-plane architecture in [Architecture Guide](architecture.md).
- Refer to [Configuration Reference](configuration.md) for all YAML settings.
