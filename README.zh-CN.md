# RepoGate

[English](README.md) | [简体中文](README.zh-CN.md)

[![CI](https://github.com/LuisCMerrick/RepoGate/actions/workflows/ci.yml/badge.svg)](https://github.com/LuisCMerrick/RepoGate/actions/workflows/ci.yml)
[![Release Build](https://github.com/LuisCMerrick/RepoGate/actions/workflows/build.yml/badge.svg)](https://github.com/LuisCMerrick/RepoGate/actions/workflows/build.yml)

RepoGate 是面向 Linux 软件仓库与 Docker/OCI Registry 的 Pull-through 网关，提供仓库路由、缓存、健康检查、配置历史以及中英文管理后台。

```text
客户端 -> External Shared Nginx -> RepoGate -> 受管上游 Nginx -> 原始上游
```

只有受管上游 Nginx 可以连接原始上游。RepoGate 会先验证每次数据面变更，再原子发布并执行 Graceful Reload。

## 主要功能

- 支持 APT、RPM、APK、OPKG、PyPI、npm、Maven、NuGet、Cargo、Go Proxy、Conda 和通用仓库。
- 支持仅 Pull 的 Docker/OCI Registry，并安全处理 Token 与重定向。
- 支持共享域名路径路由和独立域名路由。
- 域名根路径 `/` 展示仓库索引，每个仓库路径的使用体验类似本地镜像目录。
- 支持磁盘缓存、健康感知上游切换、缓存清理和流量统计。
- 内置中英文 Web UI，可操作仓库并严格校验大部分运行配置。
- 默认使用权限为 `0660` 的 Unix Socket，也可显式切换到回环 TCP 端口。

## 安装包

Release Build 同时生成两个架构的 DEB、RPM 和 tar.gz：

| 架构 | DEB | RPM | 压缩包 |
|---|---|---|---|
| amd64 | `repogate_<version>_amd64.deb` | `repogate-<version>.x86_64.rpm` | `repogate-<version>-linux-amd64.tar.gz` |
| arm64 | `repogate_<version>_arm64.deb` | `repogate-<version>.aarch64.rpm` | `repogate-<version>-linux-arm64.tar.gz` |

主要安装路径：

```text
/usr/bin/repogate
/usr/lib/repogate/nginx/nginx
/etc/repogate/config.yaml
/usr/lib/systemd/system/repogate.service
```

内置 Nginx 使用固定版本的 musl、OpenSSL、PCRE2 和 zlib 静态构建。安装包不会修改或 Reload 现有的 External Shared Nginx。

## 快速开发启动

```sh
go run ./cmd/repogate -dev
```

打开 `https://127.0.0.1:8443/admin/`，使用 `admin` / `adminadmin` 登录。开发模式的数据和证书位于 `dev-data/`，生产环境禁止使用该密码。

常用检查：

```sh
make check
make upstream-nginx-musl ARCH=amd64
make upstream-nginx-musl ARCH=arm64
```

## 文档

- [安装说明](docs/installation.zh-CN.md) ([English](docs/installation.md))
- [Web UI 使用说明](docs/web-ui.zh-CN.md) ([English](docs/web-ui.md))
- [配置参考](docs/configuration.zh-CN.md) ([English](docs/configuration.md))
- [验证说明](docs/verification.zh-CN.md) ([English](docs/verification.md))
- [配置示例](configs/config.example.yaml)

RepoGate 的程序版本从 `0.0.1` 开始，并采用 [GNU GPL v3.0 only](LICENSE) 许可证。内置第三方组件声明见 [nginx/NOTICE.md](nginx/NOTICE.md)。
