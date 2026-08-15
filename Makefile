.PHONY: build check package release release-repogate test test-integration test-race upstream-nginx-musl upstream-nginx-musl-amd64 upstream-nginx-musl-arm64 verify-package

VERSION ?= 0.0.1
ARCH ?= amd64
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || printf unknown)
SOURCE_DATE_EPOCH ?= $(shell git show -s --format=%ct HEAD 2>/dev/null || date +%s)
BUILD_TIMESTAMP ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_ID ?= repogate-$(VERSION)-linux-$(ARCH)-$(shell printf '%s' "$(GIT_COMMIT)" | cut -c 1-12)
UPSTREAM_NGINX_DIR ?= dist/upstream-nginx-linux-$(ARCH)
RELEASE_DIR ?= dist/release
GIT_COMMIT := $(GIT_COMMIT)
SOURCE_DATE_EPOCH := $(SOURCE_DATE_EPOCH)
BUILD_TIMESTAMP := $(BUILD_TIMESTAMP)
BUILD_ID := $(BUILD_ID)
GO_LDFLAGS = -s -w -X main.version=$(VERSION) -X main.gitCommit=$(GIT_COMMIT) -X main.buildTimestamp=$(BUILD_TIMESTAMP) -X main.buildID=$(BUILD_ID)

build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -trimpath -buildvcs=false -ldflags="$(GO_LDFLAGS)" -o bin/repogate ./cmd/repogate

test:
	go test ./...

test-race:
	go test -race -p 1 ./...

test-integration:
	REPOGATE_TEST_UPSTREAM_NGINX="$(abspath $(UPSTREAM_NGINX_DIR))/nginx" go test ./internal/upstreamnginx -run '^TestRealManagedUpstreamNginx' -count=1

check:
	test -z "$$(gofmt -l .)"
	go mod verify
	go vet ./...
	go test ./...
	node --check internal/web/dist/app.js

release-repogate:
	mkdir -p dist/repogate-linux-$(ARCH)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -trimpath -buildvcs=false -ldflags="$(GO_LDFLAGS)" -o dist/repogate-linux-$(ARCH)/repogate ./cmd/repogate

upstream-nginx-musl:
	./scripts/build-upstream-nginx-musl.sh $(ARCH) $(UPSTREAM_NGINX_DIR)

upstream-nginx-musl-amd64:
	$(MAKE) upstream-nginx-musl ARCH=amd64

upstream-nginx-musl-arm64:
	$(MAKE) upstream-nginx-musl ARCH=arm64

package: release-repogate
	GIT_COMMIT="$(GIT_COMMIT)" \
	BUILD_TIMESTAMP="$(BUILD_TIMESTAMP)" \
	SOURCE_DATE_EPOCH="$(SOURCE_DATE_EPOCH)" \
	REPOGATE_BUILD_ID="$(BUILD_ID)" \
	./scripts/package-release.sh "$(VERSION)" "$(ARCH)" \
		dist/repogate-linux-$(ARCH)/repogate "$(UPSTREAM_NGINX_DIR)" "$(RELEASE_DIR)"

verify-package:
	./scripts/verify-release-packages.sh "$(VERSION)" "$(ARCH)" "$(RELEASE_DIR)" "$(UPSTREAM_NGINX_DIR)/nginx"

release:
	$(MAKE) upstream-nginx-musl package verify-package VERSION="$(VERSION)" ARCH=amd64 RELEASE_DIR="$(RELEASE_DIR)"
	$(MAKE) upstream-nginx-musl package verify-package VERSION="$(VERSION)" ARCH=arm64 RELEASE_DIR="$(RELEASE_DIR)"
	cd "$(RELEASE_DIR)" && sha256sum \
		"repogate_$(VERSION)_amd64.deb" \
		"repogate_$(VERSION)_arm64.deb" \
		"repogate-$(VERSION).x86_64.rpm" \
		"repogate-$(VERSION).aarch64.rpm" \
		"repogate-$(VERSION)-linux-amd64.tar.gz" \
		"repogate-$(VERSION)-linux-arm64.tar.gz" > SHA256SUMS
	cd "$(RELEASE_DIR)" && sha256sum -c SHA256SUMS
	cd "$(RELEASE_DIR)" && sed 's|^|# linux/amd64: |' Managed-Upstream-Nginx-SHA256-amd64 >> SHA256SUMS
	cd "$(RELEASE_DIR)" && sed 's|^|# linux/arm64: |' Managed-Upstream-Nginx-SHA256-arm64 >> SHA256SUMS
