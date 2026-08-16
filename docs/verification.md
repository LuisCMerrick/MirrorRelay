# Verification and production acceptance

[English](verification.md) | [简体中文](verification.zh-CN.md)

MirrorRelay's local test suite covers routing isolation, Desired/Active publication, configuration validation, database round trips, cache generations, metadata adapters, Registry challenge parsing, SSRF address policy, Unix/TCP endpoints, embedded assets and generated Nginx syntax. Production readiness additionally depends on the target ingress, DNS, upstreams, clients, filesystem and workload.

## Release checks

Run from the repository root. The hosted release builds and validates complete amd64 and arm64 DEB, RPM and tar.gz packages. The local commands below cross-build both Go binaries and validate the checked-in amd64 Managed Upstream Nginx fixture:

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
```

The amd64 artifact must be ELF x86-64 and the arm64 artifact must be ELF AArch64. Neither may have an ELF interpreter or an unexpected runtime shared-library dependency. A release must not contain source-code comments written in Chinese; Chinese UI strings and Chinese documentation are intentional content, not comments.

Managed Upstream Nginx is compiled in a native-build-platform container with a pinned `xx`/Clang musl cross toolchain. The arm64 job uses QEMU only for the small configure-time runtime probes and the final executable integration checks; compilation itself does not run under emulation.

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
| Web UI | Every repository action reaches its API, `/` lists enabled visible repositories, and saved Settings apply after restart |
| Browsable HTML | With the repository switch on, relative/root URLs resolve correctly, in-base links stay in the public namespace, same-origin out-of-base assets use `/_mirrorrelay/upstream/<id>/`, and cross-origin URLs stay unchanged |
| Ingress | Multiple existing External Shared Nginx sites continue serving while MirrorRelay is installed/restarted |

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
- Send forwarded IP headers over a nonlocal peer and confirm they are ignored; confirm local External Shared Nginx `X-Real-IP` drives admin CIDR, audit and per-IP limits.
- Verify malformed or missing-realm Registry Bearer challenges return 502 and never fall back to a direct token endpoint.
- Attempt redirect/token targets using userinfo, unsupported schemes, forbidden ports/addresses, DNS rebinding and non-allowlisted hosts.
- Verify HTTP/private upstream access requires both global and repository permission.
- Verify upstream certificate, name and chain errors fail closed; TLS verification cannot be disabled in the repository API.
- Attempt Nginx directive injection through names, hosts, paths, headers and custom configuration.
- Verify CSRF rejection, Secure/HttpOnly/SameSite cookies, session expiry, login throttling, password changes and audit client IPs.

## Deployment sign-off

A local green test suite means the implementation is internally consistent; it does not certify an external shared Nginx configuration or upstream availability. Production sign-off should include the completed matrix above, retained test evidence, a backup/rollback procedure, disk-capacity thresholds and an explicit owner for applying the generated External Shared Nginx snippet.
