# 安全模型与规范

[English](security.md) | [简体中文](security.zh-CN.md)

MirrorRelay 从架构底层贯彻零信任网络边界、严格的上游源隔离以及针对服务端请求伪造（SSRF）与凭据泄露的深度防御体系。

---

## 1. 核心安全不变式

1. **专用数据平面物理隔离**：Go 控制平面绝不直接发起对外部上游软件包服务器的 HTTP 连接，所有上游流量均由静态链接的 `Managed Upstream Nginx` 独立进程代理处理。
2. **严格 SSRF 深度防御**：所有上游源地址与重定向跳转目标均需解析并匹配私有网络、环回地址、链路本地地址及云元数据 CIDR 黑名单。
3. **禁止绕过 TLS 证书链校验**：上游 HTTPS 连接始终强制执行严格的证书链与主机名验证（`proxy_ssl_verify on`），系统拒绝任何不安全证书绕过选项。
4. **内部 Header 绝对净化**：客户端请求中携带的 `X-Mirror-Internal-*` 前缀请求头一律在代理前被强制剥离，杜绝内部路由上下文伪造。
5. **最小权限原则**：系统以非特权专属账户 `mirrorrelay:mirrorrelay` 运行，并在 systemd 服务单元中开启全方位沙箱加固。

---

## 2. SSRF 防护与 IP 地址锁定

当系统配置仓库上游或处理重定向时：

1. **协议与端口合规性校验**：仅放行 `http`（端口 80、8080）与 `https`（端口 443、8443）及合法的主机名。
2. **DNS 解析与网段过滤**：主机名解析为数值 IP 后，若命中以下保留与私有网段且未开启 `allow_private_upstream`，请求将被立即拦截：
   - IPv4: `0.0.0.0/8`, `10.0.0.0/8`, `100.64.0.0/10`, `127.0.0.0/8`, `169.254.0.0/16`, `172.16.0.0/12`, `192.0.0.0/24`, `192.0.2.0/24`, `192.168.0.0/16`, `198.18.0.0/15`, `198.51.100.0/24`, `203.0.113.0/24`, `224.0.0.0/4`, `240.0.0.0/4`
   - IPv6: `::/128`, `::1/128`, `fc00::/7`, `fe80::/10`, `ff00::/8`, `::ffff:0:0/96`
3. **IP 锁定与 SNI 证书保留**：底层网络拨号直接绑定已验证的数值 IP，彻底规避 DNS 重绑定（DNS Rebinding）攻击，同时完整保留原始主机名以供 TLS SNI 和证书链验证。

---

## 3. 身份认证与会话管理

- **首次注册**：系统不提供默认管理员密码，也不通过环境变量预置管理员。用户表为空时，只允许通过配置的管理 Host/Path 与 CIDR 边界注册一次初始 Admin；数据库原子条件保证并发请求中只能有一个成功。
- **密码哈希算法**：管理员密码采用 **Argon2id**（内存：64 MB，迭代轮数：3，最多 4 个线程）高强度哈希算法。
- **并发防 DoS 节流**：密码校验由并发信号量保护，防止通过大量并发登录请求耗尽 CPU 资源。
- **高熵会话令牌**：采用 256 位密码学安全随机数（`crypto/rand`），在 SQLite 数据库中以 SHA-256 哈希值持久化（明文令牌绝不落盘）。
- **Cookie 安全属性**：会话 Cookie 强制开启 `HttpOnly`、`Secure` 及 `SameSite=Strict`。
- **CSRF 双重防护**：所有已认证的状态修改 API（`POST`、`PUT`、`DELETE`）必须携带有效的 `X-CSRF-Token`，并在服务端通过常量时间比较校验。未认证的一次性初始注册请求改由空数据库条件、原子插入与管理网络边界共同约束。
- **可信入口身份**：TCP 请求只有在直接 Peer 命中 `security.trusted_proxy_cidrs` 时才可采用 `X-Real-IP`，否则以 Socket Peer 作为客户端身份。显式启用的前端 Unix Socket 依靠 `0660` 权限边界受信。生成的入口配置使用 `$remote_addr` 覆盖 `X-Real-IP`；仓库重写与公开帮助页 URL 生成都会忽略 `X-Forwarded-Proto`、校验回退使用的请求 Authority，并始终保持 HTTPS。
- **集群变更凭据加密**：Coordinator 节点 Mutation Token 使用 AES-256-GCM 认证密文落库。有序文件密钥环支持启动迁移与轮换；密钥缺失或凭据无法解密时会停止启动，绝不会回退为明文。

---

## 4. 输入验证与注入防御

- **SQL 注入防御**：`internal/database/store.go` 中 100% 的 SQLite 查询均采用参数化 `?` 占位符，并强制启用 `PRAGMA foreign_keys = ON`。
- **路径穿越防御**：仓库路径严格过滤 NUL 空字符、回车换行、反斜杠、目录跳转符（`.` / `..`）及 URL 编码分隔符（`%2f`、`%5c`）。缓存文件路径采用清洗后标准路径的 SHA-256 哈希值作为落盘键名。
- **HTML 辅助路由签名**：仓库 Base 外的同源目标只能使用 HMAC Scope URL，签名绑定仓库、实际选中的上游/Host 策略、转义路径与 Query。客户端修改上游、路径或 Query 时，请求会在到达 Managed Upstream Nginx 前被拒绝。
- **JSON 严格反序列化**：管理 API 解析 JSON 请求时强制开启 `DisallowUnknownFields()` 并使用 `http.MaxBytesReader` 限制请求体上限为 1 MB。

---

## 5. 系统加固与沙箱规范

官方 `mirrorrelay.service` 服务单元配置了严格的 Linux 命名空间与沙箱隔离策略：

```ini
[Service]
User=mirrorrelay
Group=mirrorrelay
UMask=0007
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
RestrictNamespaces=true
LockPersonality=true
CapabilityBoundingSet=
```

标准文件系统权限规范：
- `/usr/bin/mirrorrelay` 与 `/usr/lib/mirrorrelay/nginx/nginx`：`0755 root:root`
- `/etc/mirrorrelay/config.yaml`：`0640 root:mirrorrelay`
- Coordinator 启用时的 `/etc/mirrorrelay/cluster-mutation-token.key`：`0640 root:mirrorrelay`
- `/var/lib/mirrorrelay/`：`0750 mirrorrelay:mirrorrelay`
- `/var/cache/mirrorrelay/`：`0750 mirrorrelay:mirrorrelay`
- `/run/mirrorrelay/*.sock`：`0660 root:mirrorrelay`

---

## 6. 软件供应链安全与包名黑白名单防御（Package Name Guard）

MirrorRelay 内置企业级供应链投毒与依赖混淆（Dependency Confusion）主动防御机制：
- **包名黑名单拦截（`blocked_packages`）**：支持正则表达式与 Glob 通配符（如 `^malicious-.*`、`bad-pkg-*.tar.gz`），实时阻断受污染恶意包的拉取。
- **包名白名单准入（`allowed_packages`）**：支持企业安全准入限定（如 `^internal-.*`），仅允许经合规审计的包通过。
- 每个列表最多 128 条规则，每条最多 512 字节。规则必须是合法的 Go Glob 或 RE2 表达式，并在激活前完成编译；非法 Candidate 会被拒绝，Active 状态意外无效时按 Fail-Closed 处理。
- 当客户端请求命中拦截规则时，MirrorRelay 将立即终止代理并返回 HTTP `403 Forbidden`，同时向系统审计日志与 Webhook 发送安全拦截警报。
- 开发镜像构建时会用 `nginx.sha256` 校验仓库内的 Managed Upstream Nginx Fixture。正式镜像会根据 Package Job 的 `BUILD-INFO` 校验两个架构匹配二进制；发布包同时携带 `BUILD-INFO` 与内部校验和，已发布 OCI Manifest 还附带 SBOM 与 Provenance Attestation。

---

## 7. 基于角色的权限访问控制（RBAC）

MirrorRelay 支持三级权限隔离体系：
- **`admin`（超级管理员）**：拥有完整运维控制权，包括仓库凭据查看/轮换、自定义 Managed Upstream Nginx 片段、集群节点记录、用户、系统级设置、服务重启及 Webhook 联通性测试。
- **`operator`（运维管理员）**：可管理仓库非敏感字段、清理缓存、触发健康检查、执行集群检查/同步及 Nginx Reload/回滚。仓库静态 Header 值与 Token Endpoint 始终脱敏，编辑时只能原样保留。Operator 不能读取生成/生效/自定义 Nginx 配置，不能管理节点凭据、用户或系统级设置。
- **`viewer`（只读审计员）**：只能读取指标、Audit Log、已脱敏仓库详情和健康状态；不能读取 System 端点、Managed Upstream Nginx Access Log 或生成/生效/自定义配置。一切写操作均被拒绝（HTTP 403）。

所有非 Admin 仓库响应都会把静态 Header 值与 Token Endpoint 替换为 Sentinel；畸形 URL 或旧数据中可能存在的 Query/Userinfo 凭据采用失败闭合脱敏。手动仓库检查响应使用相同的 URL 脱敏，并把原始连接错误替换为通用失败信息。只有 Admin 能通过普通仓库编辑器新增、删除或轮换这些可能携带凭据的字段。仓库配置凭据后，也只有 Admin 能修改其 Upstream/Host、路由绑定，以及决定凭据作用范围的公开访问、包过滤、认证缓存与 Pull-only 策略。System 信息只向 Admin/Operator 开放；Operator 响应会省略证书/私钥路径、监听/Socket 端点、进程 PID、生成的接入片段与 Nginx 原始诊断。Health API 对所有非 Admin 角色同样省略本地 Network/Socket 端点坐标。配置历史的验证输出、仓库激活错误和失败 Audit Entry 的详情也仅限 Admin。运行设置与生成的入口配置仅限 Admin。

Managed Upstream Nginx 记录 `$uri` 而不是 `$request_uri`，因此 Access Log 不会持久化 Token、签名等 Query 值；管理 API 只允许 Admin/Operator 读取这些记录。

---

## 8. Webhook 告警通知与 HMAC-SHA256 签名

企业级事件通知支持平台消息格式适配与防篡改验签：
- 同一时间只启用一个 Webhook 目标。MirrorRelay 会自动识别钉钉、飞书/Lark、企业微信和 Slack 主机并使用对应平台格式，其他主机使用通用 JSON。
- 每次通知均携带 `X-MirrorRelay-Signature: sha256=<hex>` 签名与 `X-MirrorRelay-Event: <event>` 请求头，供接收端验证消息完整性与来源真实性。
- 默认强制 HTTPS。明文 HTTP 与私网/环回/链路本地目标分别需要显式启用 `webhook.allow_http` 和 `webhook.allow_private`。
- 配置目标及每次重定向都会在连接前解析并过滤。安全 Dialer 会拒绝 DNS 重绑定到受限地址，TLS 主机名验证始终开启，最多接受五次重定向。
- Webhook 测试可使用当前运行中目标，也可使用一个按相同策略校验的临时目标；临时目标不会继承运行中签名密钥。非法 JSON 会立即终止且不会发送通知。包含 Webhook 目标在内的设置 API 仅限 Admin。

---

## 9. 漏洞报告指引

若您在项目中发现潜在安全漏洞，请阅读 [.github/SECURITY.md](../.github/SECURITY.md) 获取负责任披露与报告指引。
