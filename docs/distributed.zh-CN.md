# 分布式集群部署指南

[English](distributed.md) | [简体中文](distributed.zh-CN.md)

MirrorRelay 原生支持分布式集群部署能力，允许构建由中心 Coordinator 控制节点与多台跨地域 Edge 边缘节点组成的分布式镜像加速网络。

---

## 1. 集群架构与数据流

```text
                                  ┌───────────────────────────────┐
                                  │       客户端请求流量           │
                                  │ (apt / dnf / pip / npm 等)    │
                                  └───────────────┬───────────────┘
                                                  │
                                                  │ 1. GET /debian/...
                                                  ▼
                                  ┌───────────────────────────────┐
                                  │     Coordinator 控制中心      │
                                  │   - 权威仓库配置与版本指纹    │
                                  │   - Edge 节点健康检查与探测   │
                                  │   - 智能路由与调度决策引擎    │
                                  └───────┬───────────────┬───────┘
                                          │               │
                 2. HTTP 307 重定向       │               │ 2. HTTP 307 重定向
               (至 Edge 1 华东节点)       │               │ (至 Edge 2 华南节点)
                                          ▼               ▼
          ┌──────────────────────────────────┐ ┌──────────────────────────────────┐
          │        Edge 节点 1 (华东)        │ │        Edge 节点 2 (华南)        │
          │  - Managed Upstream Nginx 数据面 │ │  - Managed Upstream Nginx 数据面 │
          │  - 本地独立磁盘高速缓存          │ │  - 本地独立磁盘高速缓存          │
          │  - 客户端直接拉取与回源          │ │  - 客户端直接拉取与回源          │
          └────────────────┬─────────────────┘ └────────────────┬─────────────────┘
                           │                                    │
                           ▼                                    ▼
                 原始上游软件包服务器                  原始上游软件包服务器
```

---

## 2. 集群角色职责

### Coordinator 控制中心
- **角色定位**：全局配置权威中心与智能流量调度器。
- **核心职责**：
  - 通过 Web UI / API 统一管理全局仓库与模板配置。
  - 主动定时轮询探测所有 Edge 节点的可用性与配置版本指纹。
  - 根据客户端请求特征执行路由匹配并下发 HTTP `307 Temporary Redirect` 重定向。
  - 当无任何健康 Edge 节点可用时返回 `HTTP 503 Service Unavailable`（`error: no_available_edge`，Coordinator 自身严格作为控制面，不执行上游数据回源）。

### Edge 边缘节点
- **角色定位**：区域高速缓存加速与拉取节点。
- **核心职责**：
  - 承接来自 Coordinator 重定向的客户端数据流量，就近提供高速下载服务。
  - 维护独立的 Managed Upstream Nginx 磁盘缓存空间。
  - 定期向 Coordinator 暴露健康探针指标与配置指纹。
  - 在与 Coordinator 发生网络抖动或临时离线时，仍可基于本地缓存自主对外提供服务。

---

## 3. 流量调度策略

`distributed.routing.mode` 控制 Coordinator 如何选取目标边缘节点。支持的合法模式为：
- `hybrid`（默认模式）：优先评估客户端 IP CIDR 与 Geo 区域映射，选取数值最小的优先级节点（数字越小优先级越高），同优先级候选节点按 `weight` 权重比例分流。
- `cidr`：严格依据 `client_networks` 中的 IP CIDR 网段规则匹配边缘节点。
- `geo`：严格依据国家代码与区域映射规则匹配地理最近的边缘节点。
- `priority`：严格选取优先级数值最小的节点（例如优先级 1 优于优先级 100）。

节点候选过滤流程：
1. **健康度与一致性过滤**：健康节点可服务其健康仓库；degraded 节点只能服务明确报告为健康的仓库。缺失、unknown 或 unhealthy 的仓库状态一律不可路由；Protocol、Coordinator 身份/Epoch、Active 配置 Generation 与指纹也必须全部匹配。
2. **能力支持检测**：校验边缘节点是否声明支持当前请求的仓库类型。
3. **权重分流**：同优先级多个候选节点根据各节点的 `weight` 权重比例平滑分流。

若所有候选节点均不可用，Coordinator 将直接返回 `HTTP 503 Service Unavailable`。

---

## 4. 节点配置示例

### Coordinator 控制节点 (`/etc/mirrorrelay/config.yaml`)

```yaml
distributed:
  enabled: true
  role: coordinator           # standalone, coordinator, edge
  token: "read-only-probe-token-at-least-32bytes"
  mutation_token_key_files:
    - /etc/mirrorrelay/cluster-mutation-token.key
  allow_http: false           # 除非显式开启，否则强制 HTTPS
  node:
    name: "coord-01"
    public_base_url: "https://repo-hub.example.com"
    region: "cn-east"
    country: "CN"
  routing:
    mode: hybrid              # hybrid, cidr, geo, priority
    client_networks:
      - cidr: "10.10.0.0/16"
        region: "cn-east"
      - cidr: "10.20.0.0/16"
        region: "cn-south"
    regions:
      - code: "cn-east"
        countries: ["CN"]
      - code: "cn-south"
        countries: ["HK", "MO"]
  health_check:
    interval: 10s
    timeout: 3s
    healthy_threshold: 2
    unhealthy_threshold: 3
  nodes:
    - name: "Edge Shanghai"
      url: "https://edge-sh.example.com"
      mutation_token: "unique-edge-sh-mutation-token-32bytes"
      region: "cn-east"
      country: "CN"
      priority: 100
      weight: 10
      enabled: true
    - name: "Edge Guangzhou"
      url: "https://edge-gz.example.com"
      mutation_token: "unique-edge-gz-mutation-token-32bytes"
      region: "cn-south"
      country: "CN"
      priority: 100
      weight: 10
      enabled: true
```

### Edge 边缘节点 (`/etc/mirrorrelay/config.yaml`)

```yaml
distributed:
  enabled: true
  role: edge                  # standalone, coordinator, edge
  token: "read-only-probe-token-at-least-32bytes"
  mutation_token: "unique-edge-sh-mutation-token-32bytes"
  coordinator_id: "coord-01"
  allow_http: false
  node:
    name: "Edge Shanghai"
    public_base_url: "https://edge-sh.example.com"
    region: "cn-east"
    country: "CN"
```

启动 Coordinator 前，应创建仅 `mirrorrelay` 服务账户可读、其他用户不可访问的数据库加密密钥文件：

```sh
sudo sh -c 'umask 0027; openssl rand -base64 32 > /etc/mirrorrelay/cluster-mutation-token.key'
sudo chown root:mirrorrelay /etc/mirrorrelay/cluster-mutation-token.key
sudo chmod 0640 /etc/mirrorrelay/cluster-mutation-token.key
```

每个密钥文件必须包含恰好 32 个原始字节，或一个 Base64 编码的 32 字节值。Coordinator 至少需要一个绝对密钥文件路径。逐 Edge Mutation Token 在 SQLite 中只以 AES-256-GCM 认证密文保存；旧版明文记录会在启动时以事务方式完成加密迁移。已有凭据无法解密或未配置密钥环时，启动会按 Fail-Closed 策略失败。应将当前密钥与数据库备份一同安全保管，且绝不能把它复制到 Edge 节点。

轮换时先创建新密钥文件，在列表中把新密钥放在首位、旧密钥放在第二位，然后重启 MirrorRelay。启动过程会把全部密文改用首个密钥加密。该次重启成功后，从列表移除旧路径并再次重启；退役密钥只需随仍依赖它的旧备份保留。

---

## 5. 安全与隔离规范

- **集群凭据分权**：`distributed.token` 是 Manifest/Health 共用的只读探测凭据。每个 Edge 必须使用不同的 `mutation_token` 执行同步与 Purge；它不得等于探测凭据，也不得被其他 Edge 复用。Coordinator 的 Seed/节点记录使用已配置密钥环加密保存与该 Edge 匹配的凭据，API 永不回显，Web UI 编辑时保持空白。只有 Admin 可以创建、编辑、启用、禁用或删除节点记录；Operator 只能执行检查与同步。凭据比较采用常量时间实现。
- **Coordinator 绑定与防重放**：每个 Edge 都要配置 `coordinator_id`。Protocol v2 的同步/Purge Envelope 将该身份绑定到持久化的随机 Coordinator Epoch。Edge 在激活前持久化已接受的最高 Generation 与指纹；旧 Generation、同 Generation 不同指纹及已退役 Epoch 会被拒绝，完全相同且已生效的 Payload 可幂等重试。
- **SSRF 安全**：节点 URL 只能是 Origin，不允许凭据、路径前缀、查询或片段。默认强制 HTTPS；明文 HTTP 必须显式设置 `distributed.allow_http: true`，私网/环回/链路本地目标还必须独立设置 `security.allow_private_upstream: true`。健康检查、配置同步及缓存淘汰在发送 Token 前都执行同一策略，连接只使用策略允许的地址，保留 TLS 主机名验证，并禁止重定向。
- **缓存数据物理隔离**：各边缘节点缓存独立落盘，互不污染。

Protocol v2 有意不兼容 Protocol v1。升级后重新启用路由前，需为 Coordinator 和每个 Edge 配置独立 `distributed.node.name`，为每条 Coordinator Node Seed/记录加入独立 `mutation_token`，并在各 Edge 配置匹配的 `mutation_token` 与 `coordinator_id`。完成 v2 同步且后续探针确认目标仓库健康前，节点不会参与路由。

---

## 6. 集群边缘配置同步与分布式缓存淘汰广播

MirrorRelay 提供了一键式及自动化的多节点边缘配置分发与缓存广播机制：

1. **完整 Active 配置同步**：
   - Coordinator 从完整的 Active 仓库与自定义 Managed Upstream Nginx 配置快照计算权威指纹，绝不会把 Edge 指纹采纳为集群权威。数据库本地 ID/时间戳被排除，仓库关联使用稳定 Slug，自定义内容统一换行符。
   - 通过 Web UI 上的「同步全部节点」按钮或 REST API（`POST /admin/api/v1/cluster/sync`），Coordinator 向每个启用的 Edge 的 `POST /api/v1/cluster/sync/apply` 并发发送完整 Active 仓库与自定义配置快照。
   - Edge 先验证 Protocol、Coordinator 身份/Epoch、单调递增 Generation、Payload 指纹、Capabilities、仓库与路由冲突，再进入正常的 Managed Upstream Nginx Candidate 校验、原子发布与 Graceful Reload 流程。校验或 Reload 失败时保留先前 Active 配置。
   - HTTP 200 本身不代表成功；Coordinator 只接受严格 JSON 中明确的 `applied` 状态以及完全匹配的指纹、协议、Generation 与 Capabilities，之后才把 Edge 记录为已同步。
2. **分布式缓存淘汰广播 (Distributed Cache Purge)**：
   - Coordinator 上的全局、仓库及对象淘汰会先推进本地 Generation，再向每个启用的 Edge 的 `POST /api/v1/cluster/sync/purge` 广播同一 Scope。
   - 每次 Purge 都绑定 Edge 已接受的 Coordinator 身份与 Epoch，每个回执都会被校验。API 响应与单条汇总审计会记录目标数、成功数及逐节点失败（包括部分失败），而本地淘汰仍然有效。
3. **配置漂移告警与可视化巡检**：
   - 有界 Worker Pool 探测 Edge，并验证 Manifest 与 Health 描述同一 Generation/指纹。持久化错误可观测，且不会替换上一次 Durable Routing Snapshot。一旦发生网络分区或配置漂移，自动触发 `config_change` Webhook 告警并在前端高亮展示。
