#!/usr/bin/env bash
set -euo pipefail

package_name="${PACKAGE_NAME:-unng-archive-keyring}"
version="${VERSION:?VERSION is required}"
key_bundle="${KEY_BUNDLE:-packaging/apt/unng-archive-keyring.asc}"
output_dir="${OUTPUT_DIR:-dist/deb}"
source_date_epoch="${SOURCE_DATE_EPOCH:-0}"

case "$version" in
  [0-9]*) ;;
  *) echo "Keyring package version must start with a digit: $version" >&2; exit 1 ;;
esac

command -v gpg >/dev/null 2>&1 || { echo "gpg is required to build $package_name" >&2; exit 1; }
command -v dpkg-deb >/dev/null 2>&1 || { echo "dpkg-deb is required to build $package_name" >&2; exit 1; }
[ -s "$key_bundle" ] || { echo "APT public key bundle is missing or empty: $key_bundle" >&2; exit 1; }
case "$source_date_epoch" in
  ''|*[!0-9]*) echo "SOURCE_DATE_EPOCH must be a non-negative integer" >&2; exit 1 ;;
esac
export SOURCE_DATE_EPOCH="$source_date_epoch"

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

pkg_root="$work_dir/${package_name}_${version}_all"
keyring_dir="$pkg_root/usr/share/keyrings"
control_dir="$pkg_root/DEBIAN"
mkdir -p "$keyring_dir" "$control_dir" "$output_dir"

gpg --batch --yes --dearmor \
  --output "$keyring_dir/$package_name.gpg" \
  "$key_bundle"
[ -s "$keyring_dir/$package_name.gpg" ] || { echo "Generated APT keyring is empty" >&2; exit 1; }
chmod 0644 "$keyring_dir/$package_name.gpg"

cat > "$control_dir/control" <<EOF
Package: $package_name
Version: $version
Section: misc
Priority: optional
Architecture: all
Multi-Arch: foreign
Maintainer: EndlessNet <support@endlessnet.ru>
Description: OpenPGP keys for the UNNG APT repository
 This package contains the trusted public keys used to verify metadata from
 the UNNG APT repository at https://apt.endlessnet.ru/apt.
EOF

dpkg-deb --build --root-owner-group \
  "$pkg_root" \
  "$output_dir/${package_name}_${version}_all.deb"
