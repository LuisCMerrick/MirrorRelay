# Quick Start Guide

[English](quick-start.md) | [简体中文](quick-start.zh-CN.md)

This guide walks you through setting up MirrorRelay, creating your first upstream repository mirrors, and configuring client package managers in under 5 minutes.

---

## 1. Install MirrorRelay

### Debian / Ubuntu
```bash
# Download latest .deb release package
sudo apt-get install --yes ./mirrorrelay_0.0.2_amd64.deb
```

### RHEL / Rocky Linux / Fedora
```bash
# Download latest .rpm release package
sudo dnf install --yes ./mirrorrelay-0.0.2.x86_64.rpm
```

### Quick Development Run (from Source)
```bash
git clone https://github.com/LuisCMerrick/MirrorRelay.git
cd MirrorRelay
go run ./cmd/mirrorrelay -dev
```

---

## 2. Access the Web Management UI

1. Open your browser and navigate to:
   - **Production (via Shared Nginx)**: `https://your-server-ip/admin/`
   - **Development Mode**: `https://127.0.0.1:8443/admin/`
2. Sign in with the administrator credentials (in development mode: `admin` / `adminadmin`).
3. You will land on the **Dashboard** displaying live system status and service health.

---

## 3. Add Your First Repositories

### Example A: APT Repository (Debian 12 Bookworm)
1. Click **Repositories** in the sidebar navigation, then click **Add repository**.
2. Fill in the repository details:
   - **Name**: `debian`
   - **Type**: `Generic` (or `Debian`)
   - **Public Mode**: `Path prefix`
   - **Public Path**: `/debian`
   - **Upstream URLs**: `https://deb.debian.org/debian`
   - **Cache**: Enabled (Default Profile)
3. Click **Save repository**. MirrorRelay will generate a candidate Nginx configuration, validate it with `nginx -t`, and reload the service automatically.

#### Configure APT Client
Edit `/etc/apt/sources.list` (or `/etc/apt/sources.list.d/mirrorrelay.sources`):
```text
deb http://your-server-ip/debian bookworm main contrib non-free non-free-firmware
deb http://your-server-ip/debian bookworm-updates main contrib non-free non-free-firmware
```
Run:
```bash
sudo apt-get update
```

---

### Example B: PyPI Repository (Python Wheels & Packages)
1. Click **Add repository** in the Web UI:
   - **Name**: `pypi`
   - **Type**: `PyPI`
   - **Public Path**: `/pypi`
   - **Upstream URLs**: `https://pypi.org`
   - **HTML URL Rewrite**: Enabled
2. Click **Save repository**.

#### Configure pip Client
```bash
pip install requests -i http://your-server-ip/pypi/simple/ --trusted-host your-server-ip
```
Or set persistently in `~/.pip/pip.conf`:
```ini
[global]
index-url = http://your-server-ip/pypi/simple/
trusted-host = your-server-ip
```

---

### Example C: Docker / OCI Container Registry (Docker Hub Proxy)
1. Click **Add repository** in the Web UI:
   - **Name**: `docker-hub`
   - **Type**: `Registry`
   - **Public Path**: `/v2`
   - **Upstream URLs**: `https://registry-1.docker.io`
   - **Cache**: Enabled
2. Click **Save repository**.

#### Configure Docker Daemon
Add the registry mirror in `/etc/docker/daemon.json`:
```json
{
  "registry-mirrors": [
    "http://your-server-ip"
  ]
}
```
Restart Docker daemon:
```bash
sudo systemctl restart docker
docker pull ubuntu:latest
```

---

## 4. Next Steps

- Explore [Web UI Guide](web-ui.md) for customizing cache policies, rate limits, and audit logs.
- Learn about multi-node clustering in [Distributed Deployment](distributed.md).
- Understand data-plane separation in [Architecture Guide](architecture.md).
- Refer to [Configuration Reference](configuration.md) for all YAML settings.
