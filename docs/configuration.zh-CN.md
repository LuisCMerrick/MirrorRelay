# 配置参考

[English](configuration.md) | [简体中文](configuration.zh-CN.md)

RepoGate 默认读取 `/etc/repogate/config.yaml`。请从 [`configs/config.example.yaml`](../configs/config.example.yaml) 开始。Duration 使用 Go 格式，例如 `15s`、`5m`、`720h`；字节数使用整数。

只有以下 Bootstrap/Runtime 值可由环境变量覆盖：

| 变量 | 含义 |
|---|---|
| `REPOGATE_ADMIN_USERNAME` | 数据库无用户时创建的初始管理员名 |
| `REPOGATE_ADMIN_PASSWORD` | 初始管理员密码；生产首次启动必填 |
| `GOGC` | Go Runtime GC 目标，优先于 YAML |
| `GOMEMLIMIT` | Go Runtime 软内存上限，优先于 YAML |

配置采用严格解析：未知 YAML Key 和多 YAML Document 会被拒绝，避免拼错安全或传输开关后静默回退到默认值。

## Web UI 覆盖

**设置** 页面可以管理下文大部分运行配置，并把一份经过严格验证的设置文档保存在现有 SQLite 数据库中。进程启动时，RepoGate 先加载 YAML，再把保存的 Web UI 值覆盖到对应字段，因此优先级为：

```text
已记录的环境变量 -> Web UI 保存的运行值 -> YAML
```

保存或重置 Web UI 覆盖不会热更新当前进程，必须重启 RepoGate 才能应用。仓库 Desired/Active 变更使用另一条即时验证和激活流程。

Web UI 覆盖端点启用状态与回环端口、入口行为、安全的 HTTP/TLS 配置、性能、Metadata、重定向策略、缓存限制/TTL、运行安全、传输、限流、健康检查、日志、退出及 Managed Upstream Nginx 生命周期。Bootstrap 与信任边界路径仍以 YAML 为准：

```text
server.frontend_socket, server.frontend_socket_mode, runtime.*,
ingress.snippet_path, redirect.pin_validated_ip, tls.certificate,
tls.private_key, database.path,
cache.path, logging.path, admin.*, upstream_nginx.binary,
upstream_nginx.prefix, upstream_nginx.pid, upstream_nginx.log_path,
upstream_nginx.upstream_socket, upstream_nginx.upstream_socket_mode,
upstream_nginx.ca_bundle
```

使用 **重启后恢复 YAML** 删除保存的覆盖值。保存数据无效时，启动会明确失败，不会静默回退到 YAML。

## 本地端点

| 配置 | 默认值 | 说明 |
|---|---:|---|
| `server.unix_socket_enabled` | `true` | External Shared Nginx 通过 Unix Socket 连接 Go |
| `server.frontend_socket` | `/run/repogate/frontend.sock` | 前端 Socket 路径 |
| `server.frontend_socket_mode` | `0660` | 固定要求的 Socket 权限 |
| `server.local_port` | `9081` | 只有显式关闭前端 Unix Socket 后才使用的回环端口 |
| `upstream_nginx.upstream_unix_socket_enabled` | `true` | Go 到 Managed Upstream Nginx 使用 Unix Socket |
| `upstream_nginx.upstream_socket` | `/run/repogate/upstream.sock` | Managed Upstream Nginx Socket 路径 |
| `upstream_nginx.upstream_socket_mode` | `0660` | 固定要求的 Socket 权限 |
| `upstream_nginx.upstream_local_port` | `9082` | 只有显式关闭上游 Unix Socket 后才使用的回环端口 |

TCP 回退端点始终绑定 `127.0.0.1`。如果两个 Socket 都关闭，两个端口必须不同。Unix Socket 失败时不会隐式回退到 TCP。回环 TCP 不具备 Unix 文件属主与权限模式隔离，因此只能在信任本机进程的环境中使用。

## Runtime 与入口

| 配置 | 说明 |
|---|---|
| `runtime.root` | 产品运行状态根目录 |
| `runtime.run_dir` | Socket 与 PID 目录 |
| `ingress.mode` | `external`（默认）或 `managed-standalone` |
| `ingress.generate_snippet` | 成功激活后生成供审核的 External Shared Nginx 接入辅助文件 |
| `ingress.snippet_path` | 目标 `.conf` 文件或目录；目录内生成 `repogate.conf` |
| `http.public_base_url` | 客户端示例与 Metadata 改写使用的 HTTPS Origin（不得包含路径、查询或片段） |
| `http.listen`、`http.https_listen` | 仅 Managed Standalone Ingress 使用 |
| `http.read_timeout`、`http.idle_timeout` | RepoGate HTTP Server 的 Header/请求与 Keepalive 时限 |
| `http.write_timeout` | RepoGate HTTP 写入时限；`0` 表示有意允许长时间流式响应 |
| `tls.certificate`、`tls.private_key`、`tls.min_version` | 仅独立入口使用；最低 TLS 为 `1.2` 或 `1.3` |
| `admin.path` | 仅配置文件可改的后台前缀；默认 `/admin/`，包含 UI 与内嵌 `api/v1/`，修改后必须重启 |

External 模式不测试或绑定公网端口，也绝不会 Reload 共享 External Shared Nginx。生成的 Host Mode 块含证书占位说明，必须由入口管理员补充。

`admin.path` 必须是由安全 URL 路径段组成的绝对路径。RepoGate 会自动补齐末尾 `/`、拒绝系统路径冲突，并且不会在公开仓库索引中显示该值。自定义路径只能降低被发现概率，不能代替身份验证或 CIDR 限制。修改后还应把生成的后台 Location 应用到 External Shared Nginx。

## Managed Upstream Nginx

| 配置 | 说明 |
|---|---|
| `upstream_nginx.mode` | `managed`、高级 `external` 或 `disabled` |
| `upstream_nginx.binary` | 与版本绑定的二进制；软件包默认 `/usr/lib/repogate/nginx/nginx` |
| `upstream_nginx.prefix` | 运行配置根目录；默认 `/var/lib/repogate/runtime/upstream-nginx` |
| `upstream_nginx.pid` | PID 文件；默认 `/run/repogate/upstream-nginx.pid` |
| `upstream_nginx.log_path` | Access/Error Log 目录；默认 `/var/log/repogate/upstream-nginx` |
| `upstream_nginx.ca_bundle`、`upstream_nginx.tls_verify_depth` | 平台 CA Bundle 与上游证书链验证深度；DEB 使用 `/etc/ssl/certs/ca-certificates.crt`，RPM 使用 `/etc/pki/tls/certs/ca-bundle.crt` |
| `upstream_nginx.resolver` | 写入生成配置的 Resolver 地址 |
| `upstream_nginx.resolver_refresh` | 安全 DNS 重新解析/Reconcile 间隔 |
| `upstream_nginx.history_limit` | 保留的不可变配置版本数 |
| `upstream_nginx.restart_*` | Supervisor 失败窗口和指数退避上限 |
| `upstream_nginx.worker_processes`、`upstream_nginx.worker_user`、`upstream_nginx.worker_connections` | Managed Upstream Nginx Worker 设置。仅当 Nginx Master 以 root 运行时才生成 `worker_user` 指令；打包提供的非 root 服务会省略该冗余指令。 |
| `upstream_nginx.stop_on_repogate_exit` | RepoGate 正常退出时是否停止 Managed Upstream Nginx；默认 `false`，便于重启后 Attach 并保持数据面 |

由 RepoGate 启动时，Managed Upstream Nginx 以前台子进程方式受监督，因此可以记录真实退出码或终止信号。RepoGate 重启后可 Attach 到 PID 与 Binary 均匹配的健康 Nginx；如果这个外部 Attach 的进程后来消失，状态会明确说明无法取得其退出码。

所有数据面更新都先生成 Candidate，以配置的 Binary 执行验证，再原子发布并 Graceful Reload。失败的变更停留在 Desired/Failed，不会替换 Go 的 Active 路由快照。这项隔离在 RepoGate 重启后仍然成立：如果失败的 Desired State 仍无法 Reconcile，RepoGate 会验证并恢复最近持久化的 Active 配置，并把其中的仓库快照发布给 Go Router。

## 存储与缓存

| 配置 | 说明 |
|---|---|
| `database.path` | SQLite 数据库路径 |
| `cache.path` | Managed Upstream Nginx Cache 目录 |
| `cache.max_size_bytes`、`cache.max_files` | 容量与观测限制 |
| `cache.inactive` | Nginx 非活动对象回收窗口 |
| `cache.metadata_ttl`、`cache.package_ttl` | 全局类别默认值，可由仓库覆盖 |
| `cache.cleanup_interval` | 物理回收观测间隔 |
| `cache.wait_for_fill` | Cache Lock/Fill 运维窗口 |
| `cache.minimum_free_bytes` | 向运维展示的预留空间设置 |

Purge 会立即改变 Cache Generation。旧物理文件无法再命中，由 Nginx 的 `inactive`/`max_size` 回收；任务会保持 Pending/Running，直到观测窗口结束并重新扫描实际磁盘用量。

## 代理行为与性能

| 配置 | 说明 |
|---|---|
| `performance.stream_buffer_size_bytes` | 固定流式 Buffer：`32768`、`65536` 或 `131072` |
| `performance.go_memory_limit_bytes` | Go 软内存上限；`0` 表示沿用 Runtime/环境 |
| `performance.gogc` | 仅在环境未设置 `GOGC` 时应用 |
| `metadata.rewrite_buffer_limit_bytes` | Metadata Entity 默认缓冲上限 |
| `metadata.output_compression` | `auto`、`identity` 或 `gzip` |
| `metadata.gzip_min_length_bytes` | 改写响应启用 gzip 的最小长度 |
| `metadata.validator_entries` | 内存 Metadata Validator 容量 |
| `redirect.max_hops` | 重定向上限，1–20 |
| `redirect.pin_validated_ip` | 必须保持 `true` |
| `redirect.reject_mixed_dns_result` | 同一 Host 同时解析到允许与禁止地址时整体拒绝 |
| `transport.*` | Go 到 Managed Upstream Nginx 的连接池和响应头超时；不设置固定 Body 总超时 |

## 安全与限制

| 配置 | 说明 |
|---|---|
| `security.allow_http_upstream` | HTTP 上游双重许可中的全局开关 |
| `security.allow_private_upstream` | 私网地址双重许可中的全局开关 |
| `security.expose_client_ip` | 允许按配置向内部转发客户端上下文 |
| `security.admin_cidrs` | 可访问 `admin.path` 下 UI 与内嵌 API 的 CIDR；空表示此层不限制 |
| `security.session_timeout` | 服务端 Session 生命周期 |
| `security.login_window`、`security.login_max_failures` | 按客户端登录限流 |
| `limits.max_total_concurrency` | 全局请求并发，`0` 表示不限 |
| `limits.max_ip_concurrency` | 单客户端并发，`0` 表示不限 |
| `limits.bandwidth_limit_bps` | 全局 Managed Upstream Nginx 上游带宽上限，`0` 表示不限 |

只有直接 Peer 是本地 Unix Socket 或回环 Listener 时，RepoGate 才信任转发的客户端 IP Header。使用私网或 HTTP 上游还必须开启相应仓库开关。上游 TLS 验证不能关闭。

## 健康、日志与退出

| 配置 | 说明 |
|---|---|
| `health.worker_interval` | 通过 Managed Upstream Nginx 执行仓库健康检查的调度间隔 |
| `logging.path` | RepoGate JSON Access Log 目录 |
| `logging.queue_size` | 非阻塞日志队列容量 |
| `logging.max_size_mb` | 单文件轮转阈值 |
| `logging.keep_days` | 已轮转 JSON Access Log 保留天数 |
| `shutdown.grace_period` | 前端 Graceful Drain 最长时间 |

Managed Upstream Nginx Access/Error Log 位于 `upstream_nginx.log_path`，由 RepoGate 按日期/大小轮转，随后通知 Nginx 重新打开日志。RepoGate Access 与 Application JSON Log 异步写入，并使用相同的轮转和保留设置；Audit Event 存储在 SQLite。

## 仓库覆盖项

Web UI 和 Repository API 提供 Profile/版本、路由模式、多上游、Strip/Add Prefix、Host 与请求 Header 改写、连接/读取/发送超时、Cache 类别 TTL、认证响应缓存、Metadata Rewrite Host/缓冲上限、逐仓库 `html_rewrite_enabled` 开关、健康策略、并发/带宽限制、访问策略、Registry Auth/Token/Blob 策略，以及 HTTP/私网许可开关。仓库验证会拒绝根路径、系统路径、后台路径或 `/_repogate/` 冲突、重复或相互重叠的仓库路径、重复公开 Host，以及占用已配置共享 Host 的 Host Mode 仓库。

`html_rewrite_enabled` 默认值为 `false`。为可浏览仓库响应启用后，RepoGate 会相对所选上游页面解析同源 HTML URL。位于有效上游 Base（包含 `add_prefix`）下的 URL 会返回公开仓库 Namespace（包含 `strip_prefix`）；同一 Origin 上的其他路径使用 `/_repogate/upstream/<仓库ID>/`。辅助 Scope 不会许可其他 Origin，并复用仓库原有 Upstream Group 与策略；但它确实扩大了该 Origin 可经 RepoGate 访问的路径范围，因此应把此开关视为显式发布决定。生成的共享入口片段会为 Path Mode 仓库包含必需的辅助 Location。

只有内容身份不随 Authorization 变化且实质公开时，才能启用认证响应缓存。默认情况下，携带 `Authorization` 或 `Cookie` 的请求绕过缓存；配置静态认证 Header 也会关闭缓存。Nginx 默认不会缓存带 `Set-Cookie` 的响应，RepoGate 的普通自定义配置不能覆盖这项内建保护。

普通 Managed Upstream Nginx 自定义片段严格绑定到指定 Context：不能创建 Listener、路由或 Upstream Target，不能更改 TLS 校验、缓存身份或缓存绕过规则，不能访问文件系统或进程环境，也不能引用 RepoGate 保留变量、Zone 和内部 Header。每个候选配置先由 RepoGate 解析，还必须通过随附二进制的 `nginx -t` 才能激活。危险/Hop-by-Hop/内部 Header、无效 Host、配置控制字符和 `insecure_skip_verify` 都会被拒绝。
