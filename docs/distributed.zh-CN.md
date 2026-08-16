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
          │  - 自主受管上游 Nginx 数据平面   │ │  - 自主受管上游 Nginx 数据平面   │
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
  - 维护独立的受管上游 Nginx 磁盘缓存空间。
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
1. **健康度与指纹一致性过滤**：探针未通过（`health_status != "healthy"`）或配置指纹不一致的异常节点会被自动剔除。
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
  token: "secret-shared-cluster-token-32bytes"
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
      region: "cn-east"
      country: "CN"
      priority: 100
      weight: 10
      enabled: true
    - name: "Edge Guangzhou"
      url: "https://edge-gz.example.com"
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
  token: "secret-shared-cluster-token-32bytes"
  node:
    name: "Edge Shanghai"
    public_base_url: "https://edge-sh.example.com"
    region: "cn-east"
    country: "CN"
```

---

## 5. 安全与隔离规范

- **集群通信双向鉴权**：Coordinator 与 Edge 节点间的探针交互（`/api/v1/cluster/manifest`、`/api/v1/cluster/health`）必须携带加密集群 Token 并经由常量时间比较校验。
- **探针 SSRF 安全**：节点探针在发起 HTTP 请求前严格校验解析 IP，默认禁止非授权私网地址。
- **缓存数据物理隔离**：各边缘节点缓存独立落盘，互不污染。
