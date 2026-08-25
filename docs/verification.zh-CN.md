# 验证与生产验收

[English](verification.md) | [简体中文](verification.zh-CN.md)

MirrorRelay 本地测试覆盖路由隔离、Desired/Active 发布、配置验证、数据库 Round Trip、Cache Generation、Metadata Adapter、Registry Challenge 解析、SSRF 地址策略、Unix/TCP 端点、内嵌资源和生成的 Nginx 语法。生产可用性还取决于目标入口、DNS、上游、客户端、文件系统和实际负载。

## 发布检查

请在仓库根目录运行。线上 Release 会构建并验证完整的 amd64 与 arm64 DEB、RPM 和 tar.gz 软件包，生成一个包含 `vendor/`、与架构无关的源码归档，并发布一个 Docker Hub 多平台镜像。以下本地命令会交叉构建两个架构的 Go 二进制，并验证仓库中已提交的 amd64 Managed Upstream Nginx Fixture：

```sh
go mod verify
test -z "$(gofmt -l .)"
go vet ./...
go test -count=1 ./...
go test -race -p 1 -count=1 ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -buildvcs=false -o /tmp/mirrorrelay-amd64 ./cmd/mirrorrelay
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -buildvcs=false -o /tmp/mirrorrelay-arm64 ./cmd/mirrorrelay
find internal/web/dist -name "*.js" -exec node --check {} +
(cd nginx/sbin && sha256sum -c nginx.sha256)
file nginx/sbin/nginx
ldd nginx/sbin/nginx || true
readelf -l nginx/sbin/nginx
MIRRORRELAY_TEST_UPSTREAM_NGINX="$PWD/nginx/sbin/nginx" \
  go test ./internal/upstreamnginx -run '^TestRealManagedUpstreamNginx' -count=1
docker compose config
make vendor-source VERSION=0.0.17 RELEASE_DIR=/tmp/mirrorrelay-source
tar -tzf /tmp/mirrorrelay-source/mirrorrelay-0.0.17-source-with-vendor.tar.gz \
  | grep -F 'mirrorrelay-0.0.17-source/vendor/modules.txt'
```

带 Vendor 的源码包从本次发布对应的精确 Git Commit 导出，再在导出树内执行 `go mod vendor`。托管构建流程会解包并执行 `go list -mod=vendor ./...`，通过后才把它加入 GitHub Release 与 `SHA256SUMS`。

发布容器镜像时，`scripts/prepare-container-context.sh` 会解开两个已验证
tar 包，并在暂存镜像 Context 前检查各自的内部 `SHA256SUMS`。Docker
任务不得重新编译 MirrorRelay 或 Managed Upstream Nginx。推送后，工作流
检查 OCI Index，要求应用 Manifest 精确为 `linux/amd64` 和
`linux/arm64`。amd64 镜像会使用内置配置完整启动，并探测只发布到宿主机
回环的 HTTP 端点。arm64 镜像不使用目标架构模拟运行，而是解出已发布
Filesystem，把两个二进制、`BUILD-INFO` 与配置逐字节同已验证的 arm64
软件包 Payload 比较。独立的原生 arm64 Hosted Runner 会先执行精确的
交叉编译软件包 Payload，并在发布前组装、启动和探测正式 arm64 镜像；
发布后还会重新拉取 Digest 并再次探测。工作流不会配置或使用 QEMU。
SBOM 和来源 Attestation 可以作为额外 `unknown/unknown` Manifest 出现。

发布 Release 前，必须配置 GitHub Actions Repository Variable
`DOCKERHUB_USERNAME` 与 Repository Secret `DOCKERHUB_TOKEN`。任一值缺失时，
工作流会在 Login 前明确失败；凭据不会作为 Workflow Input 接收，也不会写入
仓库配置。

使用版本标签或工作流记录的 Digest 检查已发布结果，并在匹配架构的原生
主机上分别执行 Probe：

```sh
docker buildx imagetools inspect \
  "<dockerhub-namespace>/mirrorrelay:<version>"
# 在原生 amd64 主机上：
docker run --rm --platform linux/amd64 \
  "<dockerhub-namespace>/mirrorrelay:<version>" version --verbose
# 在原生 arm64 主机上：
docker run --rm --platform linux/arm64 \
  "<dockerhub-namespace>/mirrorrelay:<version>" version --verbose
```

amd64 Artifact 必须是 ELF x86-64，arm64 Artifact 必须是 ELF AArch64；两者都不能包含 ELF Interpreter 或非预期的运行时共享库依赖。发行版源码注释不得使用中文；中文 UI 字符串和中文文档是内容，不属于代码注释。

Managed Upstream Nginx 在 amd64 构建 Runner 上使用固定版本的 `xx`/Clang musl 交叉工具链构建两个目标。Nginx 的运行期 Configure Probe 使用明确的 Linux/musl 交叉构建结果，类型尺寸、大小端与 `sys_nerr` 则由目标编译器在编译期判断；补丁集校验值会写入 `BUILD-INFO`。随后由 GitHub 原生 `ubuntu-24.04-arm` Runner 执行精确的 arm64 软件包二进制。配置、编译、集成测试和容器镜像组装均不使用 QEMU 或其它目标架构模拟器。

固定的 OpenSSL 构建启用 `no-quic`，Managed Upstream Nginx 也不构建 Nginx HTTP/3 模块。因此正式二进制会排除未使用的 OpenSSL QUIC 服务端实现，同时保留 HTTPS 上游能力。

可选的真实 Nginx 测试会验证生成配置，并通过待验收的 Managed Upstream Nginx 从私有 HTTPS Origin 流式传输 16 MiB 响应，实际覆盖 CA 信任、证书名称校验、固定目标地址和响应 SHA-256 校验。

## 真实客户端矩阵

使用隔离的测试域名与仓库。每个用例都记录客户端版本、MirrorRelay 配置版本、对象 Digest、状态、耗时、Cache 状态和 Managed Upstream Nginx 日志。

| 领域 | 最低验收 |
|---|---|
| HTTP | GET、HEAD、Range/If-Range、206、304、416、Redirect、ETag、Last-Modified、Content-Length |
| APT | 通过 Path Mode 执行 `apt update` 及软件包下载/安装 |
| RPM | DNF/YUM Metadata 刷新和软件包下载 |
| APK/OPKG | 索引刷新和软件包下载 |
| PyPI | 用 Simple Index 执行 `pip install`，文件 URL 必须继续经过 MirrorRelay |
| npm | Metadata 与 Tarball 安装，改写 URL 必须保持本地闭环 |
| Maven/Go/NuGet/Cargo/Conda | 使用生成的客户端示例完成 Metadata 解析和 Artifact 下载 |
| Registry | Docker 与 Podman Pull；Bearer Token scope/service 保留；Manifest/Blob Digest 相等 |
| Redirect | 同 Host 与白名单 CDN 跳转成功；私网、Loopback、Link-local、CGNAT 和混合 DNS 必须拒绝 |
| Cache | MISS 后 HIT、并发首次填充、全局/仓库/单对象逻辑失效、物理回收状态真实 |
| 配置 | 无效 Candidate 不改变 Active；有效变更和回滚均使用 Graceful Reload |
| 本地端点 | 软件包默认使用前端 `127.0.0.1:9081` TCP 与私有上游 Unix Socket；前端 Unix 必须显式启用，上游 TCP 必须显式关闭 Socket，Docker 的容器通配 Listener 只能发布到宿主机回环 |
| Web UI | 每个仓库操作都能调用对应 API，`/` 列出已启用且可见的仓库，保存的设置在重启后生效 |
| 可浏览 HTML | 开启仓库开关后，相对/根 URL 正确解析，Base 内链接留在公开 Namespace，同源 Base 外资源使用绑定上游的 HMAC URL，伪造路径/Query/上游会失败，data/普通 URL 混合 `srcset` 可用，跨 Origin URL 保持不变 |
| 入口 | 安装或重启 MirrorRelay 时，External Shared Nginx 上的其他既有站点继续服务 |
| 发布镜像 | Docker Hub Index 包含 amd64/arm64，版本元数据与 Release Commit 一致，两个内置 Nginx 均可运行，替换容器后挂载状态仍保留 |

## 大对象与连续性测试

至少测试 1 GiB、5 GiB、10 GiB 不可变对象和一个有代表性的 Registry Layer。每次传输期间：

1. 从 `/metrics` 与 System 页面采集进程 RSS、Go Heap、GC Cycle/Pause、Goroutine、打开 FD、活动请求和吞吐。
2. 确认 Go Heap 不随对象大小成比例增长。
3. 中断一个客户端，确认其上游 Body、Goroutine 和 FD 被释放。
4. 对同一冷对象发起并发请求，确认 Cache Lock 防止 Cache Stampede。
5. 活动下载期间应用 No-op 配置和真实仓库变更，确认 Graceful Reload 行为，并记录每个客户端是否保持连续。
6. 使用 `upstream_nginx.stop_on_mirrorrelay_exit: false` 只重启 MirrorRelay，确认它 Attach 到 Hash 一致的既有 Managed Upstream Nginx。
7. 比较直连上游、代理响应和 Cache HIT 响应的 SHA-256/Digest。

每种对象大小都要保留报告，包括峰值/基线 RSS 与 Heap、分配/GC、吞吐中位数、CPU、FD/Goroutine 变化、Cache MISS/HIT 和客户端结果。不能用小型单元测试推断 10 GiB 行为。

## 安全验收

- 伪造 `X-Mirror-Internal-*` Header，确认它们不会原样到达 Managed Upstream Nginx。
- 确认不受信任 Peer 无法访问受信前端端点。显式使用通配/非回环绑定时，应验证容器端口映射或防火墙只允许 External Shared Nginx，并把其精确 Peer CIDR 加入 `security.trusted_proxy_cidrs`。确认入口使用 `$remote_addr` 覆盖 `X-Real-IP`；不受信任 Peer 即使伪造该 Header，Admin CIDR、Audit 与单 IP 限制仍必须采用 Socket 地址。还应确认客户端提供的 `X-Forwarded-Proto: http` 无法把生成的公开 URL 从 HTTPS 降级。
- 确认格式错误或缺少 Realm 的 Registry Bearer Challenge 返回 502，且不会回退到直接 Token Endpoint。
- 尝试含 Userinfo、非 HTTP(S) Scheme、禁止端口/地址、DNS Rebinding 和非白名单 Host 的 Redirect/Token Target。
- 确认 HTTP/私网上游必须同时具有全局和仓库许可。
- 确认证书、名称或证书链错误均失败关闭；Repository API 不能关闭 TLS 验证。
- 通过名称、Host、路径、Header 和 Custom Config 尝试 Nginx Directive Injection。
- 验证 CSRF、Secure/HttpOnly/SameSite Cookie、Session 过期、登录限流、密码修改和 Audit Client IP。

## 部署签署

本地测试全绿表示实现内部一致，不代表 External Shared Nginx 或上游可用性已经认证。生产签署应包含上述完整矩阵及证据、备份/回滚步骤、磁盘容量阈值，以及负责应用所生成 External Shared Nginx Snippet 的明确责任人。
