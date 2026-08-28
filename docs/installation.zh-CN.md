# 安装

[English](installation.md) | [简体中文](installation.zh-CN.md)

与宿主机深度集成的生产部署推荐使用 DEB 或 RPM。正式 Release 同时提供一个覆盖 `linux/amd64` 和 `linux/arm64` 的 Docker Hub 镜像。每种格式都包含对应发布任务实际测试过的同一个静态链接 Managed Upstream Nginx 二进制。目标主机不会通过系统包管理器安装 Nginx，软件包安装和容器启动时也不会现场编译。

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

对于容器镜像，生产环境应优先使用不可变 Digest 或版本标签，并在部署前检查其多平台 Manifest：

```sh
export DOCKERHUB_USERNAME=<dockerhub-namespace>
docker buildx imagetools inspect \
  "${DOCKERHUB_USERNAME}/mirrorrelay:<version>"
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

## 运行 Docker 镜像

发布工作流会向 Docker Hub 推送以下标签：

- 每个已发布 Release 都有 `<version>` 和 `v<version>`；
- 只有稳定、非预发布 Release 才更新 `latest`。

所有标签都指向同一个 OCI Manifest，其中只有
`linux/amd64` 和 `linux/arm64` 两个应用镜像，另附 SBOM/来源
Attestation。Docker 会自动选择与宿主机匹配的架构。生产发布应
优先使用版本标签或发布工作流记录的不可变 Digest，而不是
`latest`。

镜像以数字 UID/GID `65532` 运行，默认配置位于
`/etc/mirrorrelay/config.yaml`，二进制路径与软件包一致：

```text
/usr/bin/mirrorrelay
/usr/lib/mirrorrelay/nginx/nginx
```

应审核并挂载 `configs/config.docker.yaml`，不要把镜像内的示例值直接当作
生产配置。特别要设置公开 URL、TLS 路径和精确的管理网段。Docker 专用
配置会在容器内显式监听 `0.0.0.0:9081`；只能把该端口发布到宿主机回环，
供管理员维护的 External Shared Nginx 使用，不能暴露受信前端端点。状态、
缓存和日志必须持久化；Runtime 目录默认是容器私有的临时文件系统：

```sh
docker run -d \
  --name mirrorrelay \
  --restart unless-stopped \
  --publish 127.0.0.1:9081:9081 \
  --mount type=bind,src=/absolute/config.yaml,dst=/etc/mirrorrelay/config.yaml,readonly \
  --mount type=volume,src=mirrorrelay-data,dst=/var/lib/mirrorrelay \
  --mount type=volume,src=mirrorrelay-cache,dst=/var/cache/mirrorrelay \
  --mount type=volume,src=mirrorrelay-logs,dst=/var/log/mirrorrelay \
  --tmpfs /run/mirrorrelay:rw,nosuid,nodev,noexec,mode=0770,uid=65532,gid=65532 \
  "${DOCKERHUB_USERNAME}/mirrorrelay:<version>"
```

Compose 文件使用相同的 UID/GID 受限 tmpfs，避免非 root 镜像因宿主机
`/run` 属主不匹配而启动失败。其固定默认 Bridge Gateway 为
`172.31.255.1`，Docker 配置已把该地址加入
`security.trusted_proxy_cidrs`。由于私有 tmpfs Socket 未挂载到宿主机入口，
Docker 配置会关闭 `zero_copy_bypass`。若通过 `MIRRORRELAY_DOCKER_SUBNET` 或
`MIRRORRELAY_DOCKER_GATEWAY` 修改网络，还必须把 Trusted CIDR 更新为新的
精确入口 Peer。

镜像不会安装、修改或重启宿主机 Nginx，也不会改动其服务用户。正常生成的
入口链路连接宿主机 `127.0.0.1:9081`。External Shared Nginx 必须用
`$remote_addr` 覆盖 `X-Real-IP`；MirrorRelay 只接受已配置可信 Peer 提供的
该 Header。若要跨容器边界启用可选零拷贝 Socket，应以为 UID/GID `65532`
显式准备的 Bind Mount 替换 tmpfs，设置 `performance.zero_copy_bypass: true`，
并通过宿主机正常的 Group 或 ACL 管理仅
授权已确认的入口 Worker；不得把 Runtime 目录改成全局可写。若把容器端口
发布到非回环宿主机地址，必须再用网络防火墙限制为可信入口 Peer。

软件包安装到以下固定路径：

```text
/usr/bin/mirrorrelay
/usr/lib/mirrorrelay/nginx/nginx
/etc/mirrorrelay/config.yaml
/usr/lib/systemd/system/mirrorrelay.service
```

DEB 配置使用 `/etc/ssl/certs/ca-certificates.crt`；RPM 配置在 RHEL 系发行版上使用 `/etc/pki/tls/certs/ca-bundle.crt`。使用便携 tar 包时，必须在启动服务前确认当前发行版的 CA Bundle 路径。

安装脚本会创建 `mirrorrelay` 系统账户和私有的运行时、状态、缓存与日志目录，但不会自动启用或启动服务。请先审核 `/etc/mirrorrelay/config.yaml`，尤其是 `security.admin_cidrs` 与 `security.trusted_proxy_cidrs`。软件包默认只允许回环管理客户端并只信任回环入口 Peer；如需远程管理或独立入口主机，请改为精确网段。然后执行：

```sh
sudo systemctl enable --now mirrorrelay.service
```

HTTPS 入口可用后，打开管理地址。用户数据库为空时，页面会要求一次性注册初始管理员，不再接受预置密码。注册只能通过配置的管理 Host/Path 与允许的管理员 CIDR 访问；请在向不受信任网络开放管理面之前完成注册。除回环开发访问外，管理 Session Cookie 要求使用 HTTPS。随后请继续阅读 [Web UI 使用指南](web-ui.zh-CN.md)。

systemd Unit 会以 `0750` 创建 `/run/mirrorrelay`，并在 MirrorRelay 重启期间保留该目录，使 `stop_on_mirrorrelay_exit` 为 false 时，与版本绑定的 Managed Upstream Nginx 可继续被新进程接管。该目录仍属于临时运行数据，并会随主机启动时的 `/run` 清理而重建。

## 接入 External Shared Nginx

MirrorRelay 只在 `ingress.snippet_path` 下生成供审核的接入片段。软件包不会安装、卸载、修改、Reload 或 Restart External Shared Nginx，也不会占用公网 80/443 端口。Go 前端默认监听 `127.0.0.1:9081`；监听 IP 与端口分别通过 `server.local_address`、`server.local_port` 配置。

Go 到 Managed Upstream Nginx 的 Unix Socket 默认保持启用；Go 授权请求后，零拷贝旁路也会让 External Shared Nginx 使用该私有端点。如需让现有 Nginx Worker 访问 `upstream.sock`，或在显式启用前端 Unix Socket 后访问 `frontend.sock`，必须把已确认的服务用户加入 `mirrorrelay` 组。例如，确认 Worker 用户确实是 `www-data` 后执行：

```sh
sudo usermod -aG mirrorrelay www-data
```

再按照该入口服务原有的维护流程应用组变更与生成的片段。不要对猜测出来的用户执行该命令；MirrorRelay 不会擅自修改现有入口账户。

只有需要用 `frontend.sock` 替换默认前端 TCP Listener 时，才设置 `server.unix_socket_enabled: true`。任何启用的 Unix Socket 都必须使用 `0660`，配置会拒绝全局可写的 `0666` 或 `0777`。如需用回环 TCP 替换默认的 Go 到 Managed Upstream Nginx Unix Socket，应显式设置 `upstream_nginx.upstream_unix_socket_enabled: false`，并选择与前端不同的 `upstream_nginx.upstream_local_port`。

## 升级与卸载

升级会替换 MirrorRelay、与其版本绑定的 Managed Upstream Nginx、systemd Unit 和内置文件，但保留 `/etc/mirrorrelay/config.yaml`、`/var/lib/mirrorrelay/mirrorrelay.db`、配置历史和 `/var/cache/mirrorrelay`。

重新发布的 v0.0.20 还会在内存中迁移旧 Release 保存的 Web UI 设置：旧记录缺少的字段继承当前 YAML/默认值，旧版数字形式的预热 Timeout 会被规范化。升级时不要 Purge 或重建实例。如果已经安装原始 v0.0.20 软件包，请使用包管理器的同版本 Reinstall/Replace 操作直接覆盖安装重新发布的软件包；持久状态会保留在原位。

手动触发的开发构建使用 `0.0.1.git.<提交时间戳>.<提交>` 版本，使 DEB 与 RPM 包管理器能够按时间顺序比较快照。已发布 Release 与 Workflow 中显式指定的版本保持原值；直接 Push 不会触发远程 Release 构建。手动运行 Release Build 默认不发布容器；只有显式选择 `publish_container` 才会推送不可变的开发版本标签和 `edge`，并保持稳定 Release 的 `latest` 标签不变。

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
