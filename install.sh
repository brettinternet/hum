#!/bin/sh
set -eu

repo=brettinternet/hum
install_dir=${HUM_INSTALL_DIR:-"${HOME}/.local/bin"}
tmp_dir=
staged_target=

cleanup() {
  if [ -n "$staged_target" ]; then
    rm -f "$staged_target"
  fi
  if [ -n "$tmp_dir" ]; then
    rm -rf "$tmp_dir"
  fi
}
trap cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

fail() {
  printf 'hum installer: %s\n' "$*" >&2
  exit 1
}

for tool in curl uname mktemp mkdir rm tar gzip awk cat chmod mv; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool not found: $tool"
done

case "$(uname -s)" in
  Linux) asset_os=linux ;;
  Darwin) asset_os=macos ;;
  *) fail "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64) asset_arch=x64 ;;
  aarch64|arm64) asset_arch=arm64 ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/hum-install.XXXXXX") || fail 'could not create a temporary directory'

if [ -n "${HUM_VERSION:-}" ]; then
  version=${HUM_VERSION#v}
  [ -n "$version" ] || fail 'HUM_VERSION must not be empty'
  tag=v$version
else
  latest_url=$(curl --proto '=https' --tlsv1.2 -fsSL -o "$tmp_dir/latest" -w '%{url_effective}' \
    "https://github.com/$repo/releases/latest") || fail 'could not resolve the latest release'
  tag=${latest_url##*/}
  case "$tag" in
    v?*) version=${tag#v} ;;
    *) fail "latest release resolved to an invalid tag: $tag" ;;
  esac
fi
case "$version" in
  *[!0-9A-Za-z._-]*|'') fail "invalid release version: $version" ;;
esac

archive="hum-${version}-${asset_os}-${asset_arch}.tar.gz"
release_url="https://github.com/$repo/releases/download/$tag"
archive_path="$tmp_dir/$archive"
checksums_path="$tmp_dir/checksums.txt"

curl --proto '=https' --tlsv1.2 -fsSL "$release_url/$archive" -o "$archive_path" \
  || fail "could not download release archive: $archive"
curl --proto '=https' --tlsv1.2 -fsSL "$release_url/checksums.txt" -o "$checksums_path" \
  || fail "could not download checksums for $tag"

expected=$(awk -v archive="$archive" '
  $2 == archive || $2 == "./" archive { checksum = $1; count++ }
  END { if (count != 1) exit 1; print checksum }
' "$checksums_path") || fail "checksum entry unavailable or ambiguous for $archive"

case "$expected" in
  *[!0-9A-Fa-f]*|'') fail "invalid checksum for $archive" ;;
esac
[ "${#expected}" -eq 64 ] || fail "invalid checksum for $archive"

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$archive_path" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$archive_path" | awk '{ print $1 }')
else
  fail 'required checksum tool not found: sha256sum or shasum'
fi
[ "$actual" = "$expected" ] || fail "checksum mismatch for $archive"

mkdir "$tmp_dir/extract" || fail 'could not prepare archive extraction'
tar -xzf "$archive_path" -C "$tmp_dir/extract" hum \
  || fail "could not extract hum from $archive"
[ -f "$tmp_dir/extract/hum" ] || fail "archive does not contain a regular hum executable"

mkdir -p "$install_dir" || fail "could not create install directory: $install_dir"
staged_target=$(mktemp "$install_dir/.hum-install.XXXXXX") \
  || fail "could not create an install file in $install_dir"
cat "$tmp_dir/extract/hum" >"$staged_target" || fail 'could not stage hum executable'
chmod 755 "$staged_target" || fail 'could not make hum executable'
mv -f "$staged_target" "$install_dir/hum" || fail "could not install hum to $install_dir/hum"
staged_target=

printf 'Installed hum %s to %s/hum\n' "$version" "$install_dir"
case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *) printf 'Warning: %s is not in PATH\n' "$install_dir" >&2 ;;
esac
