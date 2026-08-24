# MirrorRelay

[English](README.md) | [简体中文](README.zh-CN.md)

[![CI](https://github.com/LuisCMerrick/MirrorRelay/actions/workflows/ci.yml/badge.svg)](https://github.com/LuisCMerrick/MirrorRelay/actions/workflows/ci.yml)
[![Release Build](https://github.com/LuisCMerrick/MirrorRelay/actions/workflows/build.yml/badge.svg)](https://github.com/LuisCMerrick/MirrorRelay/actions/workflows/build.yml)
[![Release](https://img.shields.io/github/v/release/LuisCMerrick/MirrorRelay)](https://github.com/LuisCMerrick/MirrorRelay/releases)
[![License: GPL-3.0](https://img.shields.io/badge/License-GPL--3.0-blue.svg)](LICENSE)

自托管的 Linux 软件仓库与 OCI 容器镜像拉取代理（Pull-through Caching Gateway），内置 Web 管理界面与受管 Nginx 数据平面。

```text
APT · RPM · APK · OPKG · PyPI · npm · Maven · NuGet · Cargo · Go Proxy · Conda · Docker / OCI

无需完整全量同步镜像仓库。
```

```text
客户端 (apt / dnf / pip / npm / docker)
   │
   ▼
外部共享 Nginx (入口接入: 80 / 443)
   │ (默认 TCP 127.0.0.1:9081 / 显式启用后使用 Unix Socket 0660)
   ▼
MirrorRelay (Go 控制平面与路由器)
   ├── Web UI 与管理 API (/admin/)
   ├── 容器镜像 Token Broker 与重定向 Broker
   ├── 有界元数据与 HTML 重写器
   ├── SQLite 持久化期望状态
   └── 候选配置生成与原子发布
   │ (默认 Unix Socket 0660 / 显式关闭后使用回环 TCP)
   ▼
受管上游 Nginx (独立静态链接 Musl 数据平面)
   ├── 代理缓存与本地内容存储
   ├── SSL 证书验证与 DNS 固定
   └── 多上游故障转移
   │
   ▼
原始上游源 (Debian, Ubuntu, Rocky, PyPI, Docker Hub 等)
```

---

## 为什么选择 MirrorRelay？

传统的仓库镜像方案通常面临**全量镜像同步占用庞大**或**手动配置 Nginx `proxy_pass` 维护困难且功能受限**的问题。MirrorRelay 从根本上解决了两者的痛点：

| 挑战 | 全量同步镜像站 (如 `apt-mirror`, `bandersnatch`) | 手动 Nginx `proxy_pass` | MirrorRelay |
|---|---|---|---|
| **磁盘占用** | 初始即需数百 GB 甚至数 TB 磁盘 | 较低（按需缓存） | **低**：按需拉取磁盘缓存，冷热自动淘汰 |
| **初始同步延迟** | 初次同步需数小时至数天 | 无延迟 | **零延迟**：配置后立即对外提供服务 |
| **仓库管理维护** | 复杂的同步脚本与定时任务 | 手工修改配置文件并重载 | **Web UI 与 API**：全功能可视化 CRUD 与审计日志 |
| **Docker / OCI 镜像** | 难以镜像私有/公有 Registry | 无法处理 Bearer 鉴权与 CDN 302 重定向 | **内置 Token Broker 与重定向安全代理** |
| **数据平面安全** | 不适用 | 语法配置错误易导致整站瘫痪 | **期望/生效分离**（`nginx -t` 严格验证后原子热重载） |
| **上游请求安全** | 不适用 | 存在 SSRF 与 DNS 重绑定风险 | **严格 CIDR 过滤、IP 地址固定与 TLS SNI 验证** |
| **多节点协同调度** | 依赖复杂 DNS / CDN 配置 | 难以多节点智能调度 | **Coordinator / Edge 分布式 307 智能重定向调度** |

---

## 核心功能

| 功能特性 | 支持状态 | 说明 |
|---|---|---|
| **软件包仓库代理与缓存** | ✅ 支持 | APT、RPM/DNF、APK、OPKG、PyPI、npm、Maven、Cargo、Go Proxy、Conda |
| **Docker / OCI 镜像拉取代理** | ✅ 支持 | 完整 `/v2/` 鉴权挑战处理、Token 代理、多上游回退、S3/CDN 重定向代理 |
| **多上游故障转移** | ✅ 支持 | 自动健康检查、权重、优先级配置与故障自动降级 |
| **期望与生效状态分离** | ✅ 支持 | 候选配置生成、`nginx -t` 预检、原子覆盖与平滑无中断重载（Graceful Reload） |
| **分布式软件包路由** | ✅ 支持 | Coordinator / Edge 架构，支持客户端 CIDR、Geo 地区、优先级与权重 307 调度 |
| **Edge 节点配置一致性** | ✅ 支持 | Coordinator 与 Edge 节点之间的配置版本与指纹实时一致性检测 |
| **Docker / OCI 分布式路由** | 🚧 计划中 | 容器镜像分布式路由计划在未来控制面升级中提供（单节点 OCI 拉取代理已完整支持） |
| **内置双语 Web 管理界面** | ✅ 支持 | 零前端外部依赖、轻量响应式设计，支持中英文即时切换与系统一键重启 |

---

## 目标兼容性 (Target Compatibility)

| 软件包生态 | 代理模式 | 动态缓存 | 元数据 / URL 重写 | 已验证客户端与系统 |
|---|:---:|:---:|:---:|---|
| **APT** | ✅ | ✅ | 可选 HTML 目录重写 | Debian 11/12, Ubuntu 22.04/24.04 |
| **RPM / DNF** | ✅ | ✅ | 可选 HTML 目录重写 | Rocky Linux 8/9, AlmaLinux 9, Fedora 40/41 |
| **Alpine APK** | ✅ | ✅ | 不适用 | Alpine 3.19/3.20/3.21 |
| **OpenWrt OPKG** | ✅ | ✅ | 可选 HTML 目录重写 | OpenWrt 22.03/23.05 |
| **PyPI** | ✅ | ✅ | ✅ Simple HTML 索引重写 | `pip` 23.x/24.x |
| **npm** | ✅ | ✅ | ✅ JSON 注册表元数据重写 | `npm` 9.x/10.x, `pnpm`, `yarn` |
| **Go Modules** | ✅ | ✅ | 不适用 | Go 1.22/1.23/1.24 |
| **Rust Cargo** | ✅ | ✅ | 不适用 | Cargo / `crates.io` index |
| **Maven / Gradle** | ✅ | ✅ | 可选 HTML 目录重写 | Maven 3.8/3.9, Gradle 8.x |
| **Docker / OCI** | ✅ 拉取代理 | ✅ 分层与 Blob | ✅ Token Broker 与 S3/CDN 重定向代理 | Docker Engine 24.x/26.x/27.x, Podman 4.x/5.x |

---

## 5 分钟快速上手

### 方式 1：本地快速开发与评估体验（无需任何外部依赖）

```bash
git clone https://github.com/LuisCMerrick/MirrorRelay.git
cd MirrorRelay
go run ./cmd/mirrorrelay -dev
```

在浏览器打开 `https://127.0.0.1:8443/admin/` 并注册初始管理员。MirrorRelay 不提供默认账号密码。

### 方式 2：生产环境发行版安装包部署 (DEB / RPM)

1. **安装软件包**：
   ```bash
   # Debian / Ubuntu:
   sudo apt-get install --yes ./mirrorrelay_0.0.16_amd64.deb

   # RHEL / Rocky Linux / Fedora:
   sudo dnf install --yes ./mirrorrelay-0.0.16.x86_64.rpm
   ```
2. **审核管理网络边界并启用服务**：
   ```bash
   sudoedit /etc/mirrorrelay/config.yaml   # 设置 security.admin_cidrs
   sudo systemctl enable --now mirrorrelay.service
   ```
3. **对接外部共享 Nginx**（详见 [安装说明](docs/installation.zh-CN.md)）：
   生成的片段默认连接 `127.0.0.1:9081` 前端。零拷贝旁路需要访问私有上游
   Socket，或显式启用前端 Unix Socket 时，请将宿主机 Web 运行用户（如
   `www-data` 或 `nginx`）加入 `mirrorrelay` 用户组：
   ```bash
   sudo usermod -aG mirrorrelay www-data
   ```
   在外部 Nginx 的 `server` 块中引入自动生成的集成配置 `/var/lib/mirrorrelay/integration/external-nginx/mirrorrelay.conf` 并平滑重载 Nginx。
4. 打开 HTTPS 管理地址并完成一次性的初始管理员注册，再向不受信任网络开放管理面。

### 方式 3：官方多架构 Docker 镜像

正式 Release 还会向 Docker Hub 推送一个同时支持 `linux/amd64` 与
`linux/arm64` 的镜像；Docker 会自动选择宿主机架构：

```bash
export DOCKERHUB_USERNAME=<dockerhub-namespace>
docker pull "${DOCKERHUB_USERNAME}/mirrorrelay:<version>"
```

版本号、带 `v` 前缀的版本号以及稳定 Release 的 `latest` 标签指向同一个
已验证的多平台 Manifest。镜像包含对应软件包任务实际测试的同一份
MirrorRelay 与 Managed Upstream Nginx 二进制。配置、持久目录挂载和
External Shared Nginx 接入方式请阅读[安装说明](docs/installation.zh-CN.md)。

### 仓库添加与客户端配置示例

1. **在 Web UI 中添加 Debian 仓库**：
   - 名称：`debian`，标识：`debian`，类型：`apt`，公开路径：`/debian`
   - 上游源地址：`https://deb.debian.org/debian`
2. **配置客户端源地址**，可使用 DEB822（`/etc/apt/sources.list.d/mirrorrelay.sources`）：
   ```text
   Types: deb
   URIs: https://mirror.example.com/debian/
   Suites: bookworm
   Components: main contrib non-free non-free-firmware
   Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
   ```
   或传统单行格式（`/etc/apt/sources.list.d/mirrorrelay.list`）：
   ```text
   deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://mirror.example.com/debian/ bookworm main contrib non-free non-free-firmware
   ```
3. **运行更新测试**：
   ```bash
   sudo apt-get update
   ```

详细操作步骤请阅读 [快速上手指南](docs/quick-start.zh-CN.md) 与 [安装说明](docs/installation.zh-CN.md)。

---

## 架构概览

MirrorRelay 采用清晰的双平面架构，将管理控制与高吞吐数据流严格分离：

```text
外部共享 Nginx (系统管理员维护)
         ↓  (默认 TCP 127.0.0.1:9081；Unix Socket 0660 需显式启用)
MirrorRelay 前端 (Go 核心服务)
  - 访问策略与身份认证
  - 动态路由与 URL 路径重写
  - 容器镜像 Token Broker 鉴权中继
  - 配置生命周期管理 (SQLite 持久化期望状态)
         ↓  (默认 Unix Socket 0660；回环 TCP 需显式关闭 Socket)
受管上游 Nginx (专用 Musl 隔离进程)
  - 本地磁盘高速缓存 (/var/cache/mirrorrelay)
  - 连接池复用与 SSL 证书链严格验证
  - DNS 解析与 IP 绑定
         ↓
原始上游源 (HTTPS / HTTP)
```

- **安全不变式**：Go 服务本身绝不直接发起对上游软件包服务器的 HTTP 连接，所有上游流量均由受限、静态编译的 `受管上游 Nginx` 数据平面代理完成。
- **无损热重载**：仓库配置变更时，系统生成候选 Nginx 配置，经 `nginx -t` 严格预检通过后原子替换并平滑重载（`HUP`），全程不丢连接。

深入了解请阅读 [架构设计指南](docs/architecture.zh-CN.md)。

---

## 分布式多节点部署

MirrorRelay 内置分布式集群能力，轻松实现跨区域、多节点的负载均衡与边缘加速：

- **Coordinator 控制中心**：全局配置权威节点，负责仓库定义、客户端路由策略与节点健康监控。
- **Edge 边缘节点**：独立的缓存加速节点，通过 Coordinator 的 HTTP 307 重定向直接向客户端提供缓存与拉取服务。
- **路由调度策略**：支持客户端 IP CIDR 匹配、Geo 地区、优先级与权重调度。
- **集群容灾**：当某个 Edge 节点出现故障时，Coordinator 自动切换至下一可用健康节点（若全部 Edge 离线则返回 HTTP 503）。

深入了解请阅读 [分布式部署指南](docs/distributed.zh-CN.md)。

---

## 软件包、容器镜像与发行布局

为 `linux/amd64` 与 `linux/arm64` 平台提供静态编译的标准发行包：

| 架构 | DEB 安装包 | RPM 安装包 | 压缩包归档 |
|---|---|---|---|
| **amd64** | `mirrorrelay_<version>_amd64.deb` | `mirrorrelay-<version>.x86_64.rpm` | `mirrorrelay-<version>-linux-amd64.tar.gz` |
| **arm64** | `mirrorrelay_<version>_arm64.deb` | `mirrorrelay-<version>.aarch64.rpm` | `mirrorrelay-<version>-linux-arm64.tar.gz` |

每个 GitHub Release 还会提供与架构无关的 `mirrorrelay-<version>-source-with-vendor.tar.gz`，其中包含 Git 跟踪的源码树以及生成的 Go `vendor/` 目录，便于离线和可复现的源码构建。

同一 Release 还会将 `<dockerhub-namespace>/mirrorrelay:<version>` 发布为覆盖
两个架构的 Docker Hub OCI Manifest，并提供 `v<version>`；稳定 Release
同时更新 `latest`。发布工作流记录的 Digest 是其不可变身份。

标准文件系统布局：
```text
/usr/bin/mirrorrelay                         # 主程序二进制文件
/usr/lib/mirrorrelay/nginx/nginx             # 绑定的 musl 受管上游 Nginx
/etc/mirrorrelay/config.yaml                 # 配置文件 (权限 0640)
/usr/lib/systemd/system/mirrorrelay.service  # 沙箱化 systemd 服务单元
/var/lib/mirrorrelay/mirrorrelay.db          # 持久化 SQLite 数据库
/var/cache/mirrorrelay/                      # 软件包与容器层本地磁盘缓存
/var/log/mirrorrelay/upstream-nginx/         # 访问与错误日志
/run/mirrorrelay/                            # 运行时 PID 与 Unix Domain Socket
```

---

## 文档索引

- **入门使用**：
  - [快速上手指南](docs/quick-start.zh-CN.md) ([English](docs/quick-start.md)) — 5 分钟上手实操教程。
  - [安装说明](docs/installation.zh-CN.md) ([English](docs/installation.md)) — 生产软件包、绿色归档与 Docker 镜像部署。
  - [Web UI 使用指南](docs/web-ui.zh-CN.md) ([English](docs/web-ui.md)) — 双语管理后台与仓库操作指南。
- **核心架构与深度剖析**：
  - [架构设计指南](docs/architecture.zh-CN.md) ([English](docs/architecture.md)) — 双平面架构设计、生命周期与数据流。
  - [Docker 与 OCI 镜像代理](docs/docker-oci.zh-CN.md) ([English](docs/docker-oci.md)) — Token Broker、CDN 重定向与客户端配置。
  - [分布式集群部署](docs/distributed.zh-CN.md) ([English](docs/distributed.md)) — Coordinator、Edge 与 307 路由调度。
  - [安全模型与规范](docs/security.zh-CN.md) ([English](docs/security.md)) — SSRF 防御、Token 哈希与隔离策略。
- **参考与运维**：
  - [配置参考手册](docs/configuration.zh-CN.md) ([English](docs/configuration.md)) — 完整 YAML 配置文件指南。
  - [生产验收与验证](docs/verification.zh-CN.md) ([English](docs/verification.md)) — 大对象传输、高吞吐与故障切换验证规范。
  - [完整配置示例](configs/config.example.yaml) — 开箱即用的 YAML 模板。
  - [开发路线图](ROADMAP.md) — 未来版本规划与特性里程碑。
  - [贡献指南](CONTRIBUTING.md) — 社区开发流程与规范。

---

## 安全政策

发现潜在安全漏洞请遵循 [安全政策](.github/SECURITY.md) 负责任地向我们报告。

---

## 开源协议

MirrorRelay 遵循 [GNU General Public License v3.0](LICENSE) 开源协议。  
打包集成的 Nginx 第三方依赖组件协议（musl、OpenSSL、PCRE2、zlib）详见 [nginx/NOTICE.md](nginx/NOTICE.md)。
