# syntax=docker/dockerfile:1.7
FROM --platform=linux/amd64 golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build

ARG VERSION=0.0.16
ARG TARGETARCH
WORKDIR /src
RUN test "${TARGETARCH}" = "amd64"
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/mirrorrelay ./cmd/mirrorrelay

FROM --platform=linux/amd64 alpine:3.22.2@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 65532 mirrorrelay \
    && adduser -S -D -H -u 65532 -G mirrorrelay mirrorrelay \
    && mkdir -p \
        /opt/mirrorrelay/nginx/sbin \
        /var/lib/mirrorrelay/runtime \
        /var/lib/mirrorrelay/integration/external-nginx \
        /var/cache/mirrorrelay \
        /var/log/mirrorrelay \
        /run/mirrorrelay \
    && chown -R 65532:65532 \
        /var/lib/mirrorrelay \
        /var/cache/mirrorrelay \
        /var/log/mirrorrelay \
        /run/mirrorrelay
COPY --from=build /out/mirrorrelay /usr/local/bin/mirrorrelay
COPY --chmod=0755 nginx/sbin/nginx /opt/mirrorrelay/nginx/sbin/nginx
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/mirrorrelay"]
CMD ["-config", "/etc/mirrorrelay/config.yaml"]
