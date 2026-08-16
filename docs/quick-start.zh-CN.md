# 快速上手指南

[English](quick-start.md) | [简体中文](quick-start.zh-CN.md)

本指南将指导您在 5 分钟内完成 MirrorRelay 的安装配置、创建首批上游镜像源，并配置客户端包管理器进行拉取与缓存验证。

---

## 1. 安装 MirrorRelay

### Debian / Ubuntu
```bash
# 下载最新的 .deb 发行包
sudo apt-get install --yes ./mirrorrelay_0.0.2_amd64.deb
```

### RHEL / Rocky Linux / Fedora
```bash
# 下载最新的 .rpm 发行包
sudo dnf install --yes ./mirrorrelay-0.0.2.x86_64.rpm
```

### 源码快速开发运行
```bash
git clone https://github.com/LuisCMerrick/MirrorRelay.git
cd MirrorRelay
go run ./cmd/mirrorrelay -dev
```

---

## 2. 登录 Web UI 管理后台

1. 在浏览器中打开：
   - **生产环境（通过外部 Nginx）**：`https://your-server-ip/admin/`
   - **开发模式**：`https://127.0.0.1:8443/admin/`
2. 输入管理员账号登录（开发模式默认：`admin` / `adminadmin`）。
3. 登录后进入**概览（Dashboard）**页面，可实时查看系统运行状态与服务健康度。

---

## 3. 添加首批镜像源仓库

### 示例 A：APT 镜像源 (Debian 12 Bookworm)
1. 在左侧边栏点击**仓库（Repositories）**，然后点击右上角**添加仓库（Add repository）**。
2. 填写仓库基本信息：
   - **名称**：`debian`
   - **仓库类型**：`Generic`（或 `Debian`）
   - **公开模式**：`Path prefix`（路径前缀）
   - **公开路径**：`/debian`
   - **上游源地址**：`https://deb.debian.org/debian`
   - **缓存策略**：开启（默认缓存模板）
3. 点击**保存仓库**。MirrorRelay 会自动生成 Nginx 候选配置，经 `nginx -t` 预检通过后平滑生效。

#### 配置 APT 客户端
编辑 `/etc/apt/sources.list`（或 `/etc/apt/sources.list.d/mirrorrelay.sources`）：
```text
deb http://your-server-ip/debian bookworm main contrib non-free non-free-firmware
deb http://your-server-ip/debian bookworm-updates main contrib non-free non-free-firmware
```
运行更新测试：
```bash
sudo apt-get update
```

---

### 示例 B：PyPI 镜像源 (Python 软件包)
1. 在 Web UI 点击**添加仓库**：
   - **名称**：`pypi`
   - **仓库类型**：`PyPI`
   - **公开路径**：`/pypi`
   - **上游源地址**：`https://pypi.org`
   - **HTML URL 重写**：开启
2. 点击**保存仓库**。

#### 配置 pip 客户端
```bash
pip install requests -i http://your-server-ip/pypi/simple/ --trusted-host your-server-ip
```
或持久化写入 `~/.pip/pip.conf`：
```ini
[global]
index-url = http://your-server-ip/pypi/simple/
trusted-host = your-server-ip
```

---

### 示例 C：Docker / OCI 容器镜像源 (Docker Hub 代理)
1. 在 Web UI 点击**添加仓库**：
   - **名称**：`docker-hub`
   - **仓库类型**：`Registry`
   - **公开路径**：`/v2`
   - **上游源地址**：`https://registry-1.docker.io`
   - **缓存策略**：开启
2. 点击**保存仓库**。

#### 配置 Docker Daemon
在客户端编辑 `/etc/docker/daemon.json`：
```json
{
  "registry-mirrors": [
    "http://your-server-ip"
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
