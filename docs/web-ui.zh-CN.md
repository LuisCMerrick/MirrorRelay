# Web UI 使用指南

[English](web-ui.md) | [简体中文](web-ui.zh-CN.md)

MirrorRelay 在 `admin.path` 下内置管理后台，默认值为 `/admin/`；同源 API 位于 `<admin.path>api/v1/`。Web UI 可管理仓库、模板、Managed Upstream Nginx、缓存操作、健康状态、日志和管理员账户。

共享域名的公开根路径 `/` 是一个轻量仓库索引，会列出当前客户端可见的全部已启用仓库，并把 Path Mode 仓库链接到其公开路径，但不会公开管理后台路径。配置为 Host Mode 的独立域名仍由该仓库占用 `/`。生成的 External Shared Nginx 片段包含精确的根索引位置和已配置后台位置，同时不会接管其他无关路径。

## 访问与登录

请使用 External Shared Nginx 发布的 HTTPS 地址，例如：

```text
https://repo.example.com/admin/
```

只能在 `config.yaml` 中修改 `admin.path`，修改后应重启 MirrorRelay，并更新已审核的 External Shared Nginx 片段。路径会自动补齐末尾 `/`，也可以包含多个安全路径段。非默认路径可以减少无目的扫描发现，但不能替代身份验证；HTTPS、管理员凭据与 `security.admin_cidrs` 仍是必要控制。

Session Cookie 带有 `Secure`、`HttpOnly` 和 `SameSite=Strict` 属性。因此生产登录必须使用 HTTPS，UI 与 API 也必须保持同源。非回环主机上的明文 HTTP 地址可能接受登录请求，但浏览器不会保留 Session Cookie。不要把受信的前端 TCP Listener 或可选 Unix Socket 直接暴露到不受信任网络。

数据库没有任何用户时，登录页会切换为一次性的初始管理员注册页。第一个成功注册的账号会成为唯一初始 Admin 并立即登录；数据库原子条件可防止两个并发请求同时注册成功。该端点仍受 `admin.host` 与 `security.admin_cidrs` 保护。请先限制管理面访问范围并完成注册，再向不受信任网络开放。只要已有任意用户，注册入口就会永久关闭，除非管理员显式清除持久数据库。

APT 仓库详情与内置帮助同时提供现代 DEB822（`/etc/apt/sources.list.d/*.sources`）和传统 `sources.list` 单行格式。发行版/版本与输出格式是两个独立选项，因此同一 Debian 或 Ubuntu 客户端可以自由使用任一格式。

除非浏览器首选语言包含中文，否则 UI 默认使用英语。右上角的 `EN` 或 `中文` 可手动切换，选择会保存在浏览器 Local Storage。语言资源与页面功能解耦，独立维护在语言包资源文件（`locales/en.js` 与 `locales/zh.js`）中。使用完毕后，从侧边栏底部退出登录。

## 运行模型

仓库与自定义配置变更遵循以下激活流程：

```text
编辑 Desired 状态 -> 生成 Candidate -> 使用 nginx -t 验证
                   -> 原子发布 -> Graceful Reload -> Active 状态
```

验证或 Reload 失败时，之前的 Active 路由配置继续工作。因此 Repositories 页面可能同时显示失败的 Desired 状态和仍在服务的最后一个有效 Active 状态。重试前应查看仓库错误、生成配置和 Managed Upstream Nginx 状态。

## 界面层级规范

管理界面的所有页面遵循同一套层级：

- 首屏保留 3–5 个与当前判断直接相关的状态指标，避免在多个概览页重复铺开构建和运行时信息。
- 技术标识、端点内部信息和低频维护操作放入原生折叠区或二级详情窗口；除当前页面的即时任务外，默认保持折叠。
- 每个流程只突出一个主要操作；查看类操作使用中性次级按钮，危险样式仅用于破坏性操作。
- 生成配置默认隐藏；敏感或生效配置仅在有权限的用户主动请求时加载，复制按钮与展开后的内容放在一起。
- 亮色和暗色主题都保留清晰的键盘焦点、语义化 `details`/`summary` 行为，并尊重减少动态效果的系统偏好。

## 建议的首次使用顺序

以下流程假设当前使用 Admin 账户；其他角色不会看到受权限限制的页面与配置预览。

1. 打开 **系统**，确认 MirrorRelay 标识与本地端点。
2. 打开 **受管上游 Nginx → 技术详情**，确认其版本、Build ID 和架构。
3. 打开 **健康状态**，确认 MirrorRelay、前端端点、Go Router、Managed Upstream Nginx 与上游端点正常或正在运行。
4. 打开 **入口接入**，展开并审核生成片段，再通过现有入口管理员的正常流程应用。
5. 新增一个仓库，测试上游并检查生成配置。
6. 打开公开域名根路径，确认仓库已出现在索引中。
7. 使用仓库详情中的客户端示例完成一次小请求，再接入生产流量。

## 概览

概览首屏聚焦仓库健康、当前请求/流量总量和缓存命中率。静态请求路径拓扑默认折叠，仅在排查部署链路时按需展开。

概览还提供：

- 仓库总数与启用数；
- 正常与异常仓库数；
- 当前活动请求，以及今日、24 小时和 7 天的请求量与流量；
- 缓存命中率和已观测缓存占用；
- 各仓库流量、状态码分类、缓存命中/未命中和上游错误；
- 小时流量图表及 HTTP/缓存分类。

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

Path 模式应填写 `/debian/` 这类公开路径。MirrorRelay 会拒绝等于或包含后台/系统路径、与其他仓库路径重叠，或替换根索引的路径；也会拒绝重复公开 Host。配置 `http.public_base_url` 后，Host Mode 仓库还不能占用共享 Host。Host 模式应填写独立公开域名，并在 External Shared Nginx 中补齐 TLS/Server Block。只有 `security.admin_cidrs` 已定义预期客户端时，才应选择 **仅管理 CIDR**。

**改写可浏览 HTML 中的同源 URL** 是逐仓库兼容开关，默认关闭。它会按照实际上游页面 URL 解析 HTML 中的 `href`、`src`、`srcset`、`action` 及相关 URL 属性。仍在上游仓库 Base 内的目标会映射回公开仓库 Namespace；位于 Base 外但与上游同源的目标会获得不透明的签名 URL：`/_mirrorrelay/upstream/<仓库ID>/<上游ID>/<签名>/<目标>`，从而继续提供上游原有图标、样式表、脚本和图片。跨 Origin URL 与非 HTTP(S) Scheme 保持不变；包含 data URL 的混合 `srcset` 会逐候选处理。

辅助路由只接受 `GET`、`HEAD`。HMAC 将仓库、生成源 HTML 时实际选中的上游、Host 策略、路径与 Query 绑定，修改任一部分都会被拒绝。路由继续受仓库公开 Host 与访问策略约束，并通过 Managed Upstream Nginx 复用已经 Pin 的上游、TLS 校验、Header、缓存策略和限制；签名密钥只生成一次并保存在 MirrorRelay 数据库中。存在已启用且需要该能力的 Path Mode 仓库时，生成的 External Shared Nginx 片段会包含 `/_mirrorrelay/upstream/`，请把更新后的片段应用到共享入口。HTML Body 使用仓库 Metadata 改写上限、压缩策略和重新生成的响应 Validator。

选择 **验证、保存并生效** 提交 Candidate。成功表示生成、验证、持久化、原子发布和 Graceful Reload 全部完成。错误会留在表单中，不会替换 Active 配置。

仓库静态请求 Header 值与 Token Endpoint 属于可能携带凭据的字段，只有 Admin 可查看和编辑。Operator 响应只显示脱敏 Sentinel；Operator 编辑时必须原样保留这些由 Sentinel 代表的既有值，新增、删除或轮换都会被拒绝。仓库存在凭据时，修改其 Upstream/Host、路由、公开暴露、包过滤、认证缓存或 Pull-only 策略也只允许 Admin 执行。

### 仓库操作

| 操作 | 结果 |
|---|---|
| 详情 | 展示 Desired/Active 状态、计数器、上游健康和客户端示例 |
| 复制地址 | 复制公开仓库 URL；浏览器可能要求 Clipboard 权限 |
| 测试 | 对全部启用上游运行已配置健康检查 |
| 配置 | 仅 Admin 可预览 Candidate 中该仓库生成的 Nginx 配置 |
| 清缓存 | 逻辑失效整个仓库或一个可选对象路径 |
| 编辑 | 打开完整仓库表单 |
| 启用 / 禁用 | 使用所选状态验证并激活新的 Desired 配置 |
| 删除 | 删除仓库并逻辑失效其缓存；此操作不可撤销 |

详情窗口提供生成的客户端命令，以及存在新模板时的升级预览；Admin 还可按需请求完整生成配置。选择 **应用升级** 前，应审核逐字段 Diff，并在有权限时审核生成配置。

列表和详情窗口中的操作都通过同源 API 完成；完成后会显示成功或错误消息，请求运行期间当前按钮会暂时禁用。**复制地址** 会优先使用浏览器 Clipboard API；浏览器拒绝 Clipboard API 时，自动回退到本地文本选择复制。

Cache Purge 使用 Generation。新请求会立即停止使用已失效 Namespace；旧物理文件会继续存在，直到异步 Nginx Cache Manager 按 `inactive` 与 `max_size` 回收。

## 模板

模板是针对各仓库生态的只读、带版本起点。页面展示模板名称、版本、仓库类型、默认上游、发布/代理模式，以及是否启用缓存或 Metadata 改写。选择模板不会让以后发布的新模板版本自动应用。

## Managed Upstream Nginx

首屏只展示进程状态、运行时间、当前配置版本和配置历史。点击 **技术详情** 会打开二级视图，其中集中展示 PID、启动时间、二进制版本、Build ID、架构、SHA-256、配置哈希、Reload/退出信息和 Nginx 编译参数。

生效 Nginx 配置仅 Admin 可读，默认隐藏，并且只有展开 **生效配置** 后才会向 API 请求。Operator 与 Viewer 仍可查看各自获准的状态/历史，但不会看到配置控件；非 Admin 状态/历史还会省略进程 PID、生成的接入片段及原始验证/生命周期诊断，仓库 Test 输出也会脱敏携带凭据的 URL 部分与原始连接错误。

- **重新生成、验证并 Reload**：对当前 Desired 仓库与自定义片段执行 Reconcile。
- **回滚**：从所选不可变历史版本恢复仓库与自定义配置，验证后执行 Graceful Reload。

回滚不只是替换一份 Nginx 文本；它会把持久化仓库和自定义配置状态恢复到所选快照。

## 自定义配置

自定义片段属于代码级变更，因此对应页面与 API 仅 Admin 可用。片段可以绑定受控的 `http`、`server`、`location`、`upstream` 或 Repository Context。Repository ID 为 `0` 表示全局片段。保存或删除片段时，会先生成并验证完整 Candidate，再进行激活。

MirrorRelay 会拒绝能够逃离所选 Context、创建 Listener/Route/Upstream Target、削弱 TLS 验证、改变 Cache Identity/Bypass、访问文件系统或环境，以及使用保留变量和内部 Header 的指令。自定义片段属于高级出口；仓库字段能够表达需求时应优先使用仓库字段。

## 入口接入

此页面仅限 Admin，展示 Ingress 模式和前端端点。生成的 External Shared Nginx 片段默认隐藏，展开后方可审核和复制。MirrorRelay 不安装、编辑或 Reload 共享入口；请补全证书占位内容，再通过该入口部署的正常变更流程应用。

## 缓存

缓存页面优先展示用量、文件数、预热状态及仓库命中/未命中流量。预热任务、定向失效、存储限制、全局 Generation 和 **全局逻辑失效** 统一放入默认折叠的维护区域；确认后执行全局操作可失效全部当前 Cache Namespace。

Purge/Reclaim 表将立即完成的逻辑失效与延迟发生的物理回收分开显示。在观测窗口结束并重新扫描磁盘之前，Reclaim 显示 Pending 或 Running 属于正常情况。

## 健康状态

健康页面优先显示 MirrorRelay、Managed Upstream Nginx 和仓库健康汇总。展开 **组件与端点详情** 后可查看前端端点、External Shared Nginx、Go Router 和上游端点。精确的本地 Network/Socket 坐标仅向 Admin 显示；较低权限角色只能看到组件状态，不会收到文件系统/监听详情。仓库显示 Unknown 通常表示尚未完成成功探测；显示异常时，应结合 **仓库 → 测试**、上游详情和日志调查。

## 访问日志、审计日志与系统

- **访问日志**仅向 Admin/Operator 展示最近的 Managed Upstream Nginx Access 记录，并支持手动刷新；该日志不写入 Query 字符串。
- **审计日志**记录管理员、客户端地址、操作、对象/详情和成功或失败结果。
- **系统**只向 Admin/Operator 开放，首屏展示运行时间、RSS 和 Managed Upstream Nginx 状态；构建标识、Go 运行时计数器和 Nginx 生命周期按二级折叠区分组。只有 Admin 会收到 Ingress/TLS/监听/Socket 路径等敏感端点详情。准确的 Nginx 二进制与编译信息集中在 Nginx 页面的 **技术详情** 中。

Viewer 可读取 Audit Log，但不能访问 Managed Upstream Nginx Access Log。失败条目的诊断详情可能包含内部运行上下文，因此只向 Admin 展示；低权限角色仍可看到操作人、动作、对象、结果和时间。使用 Audit Log 确认谁修改了配置；Admin/Operator 可使用不含 Query 的 Access Log 调查数据面请求；启动、验证和上游故障应继续查看磁盘上的 Application 与 Nginx Error Log。

## 设置

仅限 Admin 的 **设置** 页面提供对 `config.yaml` 全部 20 个配置块的完整管理能力：服务器与运行时端点、入口模式、独立 HTTP/TLS、性能、元数据改写适配器、重定向策略、数据库、缓存策略、安全与管理 CIDR、网络传输连接池与超时、并发与带宽限制、日志轮转、健康检查调度、平滑退出、Managed Upstream Nginx 生命周期、分布式集群与路由调度、Webhook 告警通知及缓存预热。

### 生效机制标识

Web UI 明确标注各类配置的生效机制：
- **重启生效**（`[重启生效]`）：涉及进程监听、内存限制、超时时间与后台守护进程参数，在重启 MirrorRelay 后生效。
- **重新加载生效**（`[重新加载生效]`）：Managed Upstream Nginx 路由与模板变更在 Graceful Reload 后生效。
- **立即生效**（`[立即生效]`）：仓库、外观设置与即时缓存失效等操作立即生效。

保存需要重启的配置时，系统会弹出明确提示：
> “配置已保存，需要重启 MirrorRelay 后生效。”

点击 **验证并保存** 会执行启动级严格校验并写入 SQLite 持久化存储。您可以直接在 Web UI 点击 **立即重启** / **重启服务** 按钮，或执行命令：

```sh
sudo systemctl restart mirrorrelay
```

### 配置导出

管理员可导出符合标准规范的 YAML 配置文件：
- **标准导出**（默认）：自动剔除敏感凭据（`distributed.token`、`distributed.mutation_token`、`webhook.secret`、边缘节点密钥）以及本地实例公开访问地址（`http.public_base_url`、`distributed.node.public_base_url`）。
- **完整备份导出**：仅限管理员使用，包含全部密钥与凭据，适用于灾难恢复完整备份。

### 配置导入

**导入配置** 功能支持上传 `.yaml` 文件或直接粘贴 YAML 文本：
1. **启动级严格校验**：校验 YAML 语法、字段规范、取值范围与依赖关系。
2. **差异预览 (Diff)**：以表格形式清晰展示变更字段（`路径`、`当前值`、`导入值`），并提示是否需要重启生效。
3. **本地实例与敏感密钥保护**：若导入的 YAML 中省略了本地实例地址或密钥，自动保留当前运行实例的既有值。
4. **确认应用**：确认后持久化至数据库，并自动生成版本历史记录。

### 配置版本历史与回滚

通过 Web UI、导入、重置或回滚执行的每次配置变更均会记录至版本历史：
- 记录版本号、变更时间、操作人、来源（`web_ui`、`configuration_import`、`settings_rollback`、`settings_reset`）、Diff 差异摘要与安全快照。
- 历史记录中绝不以明文存储敏感凭据。
- 管理员可随时查看历史版本并点击 **回滚** 一键恢复至任意历史配置状态。

### 分布式集群与路由调度设置

设置页面支持管理分布式集群拓扑与流量调度：
- **集群标识与鉴权**：集群角色（`standalone`、`coordinator`、`edge`）、注册通信令牌、变更鉴权密钥及协调节点标识。
- **节点属性**：本地节点名称、公开访问地址、所在区域与国家代码。
- **调度策略**：支持调度模式切换（`hybrid` 混合模式、`cidr` 网段模式、`geo` 地理位置模式、`priority` 优先级模式），并提供 **客户端网络路由映射**（CIDR 至区域）与 **区域国家映射**（区域代码至国家列表）的可视化表格管理。
- **健康检查**：集群心跳周期、超时时间与健康/异常失败判定阈值。

Webhook 区域支持配置单一通知目标，根据 URL 自动识别钉钉、飞书/Lark、企业微信、Slack 或通用 JSON 格式，并内置一键测试通知面板。

## 外观主题与品牌

登录页和管理界面顶栏始终提供“亮色 / 暗色 / 自动”切换。浏览器会在本地保存该偏好；“自动”跟随操作系统的 `prefers-color-scheme`，并在系统主题变化时同步更新。

**外观主题** 页面用于管理实例默认主题、品牌标识、自定义 CSS 与公开仓库目录浏览器设置：

- **主题模式**：为尚无本地偏好的浏览器设置实例默认值，支持 `System`（跟随操作系统）、`Light`（明亮浅色）与 `Dark`（暗黑深色）。
- **公开界面增强**：独立控制公开仓库目录样式增强，不影响管理界面主题切换。
- **主色调**：自定义界面 Accent Color（默认 `#2563eb`）。
- **品牌定制**：设置站点名称/标题、Logo 图标 URL 与 Favicon 图标 URL。
- **登录页面**：自定义登录页主标题与副标题。
- **仓库目录浏览**：启用响应式仓库目录浏览器，提供面包屑导航、即时搜索过滤与内置 SVG 图标。
- **自定义 CSS**：启用外部 CSS 样式表注入（通过 `/ui/custom.css` 安全提供）。
- **恢复默认值**：随时一键恢复初始外观与主题设置。

## 用户与我的账号

管理员可在用户页面创建 Admin、Operator 与 Viewer 账户。用户名要求 3–64 个非空白字符，密码至少 10 位。当前登录用户不能删除自己的账户。使用 **我的账号**，输入现有密码与新密码即可修改当前密码。

界面与 API 使用同一套角色策略，并隐藏当前账户无权使用的控件。Admin 管理用户、仓库凭据、自定义 Nginx 代码、集群节点记录和进程级设置；Operator 管理仓库非敏感字段、缓存、验证、Nginx Reload/回滚及集群检查/同步；Viewer 只获得最小只读运行视图，不能打开 System。应使用独立账户，让审计记录能够识别操作人，并删除不再需要的账户。

生成的客户端配置不会关闭 TLS 证书验证。使用私有 PKI 时，应将组织 CA 加入客户端信任库，不要使用不安全的客户端参数。

## 故障排查

| 现象 | 检查项 |
|---|---|
| 登录短暂成功后又回到登录页 | 使用 HTTPS Ingress 地址；确认浏览器允许同源 Cookie，且 UI/API 未拆分到不同 Origin |
| `401 authentication required` | Session Cookie 不存在、已过期或已被清除；通过 HTTPS 重新登录 |
| 登录前直接收到 `403 forbidden` | 有效客户端地址不在 `security.admin_cidrs` 内 |
| 仓库停留在 Desired/Failed | 打开详情查看验证错误，再检查生成配置和 Managed Upstream Nginx 状态 |
| 上游异常 | 执行测试；核对 URL、DNS、CA Bundle、预期状态、私网/HTTP 权限和 Redirect Host |
| 目录页面缺少图标或样式 | 启用 **改写可浏览 HTML 中的同源 URL** 并激活仓库，再应用更新后的 External Shared Nginx `/_mirrorrelay/upstream/` Location |
| Purge 完成但磁盘占用未立即下降 | 逻辑失效已立即完成；等待异步物理回收和下一次占用扫描 |
| 升级后仓库操作按钮没有反应 | 对已配置的 `admin.path` 强制刷新一次以加载当前内置脚本，再查看页面错误与 Audit Log |
| 复制按钮失败 | UI 会自动使用本地复制回退；如果浏览器仍阻止复制，请从展示值手动复制 |
| 保存的设置没有影响当前进程 | 重启 MirrorRelay，并确认页面不再提示有待应用的保存值 |

主机级排查请继续阅读[配置参考](configuration.zh-CN.md)、[安装文档](installation.zh-CN.md)和[验证与生产验收](verification.zh-CN.md)。
