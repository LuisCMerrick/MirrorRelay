# Docker and OCI Registry Guide

[English](docker-oci.md) | [简体中文](docker-oci.zh-CN.md)

MirrorRelay provides a secure, high-performance pull-through proxy for Docker and OCI-compliant container registries, eliminating the need to maintain full registry mirrors.

---

## 1. How OCI Registry Proxying Works

Container registries adhere to the OCI Distribution Specification. Pulling an image involves three phases:

```text
Docker Client                 MirrorRelay                  Upstream Registry (e.g. Docker Hub)
     │                             │                                       │
     │ 1. GET /v2/                 │                                       │
     ├────────────────────────────>│                                       │
     │ 2. 401 Www-Authenticate     │ (Fetch token via Token Broker)        │
     │<────────────────────────────┤──────────────────────────────────────>│
     │                             │                                       │
     │ 3. GET /v2/<image>/manifests/<tag>                                  │
     ├────────────────────────────>│ (Stream manifest & cache)             │
     │                             ├──────────────────────────────────────>│
     │ 4. Manifest response        │                                       │
     │<────────────────────────────┤                                       │
     │                             │                                       │
     │ 5. GET /v2/<image>/blobs/<digest>                                   │
     ├────────────────────────────>│ (Check disk cache / stream blob)      │
     │                             ├──────────────────────────────────────>│
     │                             │ (Follow S3/CDN 302 redirect safely)   │
     │ 6. Layer blob data          │                                       │
     │<────────────────────────────┤                                       │
```

---

## 2. Key Capabilities

### A. Token Broker
Public and private container registries require clients to exchange credentials for Bearer tokens at a separate auth endpoint (e.g. `auth.docker.io`). MirrorRelay acts as a **Token Broker**:
- Intercepts upstream `Www-Authenticate: Bearer realm="...",service="...",scope="..."` challenges.
- Uses configured upstream credentials (or anonymous tokens) to acquire scoped Bearer tokens on behalf of the client.
- Passes tokens to upstream requests without exposing credentials to downstream clients.

### B. Redirect Broker (S3 / CloudFront / GCS)
Many registries store layer blobs in object storage and return HTTP `302/307` redirects with presigned URLs:
- **Pass-through mode**: Returns the validated redirect location directly to the client.
- **Proxy mode / Follow**: Follows the redirect internally, downloads the blob through Managed Upstream Nginx, and caches it locally so subsequent pulls never hit external cloud egress.
- **SSRF Safety**: Every redirect target host and IP is validated against strict CIDR blacklists before connection.

### C. Layer Blob Caching
- Blobs and layers are content-addressed by SHA-256 digest (`sha256:7b...`).
- Cached permanently or according to configured cache expiration profiles.
- Zero duplicate downloads across images sharing common base layers.

---

## 3. Client Configuration

### A. Docker Daemon (`/etc/docker/daemon.json`)

To use MirrorRelay as a default mirror for Docker Hub:
```json
{
  "registry-mirrors": [
    "http://mirror.example.com"
  ]
}
```
Restart Docker:
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

## 4. Current Status & Distributed Limitations

- **Single-Node Pull Proxy**: ✅ Fully supported in current MirrorRelay releases.
- **Distributed OCI Routing**: 🚧 In multi-node distributed mode, package repository routing uses HTTP 307 redirects to edge nodes. OCI Registry distributed routing is currently disabled (returns HTTP 501) pending future control-plane updates for distributed manifest and blob coordination. Single-node standalone deployment is the recommended configuration for Docker registries in v0.0.x.
