# Docker 与 OCI 镜像代理指南

[English](docker-oci.md) | [简体中文](docker-oci.zh-CN.md)

MirrorRelay 为 Docker 及符合 OCI 分发规范的容器镜像注册表（Container Registry）提供安全、高性能的按需拉取代理（Pull-through Proxy），无需搭建和维护庞大的全量镜像库。

---

## 1. OCI 镜像代理工作流程

容器镜像拉取遵循 OCI Distribution Specification 规范，拉取过程包含三个核心阶段：

```text
Docker 客户端                 MirrorRelay                  上游 Registry (如 Docker Hub)
     │                             │                                       │
     │ 1. GET /v2/                 │                                       │
     ├────────────────────────────>│                                       │
     │ 2. 401 Www-Authenticate     │ (Token Broker 请求鉴权服务器获取 Token) │
     │<────────────────────────────┤──────────────────────────────────────>│
     │                             │                                       │
     │ 3. GET /v2/<image>/manifests/<tag>                                  │
     ├────────────────────────────>│ (流式转发并缓存 Manifest)              │
     │                             ├──────────────────────────────────────>│
     │ 4. Manifest 响应            │                                       │
     │<────────────────────────────┤                                       │
     │                             │                                       │
     │ 5. GET /v2/<image>/blobs/<digest>                                   │
     ├────────────────────────────>│ (检查磁盘缓存 / 流式拉取分层 Blob)     │
     │                             ├──────────────────────────────────────>│
     │                             │ (安全跟踪 S3/CDN 302 重定向签名地址)   │
     │ 6. 镜像层 Blob 数据         │                                       │
     │<────────────────────────────┤                                       │
```

---

## 2. 核心技术能力

### A. 容器镜像 Token Broker (鉴权中继)
公有和私有镜像中心通常要求客户端在独立的鉴权服务器（如 `auth.docker.io`）用凭据换取 Bearer Token。MirrorRelay 内置 **Token Broker** 代理服务：
- 自动捕获上游返回的 `Www-Authenticate: Bearer realm="...",service="...",scope="..."` 挑战头。
- 使用管理员在后台配置的上游凭证（或匿名 Token）向上游鉴权端点申请具有相应作用域的 Bearer Token。
- 在后续上游请求中自动注入 Token，同时**完全对下游客户端隐藏上游核心凭据**。

### B. 重定向安全代理 (S3 / CloudFront / GCS)
主流镜像中心常将镜像分层存储在对象存储上，并在下载 Blob 时返回 HTTP `302/307` 预签名重定向地址：
- **直通模式 (Pass-through)**：经安全验证后将重定向地址直接返回给客户端。
- **内部代理模式 (Follow/Proxy)**：在 Managed Upstream Nginx 内部直接跟踪重定向链接下载 Blob，并沉淀到本地磁盘缓存，后续拉取直接从本地命中，无需重复消耗公网带宽。
- **SSRF 安全防护**：对每个重定向目标域名与解析 IP 严格执行 CIDR 黑名单校验，杜绝内网穿透风险。

### C. 镜像层 Blob 强校验与去重缓存
- 镜像层（Layer Blob）由 SHA-256 Digest 唯一定位 (`sha256:7b...`)。
- 相同基础镜像层（如 `alpine` 或 `ubuntu` 基础层）在不同业务镜像间天然共享，实现最大化磁盘空间节约。

---

## 3. 客户端配置示例

### A. Docker Daemon (`/etc/docker/daemon.json`)

将 MirrorRelay 配置为 Docker Hub 的镜像代理：
```json
{
  "registry-mirrors": [
    "http://mirror.example.com"
  ]
}
```
重启 Docker 守护进程生效：
```bash
sudo systemctl restart docker
docker pull ubuntu:24.04
```

### B. Podman (`/etc/containers/registries.conf`)

```ini
[[registry]]
prefix = "docker.io"
location = "mirror.example.com"
```

### C. Containerd (`/etc/containerd/config.toml`)

```toml
[plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
  endpoint = ["http://mirror.example.com"]
```

---

## 4. 当前功能状态与分布式限制说明

- **单节点拉取代理 (Standalone)**：✅ 当前 MirrorRelay 版本完整支持。
- **分布式集群镜像路由**：🚧 在多节点分布式模式下，软件包仓库路由通过 HTTP 307 重定向到 Edge 节点。针对 OCI 容器镜像的分布式路由目前已设为保留（返回 HTTP 501），待未来版本实现分布式 Manifest 与分层控制面后再行开放。在 v0.0.x 版本中推荐采用单节点独立模式部署容器镜像代理。
