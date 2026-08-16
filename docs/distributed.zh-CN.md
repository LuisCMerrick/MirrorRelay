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
  - 在所有 Edge 节点异常时自动回退到本地数据平面服务，保障业务连续性。

### Edge 边缘节点
- **角色定位**：区域高速缓存加速与拉取节点。
- **核心职责**：
  - 承接来自 Coordinator 重定向的客户端数据流量，就近提供高速下载服务。
  - 维护独立的受管上游 Nginx 磁盘缓存空间。
  - 定期向 Coordinator 暴露健康探针指标与配置指纹。
  - 在与 Coordinator 发生网络抖动或临时离线时，仍可基于本地缓存自主对外提供服务。

---

## 3. 流量调度策略

Coordinator 按照以下优先级依次计算路由调度策略：

1. **客户端 IP / CIDR 匹配**：将特定内网或公网 IP 网段（如 `10.0.0.0/8` 或 `192.168.1.0/24`）的客户端精准指派至指定边缘节点。
2. **Geo 地区与国家代码匹配**：利用区域标签（如 `us-east`、`eu-west`、`cn-shanghai`）将请求导流至地理位置最近的节点。
3. **优先级与权重负载均衡**：在多个候选节点中优先选择高优先级节点，或按权重比例分流。
4. **健康度与指纹一致性过滤**：自动剔除未通过健康探针或配置指纹不一致的异常节点。

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
    mode: hybrid              # hybrid, cidr, geo, priority, weight
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
