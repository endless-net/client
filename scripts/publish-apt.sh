#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'APT publication: %s\n' "$1" >&2
  exit 1
}

[[ $# == 4 ]] || fail 'expected release artifacts, APT checkout, tag and source SHA'
artifacts=$(realpath "$1")
apt_dir=$(realpath "$2/apt")
tag=$3
source_sha=$4
[[ $tag =~ ^v0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ && $source_sha =~ ^[0-9a-f]{40}$ ]] ||
  fail 'invalid runtime release identity'

version=${tag#v}
package=endlessnet-client
deb=${package}_${version}_amd64.deb
relative_deb=pool/main/e/$package/$deb
target="$apt_dir/$relative_deb"
keyring="$apt_dir/unng.gpg"
dist="$apt_dir/dists/stable"
binary="$dist/main/binary-amd64"

jq -e --arg tag "$tag" --arg sha "$source_sha" \
  '.component == "client" and .tag == $tag and .commit == $sha' "$artifacts/source.json" >/dev/null ||
  fail 'release metadata differs from the tag commit'
digest=$(sha256sum "$artifacts/$deb")
digest=${digest%% *}
[[ $(awk -v name="$deb" '$2 == name { print $1 }' "$artifacts/checksums.txt") == "$digest" ]] ||
  fail 'package checksum differs from the release manifest'
[[ $(dpkg-deb -f "$artifacts/$deb" Package) == "$package" &&
   $(dpkg-deb -f "$artifacts/$deb" Version) == "$version" &&
   $(dpkg-deb -f "$artifacts/$deb" Architecture) == amd64 ]] ||
  fail 'package identity differs from the runtime tag'

tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
export GNUPGHOME="$tmp/gnupg"
install -d -m 0700 "$GNUPGHOME"
gpgv --keyring "$keyring" --output "$tmp/current.Release" "$dist/InRelease"
cmp -s "$tmp/current.Release" "$dist/Release" || fail 'signed APT metadata differs from Release'

# Verify both architectures before signing a new Release. Preserve arm64 bytes.
index_paths=(
  main/binary-amd64/Packages main/binary-amd64/Packages.gz
  main/binary-arm64/Packages main/binary-arm64/Packages.gz
)
for path in "${index_paths[@]}"; do
  expected=$(awk -v path="$path" '/^SHA256:$/ { section=1; next }
    /^[^[:space:]]/ { section=0 }
    section && $3 == path { print $1 }' "$dist/Release")
  actual=$(sha256sum "$dist/$path")
  [[ $expected == "${actual%% *}" ]] || fail "index checksum differs: $path"
done

indexed_package() {
  awk -v package="$package" -v version="$version" -v path="$relative_deb" -v digest="$digest" '
    BEGIN { RS=""; FS="\n" }
    {
      p = v = a = f = h = 0
      for (i = 1; i <= NF; i++) {
        if ($i == "Package: " package) p=1
        if ($i == "Version: " version) v=1
        if ($i == "Architecture: amd64") a=1
        if ($i == "Filename: " path) f=1
        if ($i == "SHA256: " digest) h=1
      }
      if (p && v && a && f && h) found=1
    }
    END { exit !found }
  ' "$1"
}

if [[ -e $target ]]; then
  cmp -s "$artifacts/$deb" "$target" || fail 'this package version already exists with different bytes'
  if indexed_package "$binary/Packages"; then
    printf 'APT already indexes %s\n' "$deb"
    exit 0
  fi
fi

current_version=$(awk -v package="$package" '
  /^Package: / { matches = ($2 == package) }
  matches && /^Version: / { print $2 }
' "$binary/Packages")
if [[ -n $current_version ]] && ! dpkg --compare-versions "$version" ge "$current_version"; then
  fail 'a newer amd64 version is already indexed'
fi

install -d "$(dirname "$target")"
install -m 0644 "$artifacts/$deb" "$target"
(cd "$apt_dir" && dpkg-scanpackages -a amd64 pool /dev/null) > "$tmp/Packages"
indexed_package "$tmp/Packages" || fail 'package would not be the indexed amd64 version'

# Stage the existing index layout so apt-ftparchive never sees stale signatures.
staging="$tmp/stable"
install -d "$staging/main/binary-amd64" "$staging/main/binary-arm64"
install -m 0644 "$tmp/Packages" "$staging/main/binary-amd64/Packages"
gzip -9n -c "$tmp/Packages" > "$staging/main/binary-amd64/Packages.gz"
cp -- "$dist/main/binary-arm64/"{Packages,Packages.gz} "$staging/main/binary-arm64/"
apt-ftparchive \
  -o APT::FTPArchive::Release::Origin=UNNG \
  -o APT::FTPArchive::Release::Label=UNNG \
  -o APT::FTPArchive::Release::Suite=stable \
  -o APT::FTPArchive::Release::Codename=stable \
  -o 'APT::FTPArchive::Release::Architectures=amd64 arm64' \
  -o APT::FTPArchive::Release::Components=main \
  -o 'APT::FTPArchive::Release::Description=UNNG APT repository' \
  release "$staging" > "$tmp/Release"

printf '%s\n' "${APT_SIGNING_PRIVATE_KEY:?APT signing key is required}" | gpg --batch --import
unset APT_SIGNING_PRIVATE_KEY
sign=(gpg --batch --yes --pinentry-mode loopback --passphrase-fd 0 --digest-algo SHA256)
printf '%s\n' "${APT_SIGNING_PASSPHRASE:-}" | "${sign[@]}" --clearsign --output "$tmp/InRelease" "$tmp/Release"
printf '%s\n' "${APT_SIGNING_PASSPHRASE:-}" | "${sign[@]}" --armor --detach-sign --output "$tmp/Release.gpg" "$tmp/Release"
unset APT_SIGNING_PASSPHRASE
gpgv --keyring "$keyring" --output "$tmp/verified.Release" "$tmp/InRelease"
gpgv --keyring "$keyring" "$tmp/Release.gpg" "$tmp/Release"
cmp -s "$tmp/Release" "$tmp/verified.Release" || fail 'new InRelease differs from Release'

install -m 0644 "$staging/main/binary-amd64/"{Packages,Packages.gz} "$binary/"
install -m 0644 "$tmp/"{Release,InRelease,Release.gpg} "$dist/"
