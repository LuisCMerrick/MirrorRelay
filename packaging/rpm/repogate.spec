Name: repogate
Version: %{package_version}
Release: 1%{?dist}
Summary: Pull-through repository and registry gateway
License: GPL-3.0-only
URL: https://github.com/LuisCMerrick/RepoGate
Requires: ca-certificates
Requires: systemd
Requires(pre): shadow-utils
AutoReqProv: no

%global debug_package %{nil}
%global __os_install_post %{nil}

Source0: repogate
Source1: nginx
Source2: config.yaml
Source3: repogate.service
Source4: BUILD-INFO
Source5: managed-upstream-nginx.md
Source6: README.md
Source7: README.zh-CN.md
Source8: INSTALL.md
Source9: INSTALL.zh-CN.md
Source10: web-ui.md
Source11: web-ui.zh-CN.md
Source12: configuration.md
Source13: configuration.zh-CN.md
Source14: verification.md
Source15: verification.zh-CN.md
Source16: LICENSE

%global stop_managed_upstream_nginx() %{expand:
binary=/usr/lib/repogate/nginx/nginx
pid_file=/run/repogate/upstream-nginx.pid
is_managed_upstream_nginx() {
  managed_executable=$(readlink "/proc/$managed_pid/exe" 2>/dev/null || true)
  case "$managed_executable" in
    "$binary"|"$binary (deleted)") return 0 ;;
    *) return 1 ;;
  esac
}
managed_pid=
if [ -r "$pid_file" ]; then
  IFS= read -r managed_pid < "$pid_file" || true
fi
case "$managed_pid" in
  ''|*[!0-9]*) managed_pid= ;;
esac
if [ -n "$managed_pid" ] && is_managed_upstream_nginx; then
  kill -QUIT "$managed_pid" >/dev/null 2>&1 || true
  remaining=30
  while [ "$remaining" -gt 0 ] && is_managed_upstream_nginx; do
    sleep 1
    remaining=$((remaining - 1))
  done
  if is_managed_upstream_nginx; then
    kill -TERM "$managed_pid" >/dev/null 2>&1 || true
    remaining=5
    while [ "$remaining" -gt 0 ] && is_managed_upstream_nginx; do
      sleep 1
      remaining=$((remaining - 1))
    done
  fi
  if is_managed_upstream_nginx; then
    kill -KILL "$managed_pid" >/dev/null 2>&1 || true
  fi
fi
}

%description
RepoGate is a pull-through gateway for Linux software
repositories and Docker/OCI registries. The package includes its version-bound,
statically linked Managed Upstream Nginx data plane.

%prep

%build

%install
install -D -m 0755 %{SOURCE0} %{buildroot}/usr/bin/repogate
install -D -m 0755 %{SOURCE1} %{buildroot}/usr/lib/repogate/nginx/nginx
install -D -m 0640 %{SOURCE2} %{buildroot}/etc/repogate/config.yaml
install -D -m 0644 %{SOURCE3} %{buildroot}/usr/lib/systemd/system/repogate.service
install -D -m 0644 %{SOURCE4} %{buildroot}/usr/share/doc/repogate/BUILD-INFO
install -D -m 0644 %{SOURCE5} %{buildroot}/usr/share/doc/repogate/LICENSES/managed-upstream-nginx.md
install -D -m 0644 %{SOURCE6} %{buildroot}/usr/share/doc/repogate/README.md
install -D -m 0644 %{SOURCE7} %{buildroot}/usr/share/doc/repogate/README.zh-CN.md
install -D -m 0644 %{SOURCE8} %{buildroot}/usr/share/doc/repogate/INSTALL.md
install -D -m 0644 %{SOURCE9} %{buildroot}/usr/share/doc/repogate/INSTALL.zh-CN.md
install -D -m 0644 %{SOURCE10} %{buildroot}/usr/share/doc/repogate/web-ui.md
install -D -m 0644 %{SOURCE11} %{buildroot}/usr/share/doc/repogate/web-ui.zh-CN.md
install -D -m 0644 %{SOURCE12} %{buildroot}/usr/share/doc/repogate/configuration.md
install -D -m 0644 %{SOURCE13} %{buildroot}/usr/share/doc/repogate/configuration.zh-CN.md
install -D -m 0644 %{SOURCE14} %{buildroot}/usr/share/doc/repogate/verification.md
install -D -m 0644 %{SOURCE15} %{buildroot}/usr/share/doc/repogate/verification.zh-CN.md
install -D -m 0644 %{SOURCE16} %{buildroot}/usr/share/licenses/repogate/LICENSE

%pre
getent group repogate >/dev/null 2>&1 || groupadd -r repogate
getent passwd repogate >/dev/null 2>&1 || useradd -r -g repogate -d /var/lib/repogate -M -s /sbin/nologin -c "RepoGate service account" repogate

%post
install -d -m 0750 -o repogate -g repogate \
  /var/lib/repogate \
  /var/lib/repogate/runtime/upstream-nginx \
  /var/cache/repogate \
  /var/log/repogate \
  /var/log/repogate/upstream-nginx
chown root:repogate /etc/repogate/config.yaml
chmod 0640 /etc/repogate/config.yaml
systemctl daemon-reload >/dev/null 2>&1 || true
if [ "$1" -gt 1 ]; then
  was_active=false
  if systemctl is-active --quiet repogate.service; then
    was_active=true
  fi
  systemctl stop repogate.service || true
  %{stop_managed_upstream_nginx}
  if [ "$was_active" = true ]; then
    systemctl start repogate.service || true
  fi
fi

%preun
if [ "$1" -eq 0 ]; then
  install -d -m 0750 /var/lib/repogate
  if [ -f /etc/repogate/config.yaml ]; then
    cp -p /etc/repogate/config.yaml /var/lib/repogate/.config.yaml.package-preserve
  fi
  systemctl stop repogate.service >/dev/null 2>&1 || true
  %{stop_managed_upstream_nginx}
fi

%postun
systemctl daemon-reload >/dev/null 2>&1 || true
if [ "$1" -eq 0 ] && [ -f /var/lib/repogate/.config.yaml.package-preserve ]; then
  install -d -m 0750 /etc/repogate
  install -m 0640 -o root -g repogate /var/lib/repogate/.config.yaml.package-preserve /etc/repogate/config.yaml
  rm -f /var/lib/repogate/.config.yaml.package-preserve
fi
if [ "$1" -eq 0 ]; then
  rm -rf -- /run/repogate
fi

%files
%attr(0755,root,root) /usr/bin/repogate
%dir /usr/lib/repogate
%dir /usr/lib/repogate/nginx
%attr(0755,root,root) /usr/lib/repogate/nginx/nginx
%dir /etc/repogate
%config(noreplace) %attr(0640,root,repogate) /etc/repogate/config.yaml
%attr(0644,root,root) /usr/lib/systemd/system/repogate.service
%doc /usr/share/doc/repogate
%license /usr/share/licenses/repogate/LICENSE

%changelog
* Sat Aug 15 2026 RepoGate Release Pipeline <noreply@github.com> - %{package_version}-1
- Automated architecture-specific release package
