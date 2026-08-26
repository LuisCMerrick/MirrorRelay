# 配置参考

[English](configuration.md) | [简体中文](configuration.zh-CN.md)

MirrorRelay 默认读取 `/etc/mirrorrelay/config.yaml`。请从 [`configs/config.example.yaml`](../configs/config.example.yaml) 开始。Duration 使用 Go 格式，例如 `15s`、`5m`、`720h`；字节数使用整数。

只有以下部署/运行值可由环境变量覆盖：

| 变量 | 含义 |
|---|---|
| `MIRRORRELAY_ADMIN_HOST` | 独立管理 Hostname |
| `MIRRORRELAY_DISTRIBUTED_ENABLED` | 分布式模式 Boolean 覆盖值 |
| `MIRRORRELAY_DISTRIBUTED_ROLE` | `standalone`、`coordinator` 或 `edge`；Coordinator/Edge 同时启用分布式模式 |
| `MIRRORRELAY_DISTRIBUTED_TOKEN` | 集群共享的只读探测凭据 |
| `MIRRORRELAY_DISTRIBUTED_MUTATION_TOKEN` | 当前 Edge 独立的同步/Purge 凭据 |
| `MIRRORRELAY_DISTRIBUTED_MUTATION_TOKEN_KEY_FILES` | Coordinator 加密密钥文件的操作系统路径列表；首个密钥用于加密，后续密钥仅在轮换期间用于解密 |
| `MIRRORRELAY_COORDINATOR_ID` | 当前 Edge 信任的 Coordinator 身份 |
| `MIRRORRELAY_NODE_NAME` | 分布式节点标识 |
| `MIRRORRELAY_NODE_PUBLIC_BASE_URL` | Edge 对外重定向 Base URL |
| `MIRRORRELAY_NODE_REGION` | 分布式节点 Region |
| `GOGC` | Go Runtime GC 目标，优先于 YAML |
| `GOMEMLIMIT` | Go Runtime 软内存上限，优先于 YAML |

配置采用严格解析：未知 YAML Key 和多 YAML Document 会被拒绝，避免拼错安全或传输开关后静默回退到默认值。

## Web UI 覆盖与配置管理

**设置** 页面提供对 `config.yaml` 全部 20 个配置块的完整管理能力。设置修改经过启动级严格验证并持久化于 SQLite 数据库中，同时维护不可变版本历史以供审计与一键回滚。进程启动时，MirrorRelay 先加载 YAML，再把保存的 Web UI 值覆盖到对应字段，因此优先级为：

```text
已记录的环境变量 -> Web UI 保存的运行值 -> YAML
```

保存、导入或重置 Web UI 覆盖不会热更新当前进程，必须重启 MirrorRelay 才能应用。仓库 Desired/Active 变更使用另一条即时验证和激活流程。

可使用 **导出配置** 下载标准或完整备份 YAML，使用 **导入配置** 上传/粘贴 YAML 文本并进行严格校验与 Diff 差异预览。使用 **重启后恢复 YAML** 删除保存的覆盖值。保存数据无效时，启动会明确失败，不会静默回退到 YAML。

## 本地端点

| 配置 | 默认值 | 说明 |
|---|---:|---|
| `server.unix_socket_enabled` | `false` | 显式以 Unix Socket 替换默认的前端 TCP Listener |
| `server.frontend_socket` | `/run/mirrorrelay/frontend.sock` | 前端 Socket 路径 |
| `server.frontend_socket_mode` | `0660` | 固定要求的 Socket 权限 |
| `server.local_address` | `127.0.0.1` | 前端 Unix Socket 关闭时使用的 TCP 监听 IP |
| `server.local_port` | `9081` | 前端 Unix Socket 关闭时使用的 TCP 监听端口 |
| `upstream_nginx.upstream_unix_socket_enabled` | `true` | Go 到 Managed Upstream Nginx 默认使用 Unix Socket；显式设为 `false` 才改用 TCP |
| `upstream_nginx.upstream_socket` | `/run/mirrorrelay/upstream.sock` | Managed Upstream Nginx Socket 路径 |
| `upstream_nginx.upstream_socket_mode` | `0660` | 固定要求的 Socket 权限 |
| `upstream_nginx.upstream_local_port` | `9082` | 只有显式关闭上游 Unix Socket 后才使用的回环端口 |

External Shared Nginx 默认通过 `127.0.0.1:9081` 连接 Go 前端。只有需要入口改用 `server.frontend_socket` 时，才设置 `server.unix_socket_enabled: true`。`server.local_address` 接受明确的 IPv4 或 IPv6 监听地址；`0.0.0.0` 与 `::` 是有效的显式通配绑定，生成的同宿主机 Nginx 配置会使用相应回环地址连接。通配或非回环绑定会扩大受信入口范围，必须通过防火墙保护；Docker 中只能把容器端口发布到宿主机回环，绝不能直接暴露给不受信任网络。

Go 默认通过 Unix Socket 连接 Managed Upstream Nginx。只有显式设置 `upstream_nginx.upstream_unix_socket_enabled: false` 才会选择固定的 `127.0.0.1` 上游 TCP 端点。当前端绑定覆盖回环且两段连接均使用 TCP 时，两个本地端口必须不同。Unix Socket 失败后不会隐式回退；任何已启用 Socket 都必须使用 `0660`。

## Runtime 与入口

| 配置 | 说明 |
|---|---|
| `runtime.root` | 产品运行状态根目录 |
| `runtime.run_dir` | Socket 与 PID 目录 |
| `ingress.mode` | `external`（默认）或 `managed-standalone` |
| `ingress.generate_snippet` | 成功激活后生成供审核的 External Shared Nginx 接入辅助文件 |
| `ingress.snippet_path` | 目标 `.conf` 文件或目录；目录内生成 `mirrorrelay.conf` |
| `http.public_base_url` | 客户端示例与 Metadata 改写使用的 HTTPS Origin（不得包含路径、查询或片段） |
| `http.listen`、`http.https_listen` | 仅 Managed Standalone Ingress 使用 |
| `http.read_timeout`、`http.idle_timeout` | MirrorRelay HTTP Server 的 Header/请求与 Keepalive 时限 |
| `http.write_timeout` | MirrorRelay HTTP 写入时限；`0` 表示有意允许长时间流式响应 |
| `tls.certificate`、`tls.private_key`、`tls.min_version` | 仅独立入口使用；最低 TLS 为 `1.2` 或 `1.3` |
| `admin.path` | 仅配置文件可改的后台前缀；默认 `/admin/`，包含 UI 与内嵌 `api/v1/`，修改后必须重启 |

External 模式不测试或绑定公网端口，也绝不会 Reload 共享 External Shared Nginx。生成的 Host Mode 块含证书占位说明，必须由入口管理员补充。

`admin.path` 必须是由安全 URL 路径段组成的绝对路径。MirrorRelay 会自动补齐末尾 `/`、拒绝系统路径冲突，并且不会在公开仓库索引中显示该值。自定义路径只能降低被发现概率，不能代替身份验证或 CIDR 限制。修改后还应把生成的后台 Location 应用到 External Shared Nginx。

## Managed Upstream Nginx

| 配置 | 说明 |
|---|---|
| `upstream_nginx.mode` | `managed`、高级 `external` 或 `disabled` |
| `upstream_nginx.binary` | 与版本绑定的二进制；软件包默认 `/usr/lib/mirrorrelay/nginx/nginx` |
| `upstream_nginx.prefix` | 运行配置根目录；默认 `/var/lib/mirrorrelay/runtime/upstream-nginx` |
| `upstream_nginx.pid` | PID 文件；默认 `/run/mirrorrelay/upstream-nginx.pid` |
| `upstream_nginx.log_path` | Access/Error Log 目录；默认 `/var/log/mirrorrelay/upstream-nginx` |
| `upstream_nginx.ca_bundle`、`upstream_nginx.tls_verify_depth` | 平台 CA Bundle 与上游证书链验证深度；DEB 使用 `/etc/ssl/certs/ca-certificates.crt`，RPM 使用 `/etc/pki/tls/certs/ca-bundle.crt` |
| `upstream_nginx.resolver` | 写入生成配置的 Resolver 地址 |
| `upstream_nginx.resolver_refresh` | 安全 DNS 重新解析/Reconcile 间隔 |
| `upstream_nginx.history_limit` | 保留的不可变配置版本数 |
| `upstream_nginx.restart_*` | Supervisor 失败窗口和指数退避上限 |
| `upstream_nginx.worker_processes`、`upstream_nginx.worker_user`、`upstream_nginx.worker_connections` | Managed Upstream Nginx Worker 设置。仅当 Nginx Master 以 root 运行时才生成 `worker_user` 指令；打包提供的非 root 服务会省略该冗余指令。 |
| `upstream_nginx.stop_on_mirrorrelay_exit` | MirrorRelay 正常退出时是否停止 Managed Upstream Nginx；默认 `false`，便于重启后 Attach 并保持数据面 |

由 MirrorRelay 启动时，Managed Upstream Nginx 以前台子进程方式受监督，因此可以记录真实退出码或终止信号。MirrorRelay 重启后可 Attach 到 PID 与 Binary 均匹配的健康 Nginx；如果这个外部 Attach 的进程后来消失，状态会明确说明无法取得其退出码。

所有数据面更新都先生成 Candidate，以配置的 Binary 执行验证，再原子发布并 Graceful Reload。失败的变更停留在 Desired/Failed，不会替换 Go 的 Active 路由快照。这项隔离在 MirrorRelay 重启后仍然成立：如果失败的 Desired State 仍无法 Reconcile，MirrorRelay 会验证并恢复最近持久化的 Active 配置，并把其中的仓库快照发布给 Go Router。

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
| `performance.zero_copy_bypass` | Go 完成授权后允许 External Shared Nginx 使用私有 Managed Upstream Nginx Socket；宿主机软件包默认 `true`，使用容器私有 Runtime tmpfs 的 Docker 示例显式设为 `false` |
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

## 智能主动预热与热点预取

| 配置 | 说明 |
|---|---|
| `warmup.enabled` | 启用智能主动预热与热点预取引擎（默认 `false` 关闭） |
| `warmup.max_concurrency` | 最大预热并发下载协程数（默认 `4`） |
| `warmup.bandwidth_limit_bps` | 预热下载带宽限制速率（字节/秒，默认 `0` 不限速） |
| `warmup.timeout` | 单个预热任务执行超时时间（默认 `30m`） |
| `warmup.retry_count` | 预热对象失败重试次数（默认 `2`） |
| `warmup.metadata_depth` | 元数据软件包递归解析深度（默认 `1` 自动提取 APT/RPM/PyPI 元数据中的软件包并预热） |

预热任务接受按 UTC 求值的五字段数字 Cron 表达式，或 `@hourly`、`@daily`、`@every <duration>`（最短间隔 `30s`）。数字字段支持 `*`、列表、范围和步长。MirrorRelay 在创建/更新时校验表达式，持久化计算出的 `next_run_at`，绝不会把非法或未知表达式作为兜底任务反复执行。由 Metadata 发现的软件包 URL 会复用已配置的 Frontend 端点；前端 Unix Socket 关闭时会保留 `server.local_address` 与 `server.local_port`。

## Webhook 投递

| 配置 | 说明 |
|---|---|
| `webhook.enabled` | 启用异步事件投递 |
| `webhook.url` | 单个生效目标 URL；默认强制 HTTPS，并根据主机名自动识别平台消息格式 |
| `webhook.secret` | 可选 HMAC-SHA256 签名密钥；非管理员读取设置时会同时隐藏密钥与目标 URL |
| `webhook.events` | 需要投递的事件名称；空列表表示全部事件 |
| `webhook.timeout` | 请求、TLS 握手与响应头超时 |
| `webhook.allow_http` | 独立的明文 HTTP 显式许可；默认 `false` |
| `webhook.allow_private` | 独立的私网、环回与链路本地地址显式许可；默认 `false` |

MirrorRelay 会把每个事件投递到一个已配置的 Webhook 目标；钉钉、飞书/Lark、企业微信和 Slack 主机使用各自的平台消息格式，其他主机使用标准 MirrorRelay JSON。Webhook 目标及每次重定向都会在连接前执行结构校验、DNS 解析与网段过滤；网络连接只使用通过策略的地址，并保留 TLS 主机名验证。测试可使用当前运行中目标，也可使用一个经过校验的临时 URL/Secret；临时 URL 绝不会继承运行中目标的密钥，非法测试 JSON 不会产生任何投递副作用。

## 安全与限制

| 配置 | 说明 |
|---|---|
| `security.allow_http_upstream` | HTTP 上游双重许可中的全局开关 |
| `security.allow_private_upstream` | 私网地址双重许可中的全局开关 |
| `security.expose_client_ip` | 在内部请求链路转发经过校验的客户端地址；默认 `false` |
| `security.trusted_proxy_cidrs` | 允许提供 `X-Real-IP` 的 TCP Peer CIDR；默认仅 IPv4/IPv6 回环，空列表表示不信任任何 TCP Peer |
| `security.admin_cidrs` | 允许访问 `admin.path` 下 Web UI 与管理 API 的 CIDR 列表；为空表示本层不作限制 |
| `security.session_timeout` | 服务端 Session 会话生命周期 |
| `security.login_window`、`security.login_max_failures` | 登录频次限制窗口与最大失败次数 |
| `admin.host` | 管理控制台与指标端点的专用独立域名隔离 |
| `admin.path` | 管理控制台与 REST API 的基础路径前缀 |
| `limits.max_total_concurrency` | 全局并发请求上限；`0` 表示无限制 |
| `limits.max_ip_concurrency` | 单客户端并发请求上限；`0` 表示无限制 |
| `limits.bandwidth_limit_bps` | 全局 Managed Upstream Nginx 上游带宽上限；`0` 表示无限制 |

MirrorRelay 默认使用 TCP Peer 地址；只有 Peer 命中 `security.trusted_proxy_cidrs` 时，合法的 `X-Real-IP` 才能替换该地址。显式启用且受文件权限保护的前端 Unix Socket 视为可信入口 Peer。External Shared Nginx 必须覆盖客户端传入的 `X-Real-IP`，不得追加或透传；生成片段使用 `$remote_addr` 完成覆盖。MirrorRelay 生成公开链接时不信任 `X-Forwarded-Proto`：优先使用 `http.public_base_url`，否则推导出的公开 Origin 一律使用 HTTPS。应保持默认回环绑定、使用私有 Unix Socket，或在配置其他监听 IP 时建立等效的容器/防火墙边界。使用私网或 HTTP 上游还必须开启相应仓库开关。上游 TLS 验证不能关闭。

## 健康、日志与退出

| 配置 | 说明 |
|---|---|
| `health.worker_interval` | 通过 Managed Upstream Nginx 执行仓库健康检查的调度间隔 |
| `logging.path` | MirrorRelay JSON Access Log 目录 |
| `logging.queue_size` | 非阻塞日志队列容量 |
| `logging.max_size_mb` | 单文件轮转阈值 |
| `logging.keep_days` | 已轮转 JSON Access Log 保留天数 |
| `shutdown.grace_period` | 前端 Graceful Drain 最长时间 |

Managed Upstream Nginx Access/Error Log 位于 `upstream_nginx.log_path`，由 MirrorRelay 按日期/大小轮转，随后通知 Nginx 重新打开日志。MirrorRelay Access 与 Application JSON Log 异步写入，并使用相同的轮转和保留设置；Audit Event 存储在 SQLite。

## 界面增强与外观设置

MirrorRelay 提供可选的界面主题增强、颜色定制、统一仓库目录浏览与客户端使用帮助文档。

| 配置项 | 说明 |
|---|---|
| `ui_enhancement.enabled` | 公开仓库界面增强开关（默认 `false`）。为 `false` 时不改写或重设上游目录响应样式；管理界面主题切换仍然可用。 |
| `ui_enhancement.theme` | 主题模式：`system`（默认跟随系统）、`light`（浅色明亮）或 `dark`（深色暗黑） |
| `ui_enhancement.accent_color` | 主色调十六进制颜色代码（如 `#2563eb`） |
| `ui_enhancement.branding.title` | 自定义站点/实例标题（默认 `MirrorRelay`） |
| `ui_enhancement.branding.logo` | 自定义 Logo 图片 URL |
| `ui_enhancement.branding.favicon` | 自定义 Favicon 图标 URL |
| `ui_enhancement.login.title` | 登录页主标题 |
| `ui_enhancement.login.subtitle` | 登录页副标题 |
| `ui_enhancement.custom_css.enabled` | 启用自定义 CSS 注入 |
| `ui_enhancement.custom_css.file` | 自定义 CSS 文件路径（通过 `GET /ui/custom.css` 提供） |
| `ui_enhancement.repository_browser.enabled` | 启用现代自适应仓库目录浏览器（界面增强启用时默认 `true`） |

管理界面的登录页和顶栏还提供浏览器本地的“亮色 / 暗色 / 自动”切换。“自动”会跟随 `prefers-color-scheme`；浏览器保存的偏好会覆盖实例默认值，直到用户在本地再次修改。

> **安全模式 (Safe Mode)**：在任何目录或帮助页面 URL 后添加 `?safe-ui=1` 参数即可绕过所有界面增强与脚本，直接回退并显示上游原始 HTML 响应。

## 客户端配置帮助 (Help)

MirrorRelay 提供常见 Linux 发行版与软件包管理器的开箱即用交互式客户端配置指南（如 Debian、Ubuntu、Rocky Linux、AlmaLinux、Fedora、EPEL、Alpine、PyPI、npm、Docker CE、OpenWrt 等）。

- 公开帮助概览路由：`GET /help/`
- 交互式仓库帮助路由：`GET /help/<slug>/`
- 敏感信息脱敏：帮助文档中引用的上游 URL 会自动过滤凭证与查询参数。

## 仓库覆盖项

Web UI 和 Repository API 提供 Profile/版本、路由模式、多上游、Strip/Add Prefix、Host 与请求 Header 改写、连接/读取/发送超时、Cache 类别 TTL、认证响应缓存、Metadata Rewrite Host/缓冲上限、逐仓库 `html_rewrite_enabled` 开关、健康策略、并发/带宽限制、访问策略、客户端帮助配置（`help.enabled`、`help.template`、`help.title`、`help.summary`）、Registry Auth/Token/Blob 策略，以及 HTTP/私网许可开关。仓库验证会拒绝根路径、系统路径、后台路径或 `/_mirrorrelay/` 冲突、重复或相互重叠的仓库路径、重复公开 Host，以及占用已配置共享 Host 的 Host Mode 仓库。

`blocked_packages` 与 `allowed_packages` 各自最多接受 128 条规则，每条最多 512 字节。规则必须能够解析为 Go Glob 或 RE2 正则表达式；仓库 Candidate 校验及 Active 路由快照构建阶段都会编译规则。非法 Candidate 会被拒绝；若持久化状态意外包含非法规则，请求会按 Fail-Closed 策略拒绝，而不会静默跳过策略。

`html_rewrite_enabled` 默认值为 `false`。为可浏览仓库响应启用后，MirrorRelay 会相对所选上游页面解析同源 HTML URL。位于有效上游 Base（包含 `add_prefix`）下的 URL 会返回公开仓库 Namespace（包含 `strip_prefix`）；同源 Base 外路径会获得不透明的 `/_mirrorrelay/upstream/<仓库ID>/<上游ID>/<签名>/<目标>` URL。HMAC 覆盖仓库、生成页面时实际选中的上游/Host 策略、转义后的目标路径和 Query，因此客户端不能替换上游、根路径或 Query；只有 MirrorRelay 生成的目标会被接受，跨 Origin URL 保持不变。请求仍遵守仓库访问策略，并通过 Managed Upstream Nginx 复用 Pin 地址、TLS 校验、缓存策略与限制。生成的共享入口片段会为 Path Mode 仓库包含必需的辅助 Location。

只有内容身份不随 Authorization 变化且实质公开时，才能启用认证响应缓存。默认情况下，携带 `Authorization` 或 `Cookie` 的请求绕过缓存；配置静态认证 Header 也会关闭缓存。Nginx 默认不会缓存带 `Set-Cookie` 的响应，MirrorRelay 的普通自定义配置不能覆盖这项内建保护。

普通 Managed Upstream Nginx 自定义片段严格绑定到指定 Context：不能创建 Listener、路由或 Upstream Target，不能更改 TLS 校验、缓存身份或缓存绕过规则，不能访问文件系统或进程环境，也不能引用 MirrorRelay 保留变量、Zone 和内部 Header。每个候选配置先由 MirrorRelay 解析，还必须通过随附二进制的 `nginx -t` 才能激活。危险/Hop-by-Hop/内部 Header、无效 Host、配置控制字符和 `insecure_skip_verify` 都会被拒绝。

## 分布式部署

MirrorRelay 支持由一个协调器（Coordinator）与多个边缘节点（Edge）组成的分布式集群。

```text
客户端 -> 协调器 (Coordinator) -> (HTTP 307 临时重定向) -> 边缘节点 (Edge) -> Managed Upstream Nginx -> 源站
```

| 配置 | 说明 |
|---|---|
| `distributed.enabled` | 全局分布式模式开关 |
| `distributed.role` | 节点角色：`standalone`（默认单机）、`coordinator`（协调器）或 `edge`（边缘节点） |
| `distributed.token` | Manifest 与 Health 探针必填的共享只读凭据 |
| `distributed.mutation_token` | Edge 专用的同步/Purge 凭据；不得等于探测凭据，也不得与其他 Edge 共用 |
| `distributed.mutation_token_key_files` | Coordinator 用于加密逐 Edge Mutation Token 的有序 AES-256 密钥环；至少需要一个绝对路径 |
| `distributed.coordinator_id` | Edge 在 Protocol v2 变更 Envelope 中接受的 Coordinator 身份 |
| `distributed.allow_http` | 集群节点 Origin 使用明文 HTTP 的独立显式许可；默认 `false` |
| `distributed.node.name` | 节点唯一标识；Coordinator/Edge 角色必填并写入 Manifest |
| `distributed.node.public_base_url` | 协调器向客户端重定向时使用的边缘节点公开 Base URL |
| `distributed.node.region` | 节点所在地域标识（用于 Geo/CIDR 调度） |
| `distributed.node.country` | ISO 3166-1 alpha-2 国家/地区代码 |
| `distributed.routing.mode` | 路由调度模式：`hybrid`（默认混合模式）、`cidr`、`geo` 或 `priority` |
| `distributed.routing.client_networks` | CIDR 网段到地域的映射规则 |
| `distributed.routing.regions` | 国家到地域的映射定义 |
| `distributed.health_check.interval` | 协调器对边缘节点的健康检查与配置一致性探测间隔 |
| `distributed.health_check.timeout` | 探测请求超时时间 |
| `distributed.health_check.healthy_threshold` | 判定节点转为健康的连续成功次数 |
| `distributed.health_check.unhealthy_threshold` | 判定节点转为异常的连续失败次数 |
| `distributed.nodes` | Coordinator 启动时的初始 Edge Seed；每项必须提供独立 `mutation_token` |

### 分布式不变量

- **数据面隔离**：协调器永不代为拉取源站或边缘节点的包文件；它始终返回保留原始请求路径与 Query 字符串的 `HTTP 307 临时重定向`。
- **配置一致性与漂移检测**：Coordinator 从本机完整 Active 仓库与自定义 Managed Upstream Nginx 配置计算权威指纹；Edge 回执不能初始化或改写权威值。Protocol、Coordinator 身份/Epoch、配置 Generation 与指纹必须一致，且目标仓库必须明确为健康。
- **防重放**：Edge 持久化已接受的 Coordinator 身份、Epoch、Generation 与指纹；旧 Generation、冲突及已退役 Epoch 会被拒绝。同步/Purge 使用与共享探测凭据分离的逐 Edge 变更凭据。
- **安全控制面**：集群节点 URL 必须是不含凭据、路径、查询或片段的绝对 Origin。默认强制 HTTPS；`distributed.allow_http` 与全局私网地址策略是两个独立的显式决定。
- **容器镜像仓库限制**：V1 阶段分布式调度明确不支持 Docker / OCI Registry（返回 HTTP 501）。
