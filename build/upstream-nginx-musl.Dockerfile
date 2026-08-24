# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.9.0@sha256:c64defb9ed5a91eacb37f96ccc3d4cd72521c4bd18d5442905b95e2226b0e707 AS xx

FROM --platform=$BUILDPLATFORM alpine:3.22.2@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412 AS builder

ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG TARGETARCH
ARG NGINX_VERSION=1.30.4
ARG NGINX_SHA256=4261dc90e9e47c1c4041276e9aaa3d48ebe2e664f728e14fa95ae6c67d57a08b
ARG OPENSSL_VERSION=3.5.7
ARG OPENSSL_SHA256=a8c0d28a529ca480f9f36cf5792e2cd21984552a3c8e4aa11a24aa31aeac98e8
ARG PCRE2_VERSION=10.47
ARG PCRE2_SHA256=c08ae2388ef333e8403e670ad70c0a11f1eed021fd88308d7e02f596fcd9dc16
ARG ZLIB_VERSION=1.3.2
ARG ZLIB_SHA256=bb329a0a2cd0274d05519d61c667c062e06990d72e125ee2dfa8de64f0119d16

ENV SOURCE_DATE_EPOCH=0
ENV TZ=UTC

COPY --from=xx / /
COPY --chmod=0755 build/target-uname.sh /opt/mirrorrelay-cross/bin/uname

RUN apk add --no-cache bash binutils build-base ca-certificates clang curl file lld llvm perl
RUN case "${TARGETARCH}" in amd64|arm64) ;; *) echo "Unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; esac
RUN xx-apk add --no-cache gcc linux-headers musl-dev \
 && xx-clang --setup-target-triple

WORKDIR /build
RUN curl -fL --retry 3 -o nginx.tar.gz "https://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz" \
 && curl -fL --retry 3 -o openssl.tar.gz "https://github.com/openssl/openssl/releases/download/openssl-${OPENSSL_VERSION}/openssl-${OPENSSL_VERSION}.tar.gz" \
 && curl -fL --retry 3 -o pcre2.tar.gz "https://github.com/PCRE2Project/pcre2/releases/download/pcre2-${PCRE2_VERSION}/pcre2-${PCRE2_VERSION}.tar.gz" \
 && curl -fL --retry 3 -o zlib.tar.gz "https://github.com/madler/zlib/releases/download/v${ZLIB_VERSION}/zlib-${ZLIB_VERSION}.tar.gz" \
 && echo "${NGINX_SHA256}  nginx.tar.gz" | sha256sum -c - \
 && echo "${OPENSSL_SHA256}  openssl.tar.gz" | sha256sum -c - \
 && echo "${PCRE2_SHA256}  pcre2.tar.gz" | sha256sum -c - \
 && echo "${ZLIB_SHA256}  zlib.tar.gz" | sha256sum -c - \
 && tar -xzf nginx.tar.gz \
 && tar -xzf openssl.tar.gz \
 && tar -xzf pcre2.tar.gz \
 && tar -xzf zlib.tar.gz

WORKDIR /build/nginx-${NGINX_VERSION}
RUN set -eu; \
    target_triple="$(xx-info triple)"; \
    pcre_build_triple="$(/build/pcre2-${PCRE2_VERSION}/config.guess)"; \
    case "${TARGETARCH}" in \
      amd64) openssl_target=linux-x86_64 ;; \
      arm64) openssl_target=linux-aarch64 ;; \
    esac; \
    export CC="${target_triple}-clang -static"; \
    export AR="${target_triple}-ar"; \
    export RANLIB="${target_triple}-ranlib"; \
    export STRIP="${target_triple}-strip"; \
    MIRRORRELAY_TARGETARCH="${TARGETARCH}" PATH="/opt/mirrorrelay-cross/bin:${PATH}" ./configure \
      --prefix=/usr/lib/mirrorrelay/nginx \
      --sbin-path=nginx \
      --conf-path=conf/nginx.conf \
      --pid-path=run/nginx.pid \
      --lock-path=run/nginx.lock \
      --http-log-path=logs/access.log \
      --error-log-path=logs/error.log \
      --http-client-body-temp-path=temp/client \
      --http-proxy-temp-path=temp/proxy \
      --with-http_ssl_module \
      --with-http_v2_module \
      --with-threads \
      --with-file-aio \
      --with-pcre="/build/pcre2-${PCRE2_VERSION}" \
      --with-pcre-jit \
      --with-zlib="/build/zlib-${ZLIB_VERSION}" \
      --with-openssl="/build/openssl-${OPENSSL_VERSION}" \
      --with-openssl-opt="${openssl_target} no-shared no-tests no-module no-quic" \
      --without-http_fastcgi_module \
      --without-http_uwsgi_module \
      --without-http_scgi_module \
      --without-http_memcached_module \
      --without-http_autoindex_module \
      --without-http_ssi_module \
      --without-http_userid_module \
      --without-http_auth_basic_module \
      --without-http_mirror_module \
      --without-http_geo_module \
      --without-http_split_clients_module \
      --without-http_referer_module \
      --without-http_limit_conn_module \
      --without-http_limit_req_module \
      --without-http_empty_gif_module \
      --without-http_browser_module \
      --without-http_upstream_hash_module \
      --without-http_upstream_ip_hash_module \
      --without-http_upstream_least_conn_module \
      --without-http_upstream_random_module \
      --without-http_upstream_zone_module \
      --without-http_grpc_module \
      --with-cc-opt="-O2 -fstack-protector-strong -D_FORTIFY_SOURCE=2 -fPIE -static -Wno-error=sign-compare" \
      --with-ld-opt="-static -pie -Wl,-z,relro,-z,now" \
 && sed -i \
      "s#./configure --disable-shared#./configure --build=${pcre_build_triple} --host=${target_triple} --disable-shared#" \
      objs/Makefile \
 && grep -F "./configure --build=${pcre_build_triple} --host=${target_triple} --disable-shared" objs/Makefile \
 && make -j"$(getconf _NPROCESSORS_ONLN)" \
 && "${STRIP}" objs/nginx \
 && install -D -m 0755 objs/nginx /out/nginx \
 && xx-verify --static /out/nginx \
 && sed -n 's/^#define NGX_CONFIGURE "\(.*\)"$/\1/p' \
      objs/ngx_auto_config.h > /tmp/nginx-configure-arguments \
 && test -s /tmp/nginx-configure-arguments

RUN ldd /out/nginx > /tmp/nginx-ldd 2>&1 || true

RUN set -eu; \
    ! grep -Eq '=>|ld-linux|ld-musl|libc\.so|libssl\.so|libcrypto\.so|libpcre[^ ]*\.so|libz\.so' /tmp/nginx-ldd; \
    ! readelf -l /out/nginx | grep -q 'INTERP'; \
    case "${TARGETARCH}" in \
      amd64) file /out/nginx | grep -F 'x86-64' ;; \
      arm64) file /out/nginx | grep -F 'ARM aarch64' ;; \
    esac; \
    nginx_checksum="$(sha256sum /out/nginx | cut -d ' ' -f 1)"; \
    configure_arguments="$(cat /tmp/nginx-configure-arguments)"; \
    musl_version="$(apk info -v 2>/dev/null | grep '^musl-' | head -n 1)"; \
    build_id="nginx-${NGINX_VERSION}-linux-${TARGETARCH}-$(printf '%s' "${nginx_checksum}" | cut -c 1-12)"; \
    { \
      printf 'Managed Upstream Nginx Version: %s\n' "${NGINX_VERSION}"; \
      printf 'Nginx Source SHA256: %s\n' "${NGINX_SHA256}"; \
      printf 'Configure Arguments: %s\n' "${configure_arguments}"; \
      printf 'musl Version: %s\n' "${musl_version}"; \
      printf 'TLS Library Version: OpenSSL %s\n' "${OPENSSL_VERSION}"; \
      printf 'PCRE2 Version: %s\n' "${PCRE2_VERSION}"; \
      printf 'Compression Library Version: zlib %s\n' "${ZLIB_VERSION}"; \
      printf 'Build Method: cross-compiled on %s with xx/Clang for %s\n' "${BUILDPLATFORM}" "${TARGETPLATFORM}"; \
      printf 'Target OS: linux\n'; \
      printf 'Target Architecture: %s\n' "${TARGETARCH}"; \
      printf 'Build ID: %s\n' "${build_id}"; \
      printf 'Managed Upstream Nginx SHA256: %s\n' "${nginx_checksum}"; \
    } > /out/BUILD-INFO.upstream-nginx

FROM scratch AS artifact
COPY --from=builder /out/ /
