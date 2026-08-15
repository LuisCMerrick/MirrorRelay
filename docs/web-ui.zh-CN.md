# Web UI 使用指南

[English](web-ui.md) | [简体中文](web-ui.zh-CN.md)

RepoGate 在 `admin.path` 下内置管理后台，默认值为 `/admin/`；同源 API 位于 `<admin.path>api/v1/`。Web UI 可管理仓库、模板、Managed Upstream Nginx、缓存操作、健康状态、日志和管理员账户。

共享域名的公开根路径 `/` 是一个轻量仓库索引，会列出当前客户端可见的全部已启用仓库，并把 Path Mode 仓库链接到其公开路径，但不会公开管理后台路径。配置为 Host Mode 的独立域名仍由该仓库占用 `/`。生成的 External Shared Nginx 片段包含精确的根索引位置和已配置后台位置，同时不会接管其他无关路径。

## 访问与登录

请使用 External Shared Nginx 发布的 HTTPS 地址，例如：

```text
https://repo.example.com/admin/
```

只能在 `config.yaml` 中修改 `admin.path`，修改后应重启 RepoGate，并更新已审核的 External Shared Nginx 片段。路径会自动补齐末尾 `/`，也可以包含多个安全路径段。非默认路径可以减少无目的扫描发现，但不能替代身份验证；HTTPS、管理员凭据与 `security.admin_cidrs` 仍是必要控制。

Session Cookie 带有 `Secure`、`HttpOnly` 和 `SameSite=Strict` 属性。因此生产登录必须使用 HTTPS，UI 与 API 也必须保持同源。非回环主机上的明文 HTTP 地址可能接受登录请求，但浏览器不会保留 Session Cookie。不要把私有前端 Socket 或回环备用端口直接暴露到网络。

首次启动时，RepoGate 使用 `REPOGATE_ADMIN_USERNAME` 与 `REPOGATE_ADMIN_PASSWORD` 创建初始管理员；数据库已有用户后不会再次使用这些值。如果管理面只允许特定网络访问，请配置 `security.admin_cidrs`。

除非浏览器首选语言包含中文，否则 UI 默认使用英语。右上角的 `EN` 或 `中文` 可手动切换，选择会保存在浏览器 Local Storage。使用完毕后，从侧边栏底部退出登录。

## 运行模型

仓库与自定义配置变更遵循以下激活流程：

```text
编辑 Desired 状态 -> 生成 Candidate -> 使用 nginx -t 验证
                   -> 原子发布 -> Graceful Reload -> Active 状态
```

验证或 Reload 失败时，之前的 Active 路由配置继续工作。因此 Repositories 页面可能同时显示失败的 Desired 状态和仍在服务的最后一个有效 Active 状态。重试前应查看仓库错误、生成配置和 Managed Upstream Nginx 状态。

## 建议的首次使用顺序

1. 打开 **系统**，确认 RepoGate 与 Managed Upstream Nginx 的版本、架构和端点。
2. 打开 **健康状态**，确认 RepoGate、前端端点、Go Router、Managed Upstream Nginx 与上游端点正常或正在运行。
3. 打开 **入口接入**，审核生成的片段，并通过现有入口管理员的正常流程应用。
4. 新增一个仓库，测试上游并检查生成配置。
5. 打开公开域名根路径，确认仓库已出现在索引中。
6. 使用仓库详情中的客户端示例完成一次小请求，再接入生产流量。

## 概览

概览汇总：

- 仓库总数与启用数；
- 正常与异常仓库数；
- Managed Upstream Nginx 状态；
- 当前活动请求，以及今日、24 小时和 7 天的请求量与流量；
- 缓存命中率和已观测缓存占用；
- 各仓库流量、状态码分类、缓存命中/未命中和上游错误；
- RepoGate 与 Managed Upstream Nginx 的版本、Build ID、架构和运行时间。

这些计数是运维观测数据，不是计费记录。缓存占用通过异步扫描获得，因此可能晚于 Purge 或文件系统变化。

## 仓库

### 创建或编辑仓库

选择 **仓库 → 新增仓库**。模板可以填入推荐默认值，填入后的所有字段仍可修改。已有仓库会固定在所选模板版本，只有显式预览并应用升级后才会改变。

表单分组如下：

| 分组 | 用途 |
|---|---|
| 标识与路由 | 名称、Slug、仓库类型、Path/Host 发布方式和访问策略 |
| 上游与路径映射 | 有序 Origin URL、路径转换、Host 改写、代理与重定向行为 |
| Header 与超时 | 受控的请求 Header 添加/删除，以及连接、读取、发送时限 |
| 缓存与改写 | Cache 类别、Metadata Adapter、可浏览 HTML 同源 URL 改写、改写 Host Allowlist 和各类别 TTL 覆盖 |
| 健康检查与限制 | 探测设置、启用状态、并发模板和带宽限制 |
| Registry 与上游安全 | Registry 认证/Blob 行为，以及 HTTP/私网 Origin 权限 |

每行上游使用 `优先级 URL` 格式：

```text
10 https://primary.example/repository/
20 https://backup.example/repository/
```

优先级数字越小越先尝试。URL 必须包含 `http://` 或 `https://`；HTTP 与私网地址需要同时开启系统级和仓库级开关。TLS 验证不能关闭。

Path 模式应填写 `/debian/` 这类公开路径。RepoGate 会拒绝等于或包含后台/系统路径、与其他仓库路径重叠，或替换根索引的路径；也会拒绝重复公开 Host。配置 `http.public_base_url` 后，Host Mode 仓库还不能占用共享 Host。Host 模式应填写独立公开域名，并在 External Shared Nginx 中补齐 TLS/Server Block。只有 `security.admin_cidrs` 已定义预期客户端时，才应选择 **仅管理 CIDR**。

**改写可浏览 HTML 中的同源 URL** 是逐仓库兼容开关，默认关闭。它会按照实际上游页面 URL 解析 HTML 中的 `href`、`src`、`srcset`、`action` 及相关 URL 属性。仍在上游仓库 Base 内的目标会映射回公开仓库 Namespace；位于 Base 外但与上游同源的目标会映射为 `/_repogate/upstream/<仓库ID>/...`，从而继续提供上游原有图标、样式表、脚本和图片，而不替换其内容。跨 Origin URL 和非 HTTP(S) Scheme 保持不变。

辅助路由只接受 `GET`、`HEAD`，继续受仓库公开 Host 与访问策略约束，并复用该仓库已经 Pin 的上游、TLS 校验、Header、缓存策略和限制。开启后，上游仓库 Base 之外的同源路径会有意通过这个辅助 Scope 变得可访问；只应在适合暴露该上游范围时启用。存在已启用且需要该能力的 Path Mode 仓库时，生成的 External Shared Nginx 片段会包含 `/_repogate/upstream/`，请把更新后的片段应用到共享入口。HTML Body 使用仓库 Metadata 改写上限、压缩策略和重新生成的响应 Validator。

选择 **验证、保存并生效** 提交 Candidate。成功表示生成、验证、持久化、原子发布和 Graceful Reload 全部完成。错误会留在表单中，不会替换 Active 配置。

### 仓库操作

| 操作 | 结果 |
|---|---|
| 详情 | 展示 Desired/Active 状态、计数器、上游健康和客户端示例 |
| 复制地址 | 复制公开仓库 URL；浏览器可能要求 Clipboard 权限 |
| 测试 | 对全部启用上游运行已配置健康检查 |
| 配置 | 预览 Candidate 中该仓库生成的 Nginx 配置 |
| 清缓存 | 逻辑失效整个仓库或一个可选对象路径 |
| 编辑 | 打开完整仓库表单 |
| 启用 / 禁用 | 使用所选状态验证并激活新的 Desired 配置 |
| 删除 | 删除仓库并逻辑失效其缓存；此操作不可撤销 |

详情窗口还提供完整生效配置、生成的客户端命令，以及存在新模板时的升级预览。选择 **应用升级** 前，应审核逐字段 Diff 和生成配置。

列表和详情窗口中的操作都通过同源 API 完成；完成后会显示成功或错误消息，请求运行期间当前按钮会暂时禁用。**复制地址** 会优先使用浏览器 Clipboard API；浏览器拒绝 Clipboard API 时，自动回退到本地文本选择复制。

Cache Purge 使用 Generation。新请求会立即停止使用已失效 Namespace；旧物理文件会继续存在，直到异步 Nginx Cache Manager 按 `inactive` 与 `max_size` 回收。

## 模板

模板是针对各仓库生态的只读、带版本起点。页面展示模板名称、版本、仓库类型、默认上游、发布/代理模式，以及是否启用缓存或 Metadata 改写。选择模板不会让以后发布的新模板版本自动应用。

## Managed Upstream Nginx

此页面展示进程状态、PID、运行时间、二进制版本、Build ID、架构、当前配置版本、最后 Reload/退出信息、构建参数和生效配置。

- **重新生成、验证并 Reload**：对当前 Desired 仓库与自定义片段执行 Reconcile。
- **回滚**：从所选不可变历史版本恢复仓库与自定义配置，验证后执行 Graceful Reload。

回滚不只是替换一份 Nginx 文本；它会把持久化仓库和自定义配置状态恢复到所选快照。

## 自定义配置

自定义片段可以绑定受控的 `http`、`server`、`location`、`upstream` 或 Repository Context。Repository ID 为 `0` 表示全局片段。保存或删除片段时，会先生成并验证完整 Candidate，再进行激活。

RepoGate 会拒绝能够逃离所选 Context、创建 Listener/Route/Upstream Target、削弱 TLS 验证、改变 Cache Identity/Bypass、访问文件系统或环境，以及使用保留变量和内部 Header 的指令。自定义片段属于高级出口；仓库字段能够表达需求时应优先使用仓库字段。

## 入口接入

此页面展示 Ingress 模式、前端网络/地址和生成的 External Shared Nginx 片段。RepoGate 不安装、编辑或 Reload 共享入口。请审核文件、补全证书占位内容，再通过该入口部署的正常变更流程应用。

## 缓存

缓存页面展示文件数、已扫描字节数、最大容量、全局 Generation、路径/限制，以及仓库命中和未命中流量。确认后使用 **全局逻辑失效** 可失效全部当前 Cache Namespace。

Purge/Reclaim 表将立即完成的逻辑失效与延迟发生的物理回收分开显示。在观测窗口结束并重新扫描磁盘之前，Reclaim 显示 Pending 或 Running 属于正常情况。

## 健康状态

健康页面分别显示 RepoGate、前端端点、External Shared Nginx、Go Router、Managed Upstream Nginx、上游端点和每个仓库。仓库显示 Unknown 通常表示尚未完成成功探测；显示异常时，应结合 **仓库 → 测试**、上游详情和日志调查。

## 访问日志、审计日志与系统

- **访问日志**展示最近的 Managed Upstream Nginx Access 记录，并支持手动刷新。
- **审计日志**记录管理员、客户端地址、操作、对象/详情和成功或失败结果。
- **系统**展示 RepoGate 构建/运行信息、内存与文件描述符计数、Ingress/TLS 端点，以及 Managed Upstream Nginx 的准确校验值和生命周期状态。

使用 Audit Log 确认谁修改了配置；使用 Access Log 调查数据面请求；启动、验证和上游故障应继续查看磁盘上的 Application 与 Nginx Error Log。

## 设置

**设置** 页面可以管理 `config.yaml` 中的大部分运行配置，包括本地 Unix/TCP 端点、入口模式、HTTP/TLS 行为、性能、Metadata、重定向、缓存默认值、安全与管理 CIDR、传输连接池和超时、并发与带宽限制、日志轮转、健康检查调度、退出行为及 Managed Upstream Nginx 生命周期。

选择 **验证并保存** 时，RepoGate 会先严格验证完整的合并配置，再把覆盖值写入 SQLite。保存的 Web UI 值会在 RepoGate 下次启动时覆盖对应 YAML 值，但不会热更新当前进程。保存后请执行页面显示的命令：

```sh
sudo systemctl restart repogate
```

重启后，页面会显示当前进程已匹配保存值。**重启后恢复 YAML** 会删除 Web UI 覆盖；需要再次重启，YAML 值才会生效。

Bootstrap、凭据、文件系统和可执行文件位置仍只能通过配置文件管理，避免运行中的服务迁移自身信任边界或数据库。页面会展示完整保护列表，其中包括 Socket 路径/权限、Runtime 路径、入口片段路径、TLS 证书/私钥路径、数据库/缓存/日志路径、初始管理员配置，以及 Managed Upstream Nginx 的 Binary、Prefix、PID、日志、Socket 与 CA Bundle 路径。

仓库 Desired/Active 变更不属于此页面，仍然会立即验证并激活。

## 用户与我的账号

用户页面可创建额外管理员。用户名要求 3–64 个非空白字符，密码至少 10 位。当前登录管理员不能删除自己的账户。使用 **我的账号**，输入现有密码与新密码即可修改当前密码。

本版本所有管理员账户拥有相同 UI 权限。应使用独立账户，让审计记录能够识别操作人，并删除不再需要的账户。

## 故障排查

| 现象 | 检查项 |
|---|---|
| 登录短暂成功后又回到登录页 | 使用 HTTPS Ingress 地址；确认浏览器允许同源 Cookie，且 UI/API 未拆分到不同 Origin |
| `401 authentication required` | Session Cookie 不存在、已过期或已被清除；通过 HTTPS 重新登录 |
| 登录前直接收到 `403 forbidden` | 有效客户端地址不在 `security.admin_cidrs` 内 |
| 仓库停留在 Desired/Failed | 打开详情查看验证错误，再检查生成配置和 Managed Upstream Nginx 状态 |
| 上游异常 | 执行测试；核对 URL、DNS、CA Bundle、预期状态、私网/HTTP 权限和 Redirect Host |
| 目录页面缺少图标或样式 | 启用 **改写可浏览 HTML 中的同源 URL** 并激活仓库，再应用更新后的 External Shared Nginx `/_repogate/upstream/` Location |
| Purge 完成但磁盘占用未立即下降 | 逻辑失效已立即完成；等待异步物理回收和下一次占用扫描 |
| 升级后仓库操作按钮没有反应 | 对已配置的 `admin.path` 强制刷新一次以加载当前内置脚本，再查看页面错误与 Audit Log |
| 复制按钮失败 | UI 会自动使用本地复制回退；如果浏览器仍阻止复制，请从展示值手动复制 |
| 保存的设置没有影响当前进程 | 重启 RepoGate，并确认页面不再提示有待应用的保存值 |

主机级排查请继续阅读[配置参考](configuration.zh-CN.md)、[安装文档](installation.zh-CN.md)和[验证与生产验收](verification.zh-CN.md)。
