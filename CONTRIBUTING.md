# Contributing to MirrorRelay

Thank you for your interest in contributing to MirrorRelay! We welcome bug reports, feature suggestions, documentation improvements, and code contributions.

---

## Development Setup

### Prerequisites
- Go 1.26.6+
- Node.js (for syntax checking `node --check`)
- Docker (for building musl Managed Upstream Nginx binaries via Buildx)

### Running in Development Mode
```bash
git clone https://github.com/LuisCMerrick/MirrorRelay.git
cd MirrorRelay
go run ./cmd/mirrorrelay -dev
```

Open `https://127.0.0.1:8443/admin/` and complete the one-time initial-administrator registration. Development mode does not create default credentials.

---

## Code & Quality Standards

### Go Guidelines
- Format all code with `gofmt`.
- Write identifiers, comments, and log messages in English.
- Use standard Go error wrapping (`fmt.Errorf("...: %w", err)`).
- Preserve package boundaries and adhere to pure Go CGO-free architecture (`CGO_ENABLED=0`).

### Web UI Guidelines
- Keep the Web UI zero-dependency (vanilla JavaScript, HTML5, and CSS).
- Maintain bilingual resource decoupling: all user-facing strings must be defined in both `internal/web/dist/locales/en.js` and `locales/zh.js`.
- Always validate JavaScript syntax: `find internal/web/dist -name "*.js" -exec node --check {} +`.

### Documentation Parity
- When modifying user-visible features, configuration options, or APIs, always update both the English documentation (`docs/*.md`) and the paired Chinese translation (`docs/*.zh-CN.md`).

---

## Testing & Verification

Before submitting a pull request, ensure the complete test and linting suite passes:

```bash
# 1. Run formatting, vet, and unit tests
make check

# 2. Run race detector across all packages
go test -race -p 1 -count=1 ./...

# 3. Test cross-compilation for both supported architectures
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/mirrorrelay
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/mirrorrelay

# 4. Optional: Run integration test with local Managed Upstream Nginx fixture
MIRRORRELAY_TEST_UPSTREAM_NGINX="$PWD/nginx/sbin/nginx" \
  go test ./internal/upstreamnginx -run '^TestRealManagedUpstreamNginx' -count=1
```

---

## Reporting Issues

- **Bug Reports**: Use the [Bug Report Template](.github/ISSUE_TEMPLATE/bug_report.md).
- **Feature Requests**: Use the [Feature Request Template](.github/ISSUE_TEMPLATE/feature_request.md).
- **Security Disclosures**: Please follow our [Security Policy](.github/SECURITY.md).
