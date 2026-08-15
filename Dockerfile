# syntax=docker/dockerfile:1.7
FROM --platform=linux/amd64 golang:1.24-alpine AS build

ARG VERSION=0.0.1
ARG TARGETARCH
WORKDIR /src
RUN test "${TARGETARCH}" = "amd64"
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/repogate ./cmd/repogate

FROM --platform=linux/amd64 alpine:3.22.2
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 65532 repogate \
    && adduser -S -D -H -u 65532 -G repogate repogate \
    && mkdir -p \
        /opt/repogate/nginx/sbin \
        /var/lib/repogate/runtime \
        /var/lib/repogate/integration/external-nginx \
        /var/cache/repogate \
        /var/log/repogate \
        /run/repogate \
    && chown -R 65532:65532 \
        /var/lib/repogate \
        /var/cache/repogate \
        /var/log/repogate \
        /run/repogate
COPY --from=build /out/repogate /usr/local/bin/repogate
COPY --chmod=0755 nginx/sbin/nginx /opt/repogate/nginx/sbin/nginx
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/repogate"]
CMD ["-config", "/etc/repogate/config.yaml"]
