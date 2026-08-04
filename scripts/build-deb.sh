#!/usr/bin/env bash
set -euo pipefail

package_name="${PACKAGE_NAME:-endlessnet-client}"
version="${VERSION:?VERSION is required}"
output_dir="${OUTPUT_DIR:-dist/deb}"
commit="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
prebuilt_binary="${ENDLESSNET_DEB_PREBUILT_BINARY:-}"
prebuilt_recovery_helper="${ENDLESSNET_DEB_PREBUILT_RECOVERY_HELPER:-}"
prebuilt_systemd_dir="${ENDLESSNET_DEB_PREBUILT_SYSTEMD_DIR:-}"

mkdir -p "$output_dir"

case "$version" in
  [0-9]*) ;;
  *) echo "Debian package version must start with a digit: $version" >&2; exit 1 ;;
esac

build_one() {
  arch="$1"
  work_dir="$(mktemp -d)"
  trap 'rm -rf "$work_dir"' RETURN

  case "$arch" in
    amd64|arm64) ;;
    *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
  esac

  pkg_root="$work_dir/${package_name}_${version}_${arch}"
  opt_bin_dir="$pkg_root/opt/endlessnet/bin"
  usr_bin_dir="$pkg_root/usr/bin"
  systemd_dir="$pkg_root/lib/systemd/system"
  tmpfiles_dir="$pkg_root/usr/lib/tmpfiles.d"
  libexec_dir="$pkg_root/usr/libexec/endlessnet"
  polkit_dir="$pkg_root/usr/share/polkit-1/actions"
  doc_dir="$pkg_root/usr/share/doc/$package_name"
  control_dir="$pkg_root/DEBIAN"

  mkdir -p "$opt_bin_dir" "$usr_bin_dir" "$systemd_dir" "$tmpfiles_dir" "$libexec_dir" "$polkit_dir" "$doc_dir" "$control_dir"

  if [ -n "$prebuilt_binary" ]; then
    cp "$prebuilt_binary" "$opt_bin_dir/$package_name"
  else
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build \
      -trimpath \
      -ldflags "-s -w -X main.version=$version -X main.commit=$commit -X main.buildDate=$build_date" \
      -o "$opt_bin_dir/$package_name" \
      ./cmd/endlessnet-client
  fi
  chmod 0755 "$opt_bin_dir/$package_name"
  if [ -n "$prebuilt_recovery_helper" ]; then
    cp "$prebuilt_recovery_helper" "$libexec_dir/endlessnet-client-recovery-helper"
  else
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build \
      -trimpath \
      -ldflags "-s -w" \
      -o "$libexec_dir/endlessnet-client-recovery-helper" \
      ./cmd/endlessnet-client-recovery-helper
  fi
  chmod 0755 "$libexec_dir/endlessnet-client-recovery-helper"
  install -m 0644 packaging/polkit/ru.endlessnet.client.recovery.policy "$polkit_dir/ru.endlessnet.client.recovery.policy"
  ln -s "../../opt/endlessnet/bin/$package_name" "$usr_bin_dir/$package_name"
  ln -s "../../opt/endlessnet/bin/$package_name" "$usr_bin_dir/endlessnet"

  rendered_systemd_dir="$work_dir/systemd"
  if [ -n "$prebuilt_systemd_dir" ]; then
    cp "$prebuilt_systemd_dir/$package_name.service" "$systemd_dir/$package_name.service"
    cp "$prebuilt_systemd_dir/$package_name.tmpfiles.conf" "$tmpfiles_dir/$package_name.conf"
  else
    go run ./cmd/endlessnet-client service render-systemd \
      --output-dir "$rendered_systemd_dir" \
      --name "$package_name" \
      --binary "/opt/endlessnet/bin/$package_name"
    cp "$rendered_systemd_dir/$package_name.service" "$systemd_dir/$package_name.service"
    cp "$rendered_systemd_dir/$package_name.tmpfiles.conf" "$tmpfiles_dir/$package_name.conf"
  fi
  chmod 0644 "$systemd_dir/$package_name.service" "$tmpfiles_dir/$package_name.conf"
  install -m 0644 LICENSE "$doc_dir/copyright"
  install -m 0644 NOTICE "$doc_dir/NOTICE"
  install -m 0644 THIRD_PARTY_NOTICES "$doc_dir/THIRD_PARTY_NOTICES"

  cat > "$control_dir/preinst" <<EOF
#!/bin/sh
set -e

state_dir=/var/lib/endlessnet
restart_marker=\$state_dir/.restart-after-upgrade

if [ "\${1:-}" = "upgrade" ] && command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet $package_name.service; then
  install -d -m 0700 -o root -g root "\$state_dir"
  : > "\$restart_marker"
  chmod 0600 "\$restart_marker"
  systemctl stop $package_name.service
fi

exit 0
EOF

  cat > "$control_dir/postinst" <<EOF
#!/bin/sh
set -e

restart_marker=/var/lib/endlessnet/.restart-after-upgrade

if command -v systemd-tmpfiles >/dev/null 2>&1; then
  systemd-tmpfiles --create /usr/lib/tmpfiles.d/$package_name.conf >/dev/null 2>&1 || true
fi
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
  if [ -f "\$restart_marker" ]; then
    systemctl reset-failed $package_name.service >/dev/null 2>&1 || true
    systemctl start $package_name.service
    rm -f "\$restart_marker"
  fi
fi

exit 0
EOF

  cat > "$control_dir/prerm" <<EOF
#!/bin/sh
set -e

if [ "\${1:-}" = "remove" ] || [ "\${1:-}" = "deconfigure" ]; then
  if command -v systemctl >/dev/null 2>&1; then
    systemctl stop $package_name.service >/dev/null 2>&1 || true
    systemctl disable $package_name.service >/dev/null 2>&1 || true
  fi
  if [ -x /opt/endlessnet/bin/$package_name ] && [ -s /var/lib/endlessnet/client.json ]; then
    /opt/endlessnet/bin/$package_name down \
      --config /var/lib/endlessnet/client.json >/dev/null 2>&1 || true
  fi
fi

exit 0
EOF

  cat > "$control_dir/postrm" <<EOF
#!/bin/sh
set -e

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi

exit 0
EOF
  chmod 0755 "$control_dir/preinst" "$control_dir/postinst" "$control_dir/prerm" "$control_dir/postrm"
  installed_size="$(du -sk "$pkg_root" | awk '{print $1}')"

  cat > "$control_dir/control" <<EOF
Package: $package_name
Version: $version
Section: net
Priority: optional
Architecture: $arch
Maintainer: EndlessNet <support@endlessnet.ru>
Installed-Size: $installed_size
Description: EndlessNet CLI client
 EndlessNet client registers nodes with the control plane and renders
 WireGuard-compatible peer configuration.
EOF

  dpkg-deb --build --root-owner-group "$pkg_root" "$output_dir/${package_name}_${version}_${arch}.deb"
}

if [ "$#" -eq 0 ]; then
  set -- amd64 arm64
fi

for arch in "$@"; do
  build_one "$arch"
done
