#!/bin/sh
set -eu
export LC_ALL=C

[ "$#" -eq 2 ] || { printf '%s\n' 'Использование: build-deb.sh v0.MINOR.PATCH /абсолютный/выход.deb' >&2; exit 1; }
tag=$1
output=$2
printf '%s\n' "$tag" | grep -Eq '^v0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
case "$output" in /*.deb) ;; *) exit 1 ;; esac
[ ! -e "$output" ] && [ ! -L "$output" ]
source_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

# Both binaries are staged by the same release build and carry the same SDK pin.
contract=$("$source_dir/client" print-contract)
[ "$contract" = "$("$source_dir/endlessnet" --print-contract)" ]
printf '%s\n' "$contract" | grep -Eq '^[0-9a-f]{64}$'

root=$(mktemp -d)
trap 'rm -rf -- "$root"' EXIT
chmod 0755 "$root"
install -d -m 0755 "$root/DEBIAN" "$root/usr/bin" "$root/usr/libexec/endlessnet-client" "$root/usr/lib/systemd/system"
install -d -m 0755 "$root/usr/share/doc/endlessnet-client"
install -m 0755 "$source_dir/client" "$root/usr/libexec/endlessnet-client/client"
install -m 0755 "$source_dir/endlessnet" "$root/usr/bin/endlessnet"
install -m 0755 "$source_dir/packaging/check-updates" "$root/usr/libexec/endlessnet-client/check-updates"
install -m 0644 "$source_dir/packaging/endlessnet-client.service" "$root/usr/lib/systemd/system/endlessnet-client.service"
install -m 0644 "$source_dir/crypto-sources.json" "$source_dir"/licenses/* "$root/usr/share/doc/endlessnet-client"
for script in preinst postinst prerm postrm; do
    install -m 0755 "$source_dir/packaging/$script" "$root/DEBIAN/$script"
done
cat > "$root/DEBIAN/control" <<EOF
Package: endlessnet-client
Version: ${tag#v}
Architecture: amd64
Maintainer: EndlessNet <support@endlessnet.ru>
Depends: systemd, init-system-helpers, iproute2, nftables, ca-certificates
Provides: endlessnet-client-linux-runtime
Section: net
Priority: optional
Description: EndlessNet headless Client for Ubuntu
 Runs the EndlessNet service and provides the endlessnet local command.
EOF
dpkg-deb --root-owner-group --build "$root" "$output"
