# 安装

[English](installation.md) | [简体中文](installation.zh-CN.md)

生产环境推荐使用 DEB 或 RPM。每个架构的软件包都包含 MirrorRelay，以及发布流水线实际测试过的同一个静态链接 Managed Upstream Nginx 二进制。目标主机不会通过系统包管理器安装 Nginx，也不会现场编译。

## 校验 Release

从同一个 GitHub Release 下载所需架构的三个软件包和 `SHA256SUMS`，然后执行：

```sh
sha256sum -c SHA256SUMS
```

软件包中的 `BUILD-INFO` 记录 MirrorRelay 版本、Commit、Build ID、Go 版本、目标架构、固定的上游库版本、Configure 参数和两个二进制的校验和。安装后可查询：

```sh
mirrorrelay version
mirrorrelay version --verbose
```

## 安装 DEB

```sh
sudo apt install ./mirrorrelay_<version>_amd64.deb
```

arm64 请改用 `sudo apt install ./mirrorrelay_<version>_arm64.deb`。

## 安装 RPM

```sh
sudo dnf install ./mirrorrelay-<version>.x86_64.rpm
```

arm64 请改用 `sudo dnf install ./mirrorrelay-<version>.aarch64.rpm`。

软件包安装到以下固定路径：

```text
/usr/bin/mirrorrelay
/usr/lib/mirrorrelay/nginx/nginx
/etc/mirrorrelay/config.yaml
/usr/lib/systemd/system/mirrorrelay.service
```

DEB 配置使用 `/etc/ssl/certs/ca-certificates.crt`；RPM 配置在 RHEL 系发行版上使用 `/etc/pki/tls/certs/ca-bundle.crt`。使用便携 tar 包时，必须在启动服务前确认当前发行版的 CA Bundle 路径。

安装脚本会创建 `mirrorrelay` 系统账户和私有的运行时、状态、缓存与日志目录，但不会自动启用或启动服务。请先审核 `/etc/mirrorrelay/config.yaml`，尤其是 `security.admin_cidrs`。软件包默认值只允许回环客户端；如需远程管理，请改为精确的管理网络。然后执行：

```sh
sudo systemctl enable --now mirrorrelay.service
```

HTTPS 入口可用后，打开管理地址。用户数据库为空时，页面会要求一次性注册初始管理员，不再接受预置密码。注册只能通过配置的管理 Host/Path 与允许的管理员 CIDR 访问；请在向不受信任网络开放管理面之前完成注册。除回环开发访问外，管理 Session Cookie 要求使用 HTTPS。随后请继续阅读 [Web UI 使用指南](web-ui.zh-CN.md)。

systemd Unit 会以 `0750` 创建 `/run/mirrorrelay`，并在 MirrorRelay 重启期间保留该目录，使 `stop_on_mirrorrelay_exit` 为 false 时，与版本绑定的 Managed Upstream Nginx 可继续被新进程接管。该目录仍属于临时运行数据，并会随主机启动时的 `/run` 清理而重建。

## 接入 External Shared Nginx

MirrorRelay 只在 `ingress.snippet_path` 下生成供审核的接入片段。软件包不会安装、卸载、修改、Reload 或 Restart External Shared Nginx，也不会占用公网 80/443 端口。

两个本地 Socket 默认权限都是 `0660`，配置会拒绝全局可写的 `0666` 或 `0777`。如需让现有 Nginx Worker 访问 `frontend.sock`，必须显式把其服务用户加入 `mirrorrelay` 组。例如，确认 Worker 用户确实是 `www-data` 后执行：

```sh
sudo usermod -aG mirrorrelay www-data
```

再按照该入口服务原有的维护流程应用组变更与生成的片段。不要对猜测出来的用户执行该命令；MirrorRelay 不会擅自修改现有入口账户。

如果无法使用 Unix Socket，可显式设置 `server.unix_socket_enabled: false` 和/或 `upstream_nginx.upstream_unix_socket_enabled: false`，并通过 `server.local_port`、`upstream_nginx.upstream_local_port` 配置两个不同的回环端口。

## 升级与卸载

升级会替换 MirrorRelay、与其版本绑定的 Managed Upstream Nginx、systemd Unit 和内置文件，但保留 `/etc/mirrorrelay/config.yaml`、`/var/lib/mirrorrelay/mirrorrelay.db`、配置历史和 `/var/cache/mirrorrelay`。

手动触发的开发构建使用 `0.0.1.git.<提交时间戳>.<提交>` 版本，使 DEB 与 RPM 包管理器能够按时间顺序比较快照。已发布 Release 与 Workflow 中显式指定的版本保持原值；直接 Push 不会触发远程构建。

普通 DEB/RPM 卸载也会保留配置、数据库、缓存、证书与审计数据。在 Debian 系系统上，只有显式 purge 才删除这些持久路径：

```sh
sudo apt purge mirrorrelay
```

RPM 不提供隐式 Purge Script。执行 `dnf remove mirrorrelay` 后，只有管理员明确要求不可恢复地清理时，才应逐一核对并删除 `/etc/mirrorrelay`、`/var/lib/mirrorrelay`、`/var/cache/mirrorrelay`、`/var/log/mirrorrelay` 以及专用账户。任何升级或 Purge 前都应备份生产数据。

## 安装 tar 包

便携 tar 包用于手工部署、测试和恢复，包含两个二进制、示例配置、systemd Unit、双语文档、许可证、`BUILD-INFO` 和内部 `SHA256SUMS`：

```sh
tar -xzf mirrorrelay-<version>-linux-amd64.tar.gz
cd mirrorrelay-<version>
sha256sum -c SHA256SUMS
```

arm64 请改为解压 `mirrorrelay-<version>-linux-arm64.tar.gz`。

tar 包不会创建用户、目录或权限。管理员必须按上述软件包布局与属主要求自行安装、部署 systemd Unit，并显式接入 External Shared Nginx。
