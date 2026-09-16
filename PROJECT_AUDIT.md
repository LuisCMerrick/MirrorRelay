# MirrorRelay 项目只读审计报告

审计日期：2026-09-15（Asia/Shanghai）
审计基线：`836d97c77300f9f1b3564a09bc2ac62ca352b768`
范围：`/opt/MirrorRelay`；239 个 Git 跟踪文件，Go 源码约 36,009 行（含测试）。

修复更新：2026-09-16。第 1–5 节保留原始只读审计记录，源码位置指向审计基线；当前工作区的修复、验证和升级注意事项见第 6 节。第 4 节临时 Overlay 断言的是旧漏洞行为，不应再用它判断修复是否通过。

## 1. 结论

本次发现 **9 项问题：4 项 P1、5 项 P2**。最需要优先处理的是上游响应头触发跨仓库越权读取、默认同源部署的管理权限风险、包名拦截绕过和已撤销会话恢复。

现有单元测试、竞态检测、静态检查、双架构编译及内置 Nginx 集成测试均通过。另在仓库外编写了 10 个定向验证场景，均复现了预设的问题现象。A02 验证到了 HTTP 响应层，浏览器内的权限利用链依据源码及同源机制分析，未实际执行浏览器攻击。

本次未使用子代理，未修改源码、配置、依赖或已有测试。项目内唯一新增文件为本报告。复现代码、数据库和 Nginx 测试文件位于 `/tmp`，临时进程均已结束；未连接业务实例、操作真实缓存或发送真实通知。

优先级含义：P1 为应优先修复的安全边界问题；P2 为需要修复的运行正确性或策略一致性问题。此分级不等同于 CVSS 评分。

| 编号 | 优先级 | 发现 | 证据状态 |
|---|---|---|---|
| A01 | P1 | 上游 `X-Accel-Redirect` 可绕过 Go 的跨仓库访问限制 | Go 代理与真实 Nginx 联合复现 |
| A02 | P1 | 默认管理端与上游 HTML 同源，可形成管理权限利用链 | HTTP 响应已复现；浏览器影响为高置信分析 |
| A03 | P1 | 编码适配器 URL 可绕过包名黑名单 | 请求级复现：403 → 200 |
| A04 | P1 | 会话续期可重新插入已撤销的会话 | 真实 SQLite、确定性并发时序复现 |
| A05 | P2 | 本地 304 响应忽略鉴权差异及禁缓存指令 | 请求级复现 |
| A06 | P2 | 缓存清理不能使本地元数据验证器失效 | 真实缓存代际更新后复现 |
| A07 | P2 | 配置验证失败会删除已发布的配置目录 | 临时文件系统与失败注入复现 |
| A08 | P2 | 生成的 Host 模式入口缺少加速 Location，导致循环及 500 | 真实 Nginx 复现 |
| A09 | P2 | 零拷贝绕过 Registry Full-proxy 重定向处理 | Go 分支与真实入口行为分别复现 |

## 2. 详细发现

### A01 — 上游响应头可触发跨仓库越权读取（P1）

位置：[Nginx 代理公共配置](/opt/MirrorRelay/internal/upstreamnginx/render_repo.go:201)、[仓库内部 Location](/opt/MirrorRelay/internal/upstreamnginx/render_repo.go:111)、[Go 访问策略检查](/opt/MirrorRelay/internal/proxy/engine.go:152)。

Managed Upstream Nginx 没有禁用上游响应中的 `X-Accel-Redirect` 控制语义。一个公开仓库的上游可以要求 Nginx 内部跳转到另一个仓库的 `/_repo/<repository>/<upstream>/...`。这次跳转发生在 Nginx 内部，不会重新经过 Go 的 `access_policy` 或包策略检查；目标 Location 还会应用目标仓库自己的静态认证 Header。

触发条件：攻击者能控制一个已配置上游的响应，且实例中存在其他受限仓库。控制上游响应包括上游遭入侵的情形；不要求攻击者拥有受限仓库凭据。

本地复现设置了公开仓库 A，以及 `access_policy=admin`、带静态 Authorization 的仓库 B。非管理来源直接请求 B 返回 403。A 的上游响应加入：

```http
X-Accel-Redirect: /_repo/2/2/package/private.zip
```

随后，同一来源请求 A 得到 200 和 B 的受保护正文。B 的模拟源站确认收到了 B 的认证 Header。此测试通过项目 Go 代理及生成的真实 Nginx 仓库配置运行，且 `zero_copy_bypass=false`。因此，关闭零拷贝不能缓解 A01。复现使用环回 HTTP 源站并为测试开启相应许可，跨仓库跳转的根因不依赖生产环境开启 HTTP 或私网访问。

建议：在所有面向外部上游的 Nginx 代理 Location 中禁用 `X-Accel-Redirect` 等不应由上游控制的内部行为；明确区分 Go 发给入口的可信加速响应与外部源站响应。仅隐藏响应头或在 Go 中删除它太晚，Nginx 可能已经执行内部跳转。补充公开仓库到受限仓库、跨包策略及开启/关闭零拷贝的回归测试。

### A02 — 默认同源部署允许上游脚本进入管理端信任域（P1）

位置：[默认管理 Host](/opt/MirrorRelay/internal/config/defaults.go:107)、[管理路由分发](/opt/MirrorRelay/internal/api/server.go:297)、[代理响应处理](/opt/MirrorRelay/internal/proxy/response_modifier.go:17)、[会话及 CSRF Token 响应](/opt/MirrorRelay/internal/api/admin_api.go:113)。

默认 `admin.host` 为空，路径模式仓库与 `/admin/` 可以位于同一个 HTTPS Origin。普通仓库响应可直接返回上游 HTML 和脚本；管理页面的 CSP 仅施加于管理页面及 API 响应，不约束另一个仓库页面的脚本。HTML URL 重写也没有承担脚本隔离职责。

触发条件：管理员已登录、从允许的管理网络访问，与管理端同源的仓库内容可被攻击者控制，并且管理员打开该内容。仓库脚本可以向 `/admin/api/v1/auth/session` 发起同源请求、读取 CSRF Token，再以该会话调用管理 API。Cookie 的 HttpOnly、Secure、Path 和 SameSite 属性不能隔离同一 Origin 内的这类请求。

响应级复现：使用默认空管理 Host，让模拟上游返回包含无害 `<script>` 标记的 HTML。Go 代理返回 200，脚本保留，且没有对该文档设置沙箱 CSP。**未安装或运行浏览器验证上述管理 API 利用链，也未执行任何管理写操作。**

建议：生产部署明确要求独立的管理 Origin，并确保该 Origin 不提供上游内容；对必须共用 Origin 的兼容模式建立内容沙箱或强制下载策略。补充浏览器测试，验证仓库 HTML、SVG 等主动内容不能读取管理会话或调用管理 API。

### A03 — 编码适配器地址绕过包名黑名单（P1）

位置：[外层路径的包策略检查](/opt/MirrorRelay/internal/proxy/route.go:78)、[`__fetch` 解码及返回](/opt/MirrorRelay/internal/proxy/route.go:106)、[包名匹配](/opt/MirrorRelay/internal/proxy/route.go:458)。

`routeRequest` 在解码 `__fetch` URL 前检查包名。检查对象是 Base64 字符串，解码得到实际目标后没有再次执行包策略。该适配器入口也没有要求仓库启用 Metadata Rewrite。

复现配置：公开路径 `/audit/`，上游 `https://repo.example/base/`，黑名单 `blocked.zip`，普通透明代理模式。

```text
GET /audit/blocked.zip
→ 403，未访问数据面

GET /audit/__fetch/aHR0cHM6Ly9yZXBvLmV4YW1wbGUvYmFzZS9ibG9ja2VkLnppcA
→ 200，返回被禁止包的模拟正文
```

第二个地址编码的是同一上游的 `https://repo.example/base/blocked.zip`。无须绕过 DNS 或目标域名校验即可触发。

建议：解析出最终规范化目标后统一执行路径与包策略检查，并在代理重定向的每一跳重新执行适用策略；限制适配器入口的适用模式和作用范围。回归测试覆盖直接 URL、编码 URL、辅助资源入口和重定向后的实际包名。

### A04 — 并发续期可恢复已撤销会话（P1）

位置：[会话读取及续期](/opt/MirrorRelay/internal/auth/session.go:104)、[会话撤销](/opt/MirrorRelay/internal/auth/session.go:152)、[数据库 UPSERT](/opt/MirrorRelay/internal/database/auth_audit.go:83)、[修改密码后的撤销](/opt/MirrorRelay/internal/api/admin_api.go:694)。

`Sessions.Get` 先读取会话，剩余有效期不足一半时再调用 `PutSession` 续期。`PutSession` 使用 INSERT/UPSERT，允许创建不存在的会话行。读取与写回之间若发生登出或撤销，续期会把刚刚删除的行重新插入。

确定性复现使用真实 SQLite，并控制以下交错顺序：

1. 一个请求读取到仍有效、需要续期的旧会话，暂停在写回前。
2. 执行 `RevokeUser`，确认数据库中该行已经不存在。
3. 恢复第一个请求，使其续期写回。
4. 使用旧 Token 再发起请求，认证成功。

影响：持有旧 Token 的一方可能在密码修改、登出或应急撤销之后继续访问。该问题需要命中续期窗口和并发交错；不是不持有凭据即可登录。复现加上 `-race` 后仍成立且没有数据竞态告警，属于逻辑时序问题。

建议：将创建与续期拆成不同数据库操作，续期只更新仍存在且有效的会话并检查受影响行数；必要时引入账户会话版本，统一处理密码变更、CLI 恢复及正在进行的认证。增加“读取 → 撤销 → 续期”的确定性测试。

### A05 — 本地 304 响应忽略认证差异和禁缓存设置（P2）

位置：[本地 304 快速返回](/opt/MirrorRelay/internal/proxy/engine.go:234)、[验证器缓存键](/opt/MirrorRelay/internal/proxy/route.go:291)、[验证器写入](/opt/MirrorRelay/internal/proxy/response_modifier.go:68)。

元数据验证器没有包含请求认证分区，写入及使用时也没有遵守 `cache_enabled`、上游 `Cache-Control: private, no-store` 等限制。后续条件请求可以直接得到 304，从而跳过数据面及源站鉴权。

复现：禁用仓库缓存，模拟源站要求 Bearer 凭据并返回 `private, no-store`。第一次携带有效凭据请求得到 200。随后不携带凭据、仅发送 `If-None-Match: *`，仍得到 304；源站总访问次数保持为 1。

影响：未认证请求可以获得私有表示的验证信息，401/403 与禁缓存语义失效，持有旧缓存的客户端可能继续使用不应认可的内容。此复现没有证明向未持有缓存的客户端返回私有正文。

建议：为本地验证器建立与正文缓存一致的准入规则，遵守缓存开关、请求及响应缓存指令，并纳入认证与内容协商维度。需要源站重新鉴权的请求应访问源站后决定响应。

### A06 — 缓存清理后仍可命中旧元数据验证器（P2）

位置：[缓存代际更新](/opt/MirrorRelay/internal/cachectl/manager.go:112)、[带代际的正文缓存键](/opt/MirrorRelay/internal/cachectl/manager.go:92)、[独立验证器键](/opt/MirrorRelay/internal/proxy/route.go:291)、[本地 304 返回](/opt/MirrorRelay/internal/proxy/engine.go:234)。

清理缓存会递增全局、仓库或对象代际，使 Nginx 正文缓存键变化。但元数据验证器键不含代际，清理操作也不清空这些验证器。

复现：先请求重写元数据并保存 ETag，成功执行真实 `cache.Purge(..., "repository", ...)`，再携带旧 ETag 请求。结果仍为 304，模拟源站只被访问过一次。

影响：清理后的条件请求仍可能沿用旧索引，直到验证器 TTL 到期；系统默认 Metadata TTL 为 5 分钟。紧急刷新元数据或撤回异常缓存时，管理操作的效果不完整。此项与 A05 的修复维度不同：即使正确区分认证，仍需绑定缓存代际。

建议：把正文缓存代际纳入验证器键，或提供覆盖全局、仓库、对象范围的同步失效机制；处理清理期间正在进行的响应写回。为三种清理范围分别增加条件请求测试。

### A07 — 校验失败会删除已发布配置目录（P2）

位置：[复用版本目录](/opt/MirrorRelay/internal/upstreamnginx/controller.go:429)、[校验失败清理](/opt/MirrorRelay/internal/upstreamnginx/controller.go:119)、[管理端测试入口](/opt/MirrorRelay/internal/api/admin_api.go:314)。

`writeVersion` 发现同 Hash 的配置目录已存在时直接复用。之后 `ValidateWithCustom` 只要执行 `nginx -t` 失败，就无条件删除该目录，未区分本次新建目录与已发布目录。

触发条件：对与当前配置 Hash 相同的候选执行验证，随后校验命令失败，例如依赖文件不可用或进程执行被取消。该测试 API 对 Admin/Operator 开放。

文件系统复现：预先写入一个版本并让 `current` 指向它，再注入校验失败。版本目录被删除，`current` 变成悬空符号链接。

影响：配置验证会破坏现有活动配置文件。已运行的 worker 可能仍能继续服务，但后续重载、退出及恢复过程会受到影响。本次只在临时目录注入失败，没有操作在线 Nginx 配置。

建议：候选验证使用独立临时目录，失败时只清理本次创建且未发布的目录；保护当前及恢复所需的版本。回归覆盖已有版本、正在使用的版本和请求取消。

### A08 — 生成的 Host 模式入口会在零拷贝请求上循环（P2）

位置：[Host 模式 server 生成](/opt/MirrorRelay/internal/upstreamnginx/generator.go:253)、[共享 server 的内部 Location](/opt/MirrorRelay/internal/upstreamnginx/generator.go:274)、[入口声明支持加速](/opt/MirrorRelay/internal/upstreamnginx/generator.go:315)、[Go 加速响应](/opt/MirrorRelay/internal/proxy/engine.go:247)。

生成器为每个 Host 模式仓库创建独立 server，但其中只有转发到 Go 的 `location /`。`/_repo/` 内部加速 Location 生成在后续供共享仓库 server 使用的片段中，不在这些独立 server 内。Host 模式入口仍然发送 `X-Accel-Supported: 1`。

触发条件：部署生成的 Host 模式 server，未手工补充内部加速 Location，且开启零拷贝。Go 返回 `X-Accel-Redirect` 后，Nginx 在同一 server 内重新匹配，命中 `location /` 并再次转发 Go。

真实 Nginx 复现使用生成的 Host server 路由结构，仅将 TLS 监听替换为临时环回 HTTP 监听，并模拟 Go 的加速响应。一次请求触发 11 次前端调用，最终返回 500。

建议：让每个声明支持加速的 server 都包含完整且标记为 `internal` 的目标 Location；增加 Host/Path 两种模式的入口集成测试。完成修复前，对受影响部署关闭 `performance.zero_copy_bypass` 可避开此问题。

### A09 — 零拷贝会跳过 Registry Full-proxy 重定向处理（P2）

位置：[零拷贝条件及提前返回](/opt/MirrorRelay/internal/proxy/engine.go:247)、[Go 重定向处理](/opt/MirrorRelay/internal/proxy/transport.go:169)、[Nginx 重定向配置](/opt/MirrorRelay/internal/upstreamnginx/render_repo.go:201)。

加速条件明确包含 `blob_redirect_mode != "pass"` 的 Blob，因此 `full_proxy` 请求也会提前返回加速头。实际跟随重定向、验证每跳目标及继续代理的逻辑位于 Go Transport，此时被跳过。生成的 Nginx 链路不会代替 Go 完成 Full-proxy 行为。

两部分复现结果：

- Go 代理：同一个 Full-proxy Blob 请求，在关闭零拷贝时访问数据面两次并取得最终正文；开启后直接交出初始内部路径。
- 真实入口 Nginx：完整内部加速 Location 收到模拟数据面的 307 后，把 307 和原始 CDN `Location` 返回客户端。

影响：客户端需要直接访问原始 CDN；在只允许访问 MirrorRelay 的网络中，镜像拉取可能失败。即使补齐 A08 的 Location，此问题仍然存在。普通需要 Go 跟随重定向的包请求也应审查相同提前返回条件。

建议：对仍需 Go 执行重定向、鉴权挑战或仓库回退处理的请求禁用提前加速，或在完成这些步骤后再交付经过验证的最终目标。回归应覆盖真实入口到最终 Blob 的完整链路。

## 3. 已执行的检查

环境为 Linux amd64、Go `1.27.1`、Node.js `26.8.2`。仓库声明 Go `1.26.6`，CI 使用 Node.js 22；本轮结果不代替在这两个指定版本上的验证。Go 检查使用 `GOTOOLCHAIN=local`、`GOPROXY=off`，测试及构建使用 `-mod=readonly`。

| 检查 | 结果 |
|---|---|
| `go test -mod=readonly -count=1 -timeout=180s ./...` | 27 个有测试的包通过；`internal/model` 无测试文件 |
| `go test -mod=readonly -race -p 1 -count=1 -timeout=180s ./...` | 全部通过，无竞态检测报告 |
| `go vet -mod=readonly ./...` | 通过 |
| `gofmt -l cmd internal` | 无未格式化文件 |
| `go mod verify` | 所有本地模块校验通过 |
| `scripts/verify-web-assets.sh` | 通过；Node 26 提示未提供 localStorage 文件的实验性警告 |
| `node scripts/verify-docs.mjs` | 27 个文档及双语结构、本地链接检查通过 |
| ShellCheck：`build/*.sh`、`scripts/*.sh`、Debian 生命周期脚本 | 通过 |
| 内置 Nginx 的 SHA-256、`file`、`readelf -l` | 校验通过；amd64 静态 ELF，无动态解释器段 |
| 两个 `TestRealManagedUpstreamNginx...` 集成测试 | 均通过，含生成配置验证和流式响应测试 |
| `CGO_ENABLED=0`，分别 `GOARCH=amd64`、`arm64` 编译到 `/dev/null` | 均通过；没有在 arm64 运行二进制 |
| `docker compose config --quiet` | 通过；未启动 Compose 服务 |
| 10 个仓库外定向审计场景 | 全部命中各自断言的问题现象 |
| 会话撤销复现单独加上 `-race` | 问题仍成立，无数据竞态告警 |

注意：临时审计场景中的 `PASS` 表示“成功复现当前问题”，不是问题已经修复。现有测试套件通过，也不能排除源码、数据库与 Nginx 之间的逻辑边界问题。

## 4. 复现材料

临时材料位于 `/tmp/mirrorrelay-audit-20260915-o3JPsz/`：

- `auth_audit_test.go`：会话撤销与续期交错。
- `proxy_audit_test.go`：包策略、元数据验证器、Full-proxy 分支和上游 HTML。
- `nginx_audit_test.go`：配置目录失败清理、真实入口及跨仓库访问。
- `overlay.json`：通过 Go Overlay 虚拟加入测试，项目内没有创建这些测试文件。

当前工作环境内可复跑：

```bash
GOTOOLCHAIN=local GOPROXY=off \
MIRRORRELAY_TEST_UPSTREAM_NGINX=/opt/MirrorRelay/nginx/sbin/nginx \
go test -mod=readonly \
  -overlay=/tmp/mirrorrelay-audit-20260915-o3JPsz/overlay.json \
  -run '^TestAudit' -v -count=1 -timeout=60s \
  ./internal/auth ./internal/proxy ./internal/upstreamnginx
```

这些文件为临时审计材料，可能被系统清理，不属于项目交付源码；各问题的触发条件和观测结果已记录在本报告中。模拟源站只监听回环地址，使用虚构凭据，没有访问外部软件仓库或真实受限资源。

## 5. 覆盖范围与后续建议

本轮重点审阅了管理 API 与 RBAC、会话和恢复流程、代理路由及凭据处理、缓存及 Nginx 配置生命周期，并抽查集群同步、Webhook、预热、前端输出及发布配置。这是定向审计，不是所有文件的逐行证明。

已观察到的有效防护包括：普通代理链路对上游地址进行解析及 IP 固定、TLS 校验、管理 API 的角色和 CSRF 检查、初始管理员的原子创建、参数化 SQL、恢复码单次消费及集群变更凭据加密。这些实现应保留，但不抵消上文的跨层问题。

本轮未执行：浏览器端完整攻击验证、真实 Docker/包管理器端到端拉取、线上或压力测试、真实多节点部署、镜像构建与软件包安装卸载、在线漏洞库查询、`govulncheck` 和 `actionlint`。依赖校验通过仅说明本地模块内容匹配校验记录，不能据此判断没有已知漏洞。

建议处理顺序：

1. 优先修复 A01 的上游响应控制头边界；对 A02 使用独立管理 Origin；修复 A03、A04 的策略和会话逻辑。
2. 修复 A05、A06，使认证、缓存开关及清理覆盖元数据验证器。
3. 修复 A07 的版本目录所有权与清理规则，以及 A08、A09 的入口加速链路。关闭零拷贝可临时规避 A08/A09，不能缓解 A01。
4. 将复现改写为断言安全行为的正式回归测试，并将 Go → 入口 Nginx → 数据面 → 模拟源站的跨层测试纳入 CI。

本报告仅记录问题及建议，未实施修复、提交代码、发布内容或更改部署。

## 6. 修复记录（2026-09-16）

按照后续修复请求，A01–A09 均已在工作区落实源码修复及正式回归测试。全程未使用子代理，未提交代码、发布软件或修改业务部署。

| 编号 | 修复结果 | 正式回归覆盖 |
|---|---|---|
| A01 | 所有源站代理 Location 禁止上游 `X-Accel-*` 控制语义；外部入口的内部加速 Location 同样忽略源站跳转；Go 清理上游控制头 | 真实入口 → Go → 真实数据面 → 模拟源站，分别禁止跨仓库和跨包策略跳转，覆盖零拷贝开/关 |
| A02 | 所有仓库响应强制使用不保留原 Origin 的 CSP 沙箱及 `nosniff`，覆盖重写与非重写、HTML/SVG 和零拷贝；保留独立管理 Host 部署建议 | Chromium 真实执行 HTML/SVG 探针：Cookie/本地存储不可访问、管理会话 Fetch 不可读取；可信同源对照页正常读取模拟会话；零拷贝开/关均通过 |
| A03 | 解码后统一检查最终目标路径、上游 Base/Host 重写及包策略；每次跟随重定向和向数据面发送前复查 | 直接 URL、`__fetch`、`__fetch_template`、有效签名辅助 URL、百分号编码、黑/白名单、重叠 Base、重定向目标和允许下载的正向对照 |
| A04 | 新增仅 UPDATE 存在且未过期会话的续期操作并检查行数；失败时拒绝认证；内存会话写回前复查撤销状态；API 复用统一会话存储接口 | SQLite 确定性“读取 → 撤销/登出/密码重置 → 续期”交错；缺失/过期/撤销记录不可恢复，有效会话仍可续期 |
| A05 | 本地验证器仅准入无凭据、缓存开启的新鲜公开表示；遵守请求/响应缓存指令、Cookie、Vary、Age/Expires，按 Accept/编码区分；304 保留缓存及安全头 | 关闭缓存、认证正文缓存开/关、无效凭据、静态 Authorization/Cookie、禁缓存指令、响应 Cookie、内容协商和过期时间 |
| A06 | 验证器键包含带代际的正文缓存键；旧请求只能写回旧代际 | 真实全局/仓库/对象清理；清理发生于源站响应期间时，晚完成的旧响应不能恢复当前验证器 |
| A07 | 独立临时目录验证，失败/取消只删除自身临时目录；新版本完整写入后原子重命名，现有版本不被失败验证删除 | 新版本、已有未发布版本、当前版本分别注入失败与取消；校验目录清理及现有文件保持完整 |
| A08 | Host 与 Path 模式共用完整的 `internal /_repo/` 生成函数 | 真实完整代理链下载成功且只经过一次 Go 前端；外部直接访问内部 Location 返回 404 |
| A09 | 需要 Go 跟随/重写重定向、Full-proxy 认证挑战或多上游回退时不提前加速；补齐非 Registry `full_proxy` 的重定向处理 | Registry 最终 Blob 通过真实完整链路返回，不向客户端泄漏 CDN 跳转；普通包的 follow/full_proxy/rewrite 和多上游回退 |

主要测试文件：

- [代理策略与缓存回归](internal/proxy/audit_regression_test.go)、[验证器新鲜度测试](internal/proxy/validator_cache_test.go)。
- [会话并发回归](internal/auth/session_refresh_test.go)、[数据库续期测试](internal/database/session_refresh_test.go)。
- [真实 Nginx 跨层回归](internal/upstreamnginx/audit_integration_test.go)、[浏览器隔离回归](internal/upstreamnginx/browser_isolation_test.go)、[配置验证安全测试](internal/upstreamnginx/validation_safety_test.go)。

### 6.1 验证结果

本轮使用 Go 1.27.1、Node.js 26.8.2、仓库内 Nginx 1.30.4，以及临时目录中的 Chrome for Testing 153.0.8010.47。浏览器与补充动态库仅下载/解包到 `/tmp/mirrorrelay-browser-20260916-8M38yP/`，未安装系统软件包、修改用户浏览器配置或添加应用依赖。模拟管理端使用虚构会话及 CSRF 数据，没有访问真实管理 API。

- 全项目 `go test -mod=readonly -count=1 -timeout=180s ./...`：通过，包含启用真实 Nginx 和浏览器后的测试。
- 全项目 `go test -mod=readonly -race -p 1 -count=1 -timeout=180s ./...`：通过，无竞态检测报告，同样包含真实 Nginx 和浏览器测试。
- 全项目 `go vet -mod=readonly ./...`：通过。
- Linux amd64 / arm64、`CGO_ENABLED=0`、`-trimpath -buildvcs=false` 构建：均通过；未在 arm64 执行二进制。
- 文档及双语结构、Web 资源/语言包检查：通过；Node 仍有原先的 localStorage 实验性提示。
- `go mod verify`、ShellCheck、`git diff --check`：通过。修改后的 CI 配置通过项目指定的 Actionlint 1.7.11 检查。

Go 测试、Vet 和构建使用 `GOTOOLCHAIN=local GOPROXY=off`，未变更 `go.mod` 或 `go.sum`。浏览器测试已加入 CI；其他真实 Nginx 回归沿用现有 CI 集成测试入口。未替代 Go 1.26.6 / CI Node 22 的对应版本验证，未执行真实客户端拉取、线上压力测试或多节点部署测试。

可在安装 Chromium 兼容浏览器的开发机复跑：

```bash
MIRRORRELAY_TEST_BROWSER="$(command -v google-chrome)" \
MIRRORRELAY_TEST_UPSTREAM_NGINX="$PWD/nginx/sbin/nginx" \
go test -mod=readonly ./internal/upstreamnginx \
  -run '^TestRealManagedUpstreamNginx' -count=1 -timeout=180s
```

### 6.2 升级与兼容性注意事项

1. 本次只修改项目工作区，尚未部署。上线时需更新 Go 程序，让 Managed Upstream Nginx 成功生成并激活新配置；外部共享 Nginx 的接入片段需重新生成、审阅并由管理员应用/重载，项目不会代为修改外部入口。仅替换 Go 程序而保留旧数据面配置不能完整修复 A01。
2. 仓库文档沙箱允许 DOM 脚本及下载，但有意限制 Cookie/存储、Fetch、Worker、表单和嵌入。依赖这些能力的上游页面会受到兼容性影响；`safe-ui=1` 不能关闭安全隔离。生产仍建议使用独立管理 Origin。
3. 认证、需要源站重新确认及多上游/重定向处理的请求可能比旧版本更少命中本地 304 或零拷贝，这是为保持权限和代理语义采取的保守行为。
4. 无数据库结构迁移；会话创建保持原有流程，续期改为独立的非插入操作。

相应操作说明已同步到 [英文配置文档](docs/configuration.md)、[中文配置文档](docs/configuration.zh-CN.md) 及双语安全文档。
