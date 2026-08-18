# 架构设计指南

[English](architecture.md) | [简体中文](architecture.zh-CN.md)

本文档提供 MirrorRelay 整体系统架构、双平面设计理念及配置生命周期的深度技术解析。

---

## 1. 整体请求处理链路

MirrorRelay 采用严格的双平面设计，将对外接入/策略控制与高吞吐上游数据传输完全解耦：

```text
客户端 (apt / dnf / pip / npm / docker)
   │ (HTTP / HTTPS 监听端口 80 / 443)
   ▼
外部共享 Nginx (管理员拥有)
   │ (Unix Domain Socket /run/mirrorrelay/frontend.sock, 权限 0660)
   ▼
MirrorRelay 前端 (Go 核心服务)
   ├── 路由引擎与身份认证
   ├── 元数据与 HTML URL 重写器 (有界内存缓冲区)
   ├── 容器镜像 Token Broker 与重定向 Broker
   ├── SQLite 持久化期望状态存储
   └── 上游 Nginx 候选配置生成器与预检器
   │ (Unix Domain Socket /run/mirrorrelay/upstream.sock, 权限 0660)
   ▼
受管上游 Nginx (专用 Musl 隔离数据平面)
   ├── 高吞吐代理缓存 (/var/cache/mirrorrelay)
   ├── 连接池复用与 Keep-Alive 保持
   ├── TLS 主机名与证书链严格校验
   └── 多上游健康检查与故障转移
   │ (HTTPS / HTTP)
   ▼
原始上游源 (deb.debian.org, pypi.org, registry-1.docker.io 等)
```

---

## 2. 为什么需要两个 Nginx 实例？

MirrorRelay 最具特色的设计之一是**外部共享 Nginx** 与**受管上游 Nginx** 的明确分工：

### 外部共享 Nginx (入口平面 Ingress Plane)
- **归属**：归宿主机系统管理员统一管理。
- **职责**：负责公网 TLS 终结、域名证书管理（如 Let's Encrypt / Certbot）并将请求反向代理到 MirrorRelay 前端 Unix Socket。
- **隔离性**：MirrorRelay 软件包**绝不**安装、修改、重启或重载外部共享 Nginx，而是生成一份干净、局部的配置集成片段（如 `mirrorrelay.conf`）供管理员自由引用。

### 受管上游 Nginx (数据平面 Data Plane)
- **归属**：由 MirrorRelay 服务进程全生命周期独占管理。
- **职责**：执行所有发往原始上游的高吞吐 HTTP/HTTPS 流量交互、管理本地磁盘缓存与连接池。
- **二进制特性**：采用 musl libc、OpenSSL、PCRE2 与 zlib 完全静态编译构建，运行时零动态库依赖（`ldd` 返回无动态依赖）。
- **安全不变式**：Go 服务本身**绝不**直接向上游软件包服务器建立 HTTP 连接，所有上游流量均由受管上游 Nginx 代理执行。

---

## 3. Go 控制平面与路由器

Go 核心进程（`/usr/bin/mirrorrelay`）拥有所有业务策略逻辑与配置生命周期管理：

### A. 期望状态与生效状态分离 (Desired vs. Active)
1. **期望状态**：持久化保存在 SQLite 数据库中（`/var/lib/mirrorrelay/mirrorrelay.db`），通过 Web UI 或 REST API 修改。
2. **候选配置生成**：Go 渲染引擎在临时目录中生成一份候选的 `nginx.conf` 配置文件。
3. **严格语法预检**：调用 `/usr/lib/mirrorrelay/nginx/nginx -t -c <candidate_path>` 执行完整语法检查。
4. **原子覆盖发布**：预检完全通过后，原子性覆盖当前生效配置目录。
5. **平滑热重载**：向受管 Nginx Master 发送 `SIGHUP` 信号，旧 Worker 进程处理完现有连接后退出，新 Worker 无缝加载新配置，全程不丢请求。
6. **安全容错降级**：若候选配置预检失败，系统记录详细错误信息，但**当前正在运行的生效配置保持完全不变**，确保线上服务不中断。

### B. 有界元数据与 HTML 路径重写
针对响应体内嵌绝对或相对上游链接的仓库类型（如 PyPI Simple HTML 页面或可浏览的目录索引）：
- Go 仅在严格有界内存限制下缓冲元数据（如限制最大 10 MB）。
- 动态重写页面链接，使其重定向回 MirrorRelay 公开路由或受管上游路由（`/_mirrorrelay/upstream/<id>/`）。
- 大体积二进制包（如 `.deb`、`.rpm`、`.whl`、Docker 分层 Blob 等）采用固定大小流式传输，绝不进入 Go 堆内存。

### C. Token Broker 与重定向代理
- **容器镜像 Token Broker**：拦截上游 OCI Registry 的 `/v2/` `401 Unauthorized` 鉴权挑战，使用后台配置的上游凭据向鉴权服务器申请 Bearer Token 并注入请求，避免将敏感凭证泄露给下游客户端。
- **重定向安全代理**：解析上游返回的 HTTP `301/302/307` 重定向地址（如 S3/CloudFront 签名链接），严格执行 SSRF 与 IP 固定检查后进行安全代理。

### D. 零拷贝 X-Accel 旁路加速（大二进制包内核直传）
针对不可变的大体积软件包（如 `.deb`、`.rpm`、`.whl`、`.tar.gz`、`.iso`、容器分层 Blob 等）：
- 开启配置（`performance.zero_copy_bypass: true`）且在 Ingress Nginx 下运行时（`X-Accel-Supported: 1`），Go 负责完成所有 RBAC、SSRF 与包名黑白名单安全审计；
- 校验通过后，Go 直接返回 HTTP 200 及 `X-Accel-Redirect: /_repo/<repo_id>/<upstream_id>/package/<path>` 内部重定向头与路由元数据；
- Ingress Nginx 拦截此响应后直接与 Managed Upstream Nginx 的 Unix Socket 对接并利用 Linux 内核缓冲传输，彻底绕过 Go 用户态内存中转，实现万兆线速零拷贝直传。

---

## 4. 安全与网络不变式

- **SSRF 防御**：所有上游地址与重定向目标在连接前均解析并比对私有网络、环回地址及保留 CIDR 黑名单。
- **IP 锁定与 SNI 保持**：DNS 解析后的 IP 固定用于底层 TCP 拨号，同时完整保留 TLS SNI（`server_name`）证书链验证。
- **内部 Header 净化**：客户端请求中的 `X-Mirror-Internal-*` 请求头一律被强制剥离，防止伪造内部路由上下文。
- **Socket 权限约束**：Unix Domain Socket 默认权限严格为 `0660`，属主为 `root:mirrorrelay`。

---

## 5. 存储与文件布局

```text
/etc/mirrorrelay/config.yaml                 # 只读配置文件 (权限 0640)
/var/lib/mirrorrelay/mirrorrelay.db          # 持久化 SQLite 数据库 (期望状态、用户、审计日志)
/var/lib/mirrorrelay/runtime/upstream-nginx/ # 生效的 nginx.conf 与历史版本
/var/cache/mirrorrelay/                      # Nginx 代理缓存目录
/var/log/mirrorrelay/upstream-nginx/         # 访问与错误日志目录
/run/mirrorrelay/frontend.sock               # 接收外部 Nginx 流量的前端 Socket
/run/mirrorrelay/upstream.sock               # 转发至受管 Nginx 的内部 Socket
/run/mirrorrelay/upstream-nginx.pid          # Nginx 主进程 PID
```
