# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e
FROM --platform=$BUILDPLATFORM alpine:3.22.2@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412 AS certificates

RUN apk add --no-cache ca-certificates

FROM alpine:3.22.2@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412

ARG TARGETARCH
ARG VERSION
ARG GIT_COMMIT
ARG BUILD_TIMESTAMP

LABEL org.opencontainers.image.title="MirrorRelay" \
      org.opencontainers.image.description="Pull-through repository manager with a Managed Upstream Nginx data plane" \
      org.opencontainers.image.url="https://github.com/LuisCMerrick/MirrorRelay" \
      org.opencontainers.image.source="https://github.com/LuisCMerrick/MirrorRelay" \
      org.opencontainers.image.documentation="https://github.com/LuisCMerrick/MirrorRelay/blob/main/docs/installation.md" \
      org.opencontainers.image.licenses="GPL-3.0-only" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIMESTAMP}"

COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --chown=0:65532 --chmod=0750 rootfs/etc/mirrorrelay/ /etc/mirrorrelay/
COPY --chmod=0755 rootfs/usr/lib/mirrorrelay/ /usr/lib/mirrorrelay/
COPY --chmod=0755 rootfs/usr/share/doc/mirrorrelay/ /usr/share/doc/mirrorrelay/
COPY --chmod=0755 rootfs/usr/share/licenses/mirrorrelay/ /usr/share/licenses/mirrorrelay/
COPY --chown=65532:65532 rootfs/var/lib/mirrorrelay/ /var/lib/mirrorrelay/
COPY --chown=65532:65532 rootfs/var/cache/mirrorrelay/ /var/cache/mirrorrelay/
COPY --chown=65532:65532 rootfs/var/log/mirrorrelay/ /var/log/mirrorrelay/
COPY --chown=65532:65532 rootfs/run/mirrorrelay/ /run/mirrorrelay/

COPY --chmod=0755 linux-${TARGETARCH}/mirrorrelay /usr/bin/mirrorrelay
COPY --chmod=0755 linux-${TARGETARCH}/nginx /usr/lib/mirrorrelay/nginx/nginx
COPY --chmod=0644 linux-${TARGETARCH}/BUILD-INFO /usr/share/doc/mirrorrelay/BUILD-INFO
COPY --chmod=0644 linux-${TARGETARCH}/LICENSE /usr/share/licenses/mirrorrelay/LICENSE
COPY --chmod=0644 linux-${TARGETARCH}/managed-upstream-nginx.md /usr/share/licenses/mirrorrelay/managed-upstream-nginx.md
COPY --chown=65532:65532 --chmod=0640 config.yaml /etc/mirrorrelay/config.yaml

RUN set -eu; \
    mirrorrelay_checksum="$(sha256sum /usr/bin/mirrorrelay | cut -d ' ' -f 1)"; \
    nginx_checksum="$(sha256sum /usr/lib/mirrorrelay/nginx/nginx | cut -d ' ' -f 1)"; \
    grep -Fx "MirrorRelay SHA256: ${mirrorrelay_checksum}" /usr/share/doc/mirrorrelay/BUILD-INFO; \
    grep -Fx "Managed Upstream Nginx SHA256: ${nginx_checksum}" /usr/share/doc/mirrorrelay/BUILD-INFO

USER 65532:65532
EXPOSE 9081/tcp
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/bin/mirrorrelay"]
CMD ["-config", "/etc/mirrorrelay/config.yaml"]
