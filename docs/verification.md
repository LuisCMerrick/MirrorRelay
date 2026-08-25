# Verification and production acceptance

[English](verification.md) | [简体中文](verification.zh-CN.md)

MirrorRelay's local test suite covers routing isolation, Desired/Active publication, configuration validation, database round trips, cache generations, metadata adapters, Registry challenge parsing, SSRF address policy, Unix/TCP endpoints, embedded assets and generated Nginx syntax. Production readiness additionally depends on the target ingress, DNS, upstreams, clients, filesystem and workload.

## Release checks

Run from the repository root. The hosted release builds and validates complete amd64 and arm64 DEB, RPM and tar.gz packages, an architecture-neutral source archive containing `vendor/`, and one Docker Hub multi-platform image. The local commands below cross-build both Go binaries and validate the checked-in amd64 Managed Upstream Nginx fixture:

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

The vendored source archive is generated from the exact Git commit being released, then `go mod vendor` is run inside that exported tree. The hosted workflow extracts the result and runs `go list -mod=vendor ./...` before adding it to the GitHub Release and `SHA256SUMS`.

For a container release, `scripts/prepare-container-context.sh` extracts both
verified tar packages and checks each internal `SHA256SUMS` before staging the
image context. The Docker job must not compile MirrorRelay or Managed Upstream
Nginx. After pushing, it inspects the OCI index, requires exactly
`linux/amd64` and `linux/arm64` application manifests. It starts the native
amd64 image with the bundled configuration and probes the loopback-published
HTTP endpoint. Without target emulation, it then extracts the published arm64
filesystem and byte-compares both binaries, `BUILD-INFO` and the configuration
against the already verified arm64 package payload. A separate native arm64
hosted runner executes the exact cross-compiled package payload and assembles
and probes the formal arm64 image before publication, then pulls and probes the
published digest again. The workflow does not configure or use QEMU. SBOM and
provenance attestations may appear as additional `unknown/unknown` manifests.

Before publishing a release, configure the GitHub Actions repository variable
`DOCKERHUB_USERNAME` and repository secret `DOCKERHUB_TOKEN`. The workflow
fails before login when either value is absent; credentials are never accepted
as workflow inputs or committed configuration.

Inspect the published result with the version tag or recorded digest, and run
each probe on a host with the matching native architecture:

```sh
docker buildx imagetools inspect \
  "<dockerhub-namespace>/mirrorrelay:<version>"
# On a native amd64 host:
docker run --rm --platform linux/amd64 \
  "<dockerhub-namespace>/mirrorrelay:<version>" version --verbose
# On a native arm64 host:
docker run --rm --platform linux/arm64 \
  "<dockerhub-namespace>/mirrorrelay:<version>" version --verbose
```

The amd64 artifact must be ELF x86-64 and the arm64 artifact must be ELF AArch64. Neither may have an ELF interpreter or an unexpected runtime shared-library dependency. A release must not contain source-code comments written in Chinese; Chinese UI strings and Chinese documentation are intentional content, not comments.

Managed Upstream Nginx is compiled on the amd64 build runner with a pinned `xx`/Clang musl cross toolchain for both targets. Nginx runtime configure probes are supplied as explicit Linux/musl cross-build results; type sizes, endianness and `sys_nerr` are derived with target-compiler checks. The patch-set checksum is retained in `BUILD-INFO`. The exact arm64 package binary is then exercised on GitHub's native `ubuntu-24.04-arm` runner. Neither configuration, compilation, integration testing nor container-image assembly uses QEMU or other target emulation.

The pinned OpenSSL build uses `no-quic`, and Managed Upstream Nginx is built without the Nginx HTTP/3 module. This excludes the unused OpenSSL QUIC server implementation from the formal binary while retaining HTTPS upstream support.

The opt-in real-Nginx tests validate generated syntax and stream a 16 MiB response from a private HTTPS origin through the tested Managed Upstream Nginx, including CA trust, certificate-name verification, pinned addressing and response SHA-256 validation.

## Functional client matrix

Use isolated test hostnames and repositories. Record the client version, MirrorRelay configuration version, object digest, status, elapsed time, cache state and Managed Upstream Nginx logs for every case.

| Area | Minimum acceptance |
|---|---|
| HTTP | GET, HEAD, Range/If-Range, 206, 304, 416, redirects, ETag, Last-Modified and Content-Length |
| APT | `apt update` and package download/install through a path-mode repository |
| RPM | DNF/YUM metadata refresh and package download |
| APK/OPKG | Index refresh and package download |
| PyPI | `pip install` with simple-index and file URL closure through MirrorRelay |
| npm | Metadata and tarball install; verify rewritten URLs stay local |
| Maven/Go/NuGet/Cargo/Conda | Metadata resolution and artifact download using generated client examples |
| Registry | Docker and Podman pull, Bearer token scope/service preservation, manifest digest and blob digest equality |
| Redirects | Same-host and allowlisted CDN redirects; reject private, loopback, link-local, CGNAT and mixed DNS results |
| Cache | MISS then HIT, concurrent first fill, global/repository/object logical purge, truthful reclaim states |
| Configuration | Invalid candidate leaves Active state unchanged; valid change and rollback use graceful reload |
| Local endpoints | Package defaults use frontend `127.0.0.1:9081` TCP and the private upstream Unix socket; frontend Unix requires explicit enablement, upstream TCP requires explicit disablement, and Docker publishes its wildcard container listener only on host loopback |
| Web UI | Every repository action reaches its API, `/` lists enabled visible repositories, and saved Settings apply after restart |
| Browsable HTML | With the repository switch on, relative/root URLs resolve correctly, in-base links stay in the public namespace, same-origin out-of-base assets use upstream-bound HMAC URLs, forged path/query/upstream values fail, mixed data/normal `srcset` candidates work, and cross-origin URLs stay unchanged |
| Ingress | Multiple existing External Shared Nginx sites continue serving while MirrorRelay is installed/restarted |
| Release image | Docker Hub index contains amd64/arm64, version metadata matches the release commit, both embedded Nginx binaries run, and mounted state survives replacement |

## Large-object and continuity test

Test at least 1 GiB, 5 GiB and 10 GiB immutable objects plus a representative Registry layer. During each transfer:

1. Capture process RSS, Go heap, GC cycles/pause, goroutines, open FDs, active requests and throughput from `/metrics` and the System page.
2. Confirm the Go heap does not grow proportionally with object size.
3. Interrupt one client and verify its upstream body, goroutine and file descriptor are released.
4. Start concurrent requests for the same cold object and verify cache locking prevents a cache stampede.
5. Apply a no-op configuration and a real repository change during active downloads; confirm graceful reload behavior and document whether each client remained uninterrupted.
6. Restart only MirrorRelay with `upstream_nginx.stop_on_mirrorrelay_exit: false`; confirm it attaches to the existing matching Managed Upstream Nginx configuration.
7. Compare SHA-256/digest values from direct upstream and proxied/cache-hit responses.

For each object size, retain a report containing peak/baseline RSS and heap, allocation/GC observations, median throughput, CPU usage, FD/goroutine deltas, cache MISS/HIT behavior and client result. Do not infer 10 GiB behavior solely from a small unit test.

## Security acceptance

- Submit forged `X-Mirror-Internal-*` headers and confirm they never reach Managed Upstream Nginx unchanged.
- Confirm the trusted frontend endpoint is unreachable from untrusted peers. For an explicit wildcard/non-loopback bind, verify the container port mapping or firewall admits only External Shared Nginx and configure its exact peer CIDR in `security.trusted_proxy_cidrs`. Confirm the ingress overwrites `X-Real-IP` with `$remote_addr`; a request from an untrusted peer with a spoofed header must still use its socket address for Admin CIDR, audit and per-IP limits. Also confirm a client-supplied `X-Forwarded-Proto: http` cannot downgrade generated public URLs from HTTPS.
- Verify malformed or missing-realm Registry Bearer challenges return 502 and never fall back to a direct token endpoint.
- Attempt redirect/token targets using userinfo, unsupported schemes, forbidden ports/addresses, DNS rebinding and non-allowlisted hosts.
- Verify HTTP/private upstream access requires both global and repository permission.
- Verify upstream certificate, name and chain errors fail closed; TLS verification cannot be disabled in the repository API.
- Attempt Nginx directive injection through names, hosts, paths, headers and custom configuration.
- Verify CSRF rejection, Secure/HttpOnly/SameSite cookies, session expiry, login throttling, password changes and audit client IPs.

## Deployment sign-off

A local green test suite means the implementation is internally consistent; it does not certify an external shared Nginx configuration or upstream availability. Production sign-off should include the completed matrix above, retained test evidence, a backup/rollback procedure, disk-capacity thresholds and an explicit owner for applying the generated External Shared Nginx snippet.
