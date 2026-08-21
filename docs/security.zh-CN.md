# 安全模型与规范

[English](security.md) | [简体中文](security.zh-CN.md)

MirrorRelay 从架构底层贯彻零信任网络边界、严格的上游源隔离以及针对服务端请求伪造（SSRF）与凭据泄露的深度防御体系。

---

## 1. 核心安全不变式

1. **专用数据平面物理隔离**：Go 控制平面绝不直接发起对外部上游软件包服务器的 HTTP 连接，所有上游流量均由受管、静态链接的 `受管上游 Nginx` 独立进程代理处理。
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

- **密码哈希算法**：管理员密码采用 **Argon2id**（内存：64 MB，迭代轮数：3，并发度：2）高强度哈希算法。
- **并发防 DoS 节流**：密码校验由并发信号量保护，防止通过大量并发登录请求耗尽 CPU 资源。
- **高熵会话令牌**：采用 256 位密码学安全随机数（`crypto/rand`），在 SQLite 数据库中以 SHA-256 哈希值持久化（明文令牌绝不落盘）。
- **Cookie 安全属性**：会话 Cookie 强制开启 `HttpOnly`、`Secure` 及 `SameSite=Strict`。
- **CSRF 双重防护**：所有修改状态的 API 请求（`POST`、`PUT`、`DELETE`）必须携带有效的 `X-CSRF-Token`，并在服务端通过常量时间比较校验。

---

## 4. 输入验证与注入防御

- **SQL 注入防御**：`internal/database/store.go` 中 100% 的 SQLite 查询均采用参数化 `?` 占位符，并强制启用 `PRAGMA foreign_keys = ON`。
- **路径穿越防御**：仓库路径严格过滤 NUL 空字符、回车换行、反斜杠、目录跳转符（`.` / `..`）及 URL 编码分隔符（`%2f`、`%5c`）。缓存文件路径采用清洗后标准路径的 SHA-256 哈希值作为落盘键名。
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
- `/var/lib/mirrorrelay/`：`0750 mirrorrelay:mirrorrelay`
- `/var/cache/mirrorrelay/`：`0750 mirrorrelay:mirrorrelay`
- `/run/mirrorrelay/*.sock`：`0660 root:mirrorrelay`

---

## 6. 软件供应链安全与包名黑白名单防御（Package Name Guard）

MirrorRelay 内置企业级供应链投毒与依赖混淆（Dependency Confusion）主动防御机制：
- **包名黑名单拦截（`blocked_packages`）**：支持正则表达式与 Glob 通配符（如 `^malicious-.*`、`bad-pkg-*.tar.gz`），实时阻断受污染恶意包的拉取。
- **包名白名单准入（`allowed_packages`）**：支持企业安全准入限定（如 `^internal-.*`），仅允许经合规审计的包通过。
- 当客户端请求命中拦截规则时，MirrorRelay 将立即终止代理并返回 HTTP `403 Forbidden`，同时向系统审计日志与 Webhook 发送安全拦截警报。

---

## 7. 基于角色的权限访问控制（RBAC）

MirrorRelay 支持三级权限隔离体系：
- **`admin`（超级管理员）**：拥有全系统控制权，包括用户增删、系统级设置覆写、服务平滑重启及 Webhook 联通性测试。
- **`operator`（运维管理员）**：具备仓库配置管理（CRUD）、缓存精确刷新、健康检查触发及 Nginx 平滑重载/回滚权限，不可修改系统用户或核心底层配置。
- **`viewer`（只读审计员）**：仅具备监控大盘、日志、已脱敏仓库详情与健康状态的只读查看权限。静态认证/Cookie/Token Header 及含凭据的 Token URL 不会出现在 Viewer 响应中；Effective、逐仓库及自定义 Managed Upstream Nginx 配置仅允许 `admin` 或 `operator` 读取。一切写操作均被拒绝（HTTP 403）。

---

## 8. Webhook 告警通知与 HMAC-SHA256 签名

企业级事件通知支持平台消息格式适配与防篡改验签：
- 同一时间只启用一个 Webhook 目标。MirrorRelay 会自动识别钉钉、飞书/Lark、企业微信和 Slack 主机并使用对应平台格式，其他主机使用通用 JSON。
- 每次通知均携带 `X-MirrorRelay-Signature: sha256=<hex>` 签名与 `X-MirrorRelay-Event: <event>` 请求头，供接收端验证消息完整性与来源真实性。
- 默认强制 HTTPS。明文 HTTP 与私网/环回/链路本地目标分别需要显式启用 `webhook.allow_http` 和 `webhook.allow_private`。
- 配置目标及每次重定向都会在连接前解析并过滤。安全 Dialer 会拒绝 DNS 重绑定到受限地址，TLS 主机名验证始终开启，最多接受五次重定向。
- Webhook 测试可使用当前运行中目标，也可使用一个按相同策略校验的临时目标；临时目标不会继承运行中签名密钥。非法 JSON 会立即终止且不会发送通知。非管理员设置响应绝不会包含 `webhook.secret` 或可能携带凭据的 Webhook URL。

---

## 9. 漏洞报告指引

若您在项目中发现潜在安全漏洞，请阅读 [.github/SECURITY.md](../.github/SECURITY.md) 获取负责任披露与报告指引。
