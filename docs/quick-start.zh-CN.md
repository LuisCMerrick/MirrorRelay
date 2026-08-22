# 快速上手指南

[English](quick-start.md) | [简体中文](quick-start.zh-CN.md)

本指南将指导您在 5 分钟内完成 MirrorRelay 的安装运行、创建首批上游镜像源（包含 Debian 主源、Debian Security 安全更新源、PyPI 源与 Docker Hub 镜像源），并配置客户端包管理器进行拉取与缓存验证。

---

## 1. 安装与运行 MirrorRelay

### 方式 A：本地快速开发与评估体验（无需任何外部依赖）
```bash
git clone https://github.com/LuisCMerrick/MirrorRelay.git
cd MirrorRelay
go run ./cmd/mirrorrelay -dev
```
- 开发模式监听 `https://127.0.0.1:8443`。
- 首次访问时注册初始管理员；系统不再提供默认账号密码。

### 方式 B：生产环境发行版安装包部署 (DEB / RPM)
```bash
# Debian / Ubuntu:
sudo apt-get install --yes ./mirrorrelay_0.0.16_amd64.deb

# RHEL / Rocky Linux / Fedora:
sudo dnf install --yes ./mirrorrelay-0.0.16.x86_64.rpm

# 审核 security.admin_cidrs 后启用服务：
sudoedit /etc/mirrorrelay/config.yaml
sudo systemctl enable --now mirrorrelay.service

# 授权外部共享 Nginx 运行用户访问 Unix Domain Socket：
sudo usermod -aG mirrorrelay www-data
```
在外部共享 Nginx 的 `server` 块（例如服务域名 `https://mirror.example.com`）中引入自动生成的集成片段 `/var/lib/mirrorrelay/integration/external-nginx/mirrorrelay.conf` 并平滑重载 Nginx。完整生产部署细节请参见 [安装说明](installation.zh-CN.md)。

---

## 2. 登录 Web UI 管理后台

1. 在浏览器中打开：
   - **生产环境**：`https://mirror.example.com/admin/`
   - **开发模式**：`https://127.0.0.1:8443/admin/`
2. 如果数据库为空，请注册初始管理员；否则使用已有账号登录。
3. 登录后进入**概览（Dashboard）**页面，可实时查看系统运行状态与服务健康度。

---

## 3. 添加首批镜像源仓库

### 示例 A：Debian 12 Bookworm + Debian Security 源

#### 1. 添加 `debian` 主源
在 Web UI（**仓库（Repositories）** -> **添加仓库（Add repository）**）：
- **名称**：`debian`
- **标识（Slug）**：`debian`
- **仓库类型**：`apt`
- **公开模式**：`Path prefix`（路径前缀）
- **公开路径**：`/debian`
- **上游源地址**：`https://deb.debian.org/debian`
- **缓存策略**：开启（默认策略）

#### 2. 添加 `debian-security` 安全源
在 Web UI（**仓库（Repositories）** -> **添加仓库（Add repository）**）：
- **名称**：`debian-security`
- **标识（Slug）**：`debian-security`
- **仓库类型**：`apt`
- **公开模式**：`Path prefix`（路径前缀）
- **公开路径**：`/debian-security`
- **上游源地址**：`https://security.debian.org/debian-security`
- **缓存策略**：开启（默认策略）

#### 配置 APT 客户端

以下两种格式只选一种。现代 DEB822 格式请创建 `/etc/apt/sources.list.d/mirrorrelay.sources`：

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

传统单行格式请创建 `/etc/apt/sources.list.d/mirrorrelay.list`（也可以编辑 `/etc/apt/sources.list`）：

```text
deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://mirror.example.com/debian/ bookworm main contrib non-free non-free-firmware
deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://mirror.example.com/debian/ bookworm-updates main contrib non-free non-free-firmware
deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://mirror.example.com/debian-security/ bookworm-security main contrib non-free non-free-firmware
```
运行更新测试：
```bash
sudo apt-get update
```

---

### 示例 B：PyPI 镜像源 (Python 软件包)
在 Web UI（**仓库（Repositories）** -> **添加仓库（Add repository）**）：
- **名称**：`pypi`
- **标识（Slug）**：`pypi`
- **仓库类型**：`pypi`
- **公开路径**：`/pypi`
- **上游源地址**：`https://pypi.org`
- **HTML URL 重写**：开启
- **缓存策略**：开启（默认策略）

#### 配置 pip 客户端
```bash
pip install requests -i https://mirror.example.com/pypi/simple/
```
或持久化写入 `~/.pip/pip.conf`：
```ini
[global]
index-url = https://mirror.example.com/pypi/simple/
```

---

### 示例 C：Docker / OCI 容器镜像源 (Docker Hub 代理)
在 Web UI（**仓库（Repositories）** -> **添加仓库（Add repository）**）：
- **名称**：`docker-hub`
- **标识（Slug）**：`docker-hub`
- **仓库类型**：`docker-registry`
- **公开路径**：`/v2`
- **上游源地址**：`https://registry-1.docker.io`
- **鉴权模式**：`anonymous`（若代理私有仓库可配置 `upstream_credentials`）
- **缓存策略**：开启

#### 配置 Docker Daemon
编辑客户端 `/etc/docker/daemon.json`：
```json
{
  "registry-mirrors": [
    "https://mirror.example.com"
  ]
}
```
重启 Docker 服务并测试拉取：
```bash
sudo systemctl restart docker
docker pull ubuntu:latest
```

---

## 4. 后续探索

- 查阅 [Web UI 使用指南](web-ui.zh-CN.md) 了解缓存淘汰策略、限流与审计日志。
- 学习集群跨地域加速与 307 调度请看 [分布式部署指南](distributed.zh-CN.md)。
- 了解系统双平面架构与安全机制请看 [架构设计指南](architecture.zh-CN.md)。
- 查阅 [配置参考手册](configuration.zh-CN.md) 了解所有 YAML 配置项。
