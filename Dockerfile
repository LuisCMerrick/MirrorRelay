# syntax=docker/dockerfile:1.7
FROM --platform=linux/amd64 golang:1.24-alpine AS build

ARG VERSION=0.0.11
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

FROM --platform=linux/amd64 alpine:3.22.2
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
