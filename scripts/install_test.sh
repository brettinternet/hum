#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
test_root=$(mktemp -d "${TMPDIR:-/tmp}/hum-install-test.XXXXXX")
cleanup() { rm -rf "$test_root"; }
trap cleanup 0 HUP INT TERM

fail() {
  printf 'not ok - %s\n' "$*" >&2
  exit 1
}

pass() { printf 'ok - %s\n' "$*"; }

host_sha() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

if [ "${1:-}" = --live ]; then
  version=${2:-v0.9.0}
  live_dir="$test_root/live-bin"
  HUM_VERSION=$version HUM_INSTALL_DIR=$live_dir sh "$root/install.sh"
  output=$("$live_dir/hum" --version)
  expected=${version#v}
  case "$output" in
    *"$expected"*) ;;
    *) fail "live binary reported unexpected version: $output" ;;
  esac
  case "$output" in
    *"v$expected"*) fail "live binary version retained a leading v: $output" ;;
  esac
  pass "live release $version installs and reports $expected"
  exit 0
fi

make_tool_bin() {
  case_dir=$1
  os=$2
  arch=$3
  include_checksum=${4:-yes}
  mkdir -p "$case_dir/bin"
  for tool in mktemp mkdir rm tar gzip awk cat chmod mv env; do
    tool_path=$(command -v "$tool")
    ln -s "$tool_path" "$case_dir/bin/$tool"
  done
  if [ "$include_checksum" = yes ]; then
    if command -v sha256sum >/dev/null 2>&1; then
      ln -s "$(command -v sha256sum)" "$case_dir/bin/sha256sum"
    else
      ln -s "$(command -v shasum)" "$case_dir/bin/shasum"
    fi
  fi
  cat >"$case_dir/bin/uname" <<EOF
#!/bin/sh
case "\${1:-}" in
  -s) printf '%s\\n' '$os' ;;
  -m) printf '%s\\n' '$arch' ;;
  *) exit 1 ;;
esac
EOF
  cat >"$case_dir/bin/curl" <<'EOF'
#!/bin/sh
output=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) shift; output=$1 ;;
    -w) shift ;;
    http://*|https://*) url=$1 ;;
  esac
  shift
done
printf '%s\n' "$url" >>"$STUB_LOG"
case "$url" in
  */releases/latest)
    [ "${STUB_FAIL:-}" != latest ] || exit 22
    printf '%s' "https://github.com/brettinternet/hum/releases/tag/$STUB_LATEST_TAG"
    ;;
  */checksums.txt)
    [ "${STUB_FAIL:-}" != checksums ] || exit 22
    /bin/cp "$STUB_CHECKSUMS" "$output"
    ;;
  *)
    [ "${STUB_FAIL:-}" != archive ] || exit 22
    [ "${url##*/}" = "$STUB_ARCHIVE_NAME" ] || exit 22
    /bin/cp "$STUB_ARCHIVE" "$output"
    ;;
esac
EOF
  chmod +x "$case_dir/bin/uname" "$case_dir/bin/curl"
}

make_fixture() {
  case_dir=$1
  archive_name=$2
  version=$3
  mkdir -p "$case_dir/fixture"
  cat >"$case_dir/fixture/hum" <<EOF
#!/bin/sh
printf '%s\\n' 'hum version $version'
EOF
  printf 'must not be installed\n' >"$case_dir/fixture/extra"
  chmod +x "$case_dir/fixture/hum"
  tar -C "$case_dir/fixture" -czf "$case_dir/$archive_name" hum extra
  checksum=$(host_sha "$case_dir/$archive_name")
  printf '%s  ./%s\n' "$checksum" "$archive_name" >"$case_dir/checksums.txt"
}

run_installer() {
  case_dir=$1
  shift
  env PATH="$case_dir/bin" \
    HOME="$case_dir/home" \
    TMPDIR="$case_dir/tmp" \
    HUM_INSTALL_DIR="$case_dir/install" \
    STUB_LOG="$case_dir/urls.log" \
    STUB_LATEST_TAG="${STUB_LATEST_TAG:-v0.9.0}" \
    STUB_ARCHIVE_NAME="$STUB_ARCHIVE_NAME" \
    STUB_ARCHIVE="$case_dir/$STUB_ARCHIVE_NAME" \
    STUB_CHECKSUMS="$case_dir/checksums.txt" \
    STUB_FAIL="${STUB_FAIL:-}" \
    "$@" /bin/sh "$root/install.sh" >"$case_dir/stdout" 2>"$case_dir/stderr"
}

success_case() {
  name=$1
  os=$2
  arch=$3
  asset_os=$4
  asset_arch=$5
  version=${6:-0.9.0}
  case_dir="$test_root/$name"
  archive_name="hum-$version-$asset_os-$asset_arch.tar.gz"
  mkdir -p "$case_dir/home" "$case_dir/tmp"
  make_tool_bin "$case_dir" "$os" "$arch" yes
  make_fixture "$case_dir" "$archive_name" "$version"
  STUB_ARCHIVE_NAME=$archive_name
  export STUB_ARCHIVE_NAME
  if ! run_installer "$case_dir" env HUM_VERSION="v$version"; then
    cat "$case_dir/stderr" >&2
    fail "$name installer run failed"
  fi
  [ -x "$case_dir/install/hum" ] || fail "$name did not install an executable"
  [ ! -e "$case_dir/install/extra" ] || fail "$name extracted an unrequested member"
  grep -q "/$archive_name$" "$case_dir/urls.log" || fail "$name selected the wrong archive"
  grep -q "Installed hum $version to $case_dir/install/hum" "$case_dir/stdout" || fail "$name omitted success details"
  pass "$name selects $archive_name"
}

success_case linux-x86-64 Linux x86_64 linux x64
success_case linux-amd64 Linux amd64 linux x64
success_case linux-aarch64 Linux aarch64 linux arm64
success_case linux-arm64 Linux arm64 linux arm64
success_case darwin-x86-64 Darwin x86_64 macos x64
success_case darwin-amd64 Darwin amd64 macos x64
success_case darwin-aarch64 Darwin aarch64 macos arm64
success_case darwin-arm64 Darwin arm64 macos arm64

unsupported_case() {
  name=$1
  os=$2
  arch=$3
  case_dir="$test_root/$name"
  mkdir -p "$case_dir/home" "$case_dir/tmp"
  make_tool_bin "$case_dir" "$os" "$arch" yes
  if PATH="$case_dir/bin" HOME="$case_dir/home" TMPDIR="$case_dir/tmp" HUM_INSTALL_DIR="$case_dir/install" \
    /bin/sh "$root/install.sh" >"$case_dir/stdout" 2>"$case_dir/stderr"; then
    fail "$name unexpectedly succeeded"
  fi
  [ ! -e "$case_dir/install/hum" ] || fail "$name replaced the target"
  pass "$name fails without creating hum"
}

unsupported_case unsupported-os FreeBSD x86_64
unsupported_case unsupported-arch Linux riscv64

latest_dir="$test_root/latest"
mkdir -p "$latest_dir/home" "$latest_dir/tmp"
make_tool_bin "$latest_dir" Linux x86_64 yes
STUB_ARCHIVE_NAME=hum-0.9.0-linux-x64.tar.gz
export STUB_ARCHIVE_NAME
make_fixture "$latest_dir" "$STUB_ARCHIVE_NAME" 0.9.0
run_installer "$latest_dir" env -u HUM_VERSION
head -n 1 "$latest_dir/urls.log" | grep -q '/releases/latest$' || fail 'unset HUM_VERSION did not resolve latest release'
grep -q '/releases/download/v0.9.0/hum-0.9.0-linux-x64.tar.gz$' "$latest_dir/urls.log" || fail 'latest release URLs were incorrect'
pass 'unset HUM_VERSION selects latest release'

for pinned in 0.9.0 v0.9.0; do
  pin_name=$(printf '%s' "$pinned" | tr -d '.')
  pin_dir="$test_root/pin-$pin_name"
  mkdir -p "$pin_dir/home" "$pin_dir/tmp"
  make_tool_bin "$pin_dir" Linux x86_64 yes
  STUB_ARCHIVE_NAME=hum-0.9.0-linux-x64.tar.gz
  export STUB_ARCHIVE_NAME
  make_fixture "$pin_dir" "$STUB_ARCHIVE_NAME" 0.9.0
  run_installer "$pin_dir" env HUM_VERSION="$pinned"
  grep -q '/releases/download/v0.9.0/hum-0.9.0-linux-x64.tar.gz$' "$pin_dir/urls.log" || fail "pin $pinned resolved incorrectly"
  pass "HUM_VERSION=$pinned selects tag v0.9.0 and asset 0.9.0"
done

default_dir="$test_root/default-dir"
mkdir -p "$default_dir/home" "$default_dir/tmp"
printf 'unchanged\n' >"$default_dir/home/.profile"
make_tool_bin "$default_dir" Linux x86_64 yes
STUB_ARCHIVE_NAME=hum-0.9.0-linux-x64.tar.gz
export STUB_ARCHIVE_NAME
make_fixture "$default_dir" "$STUB_ARCHIVE_NAME" 0.9.0
env PATH="$default_dir/bin" HOME="$default_dir/home" TMPDIR="$default_dir/tmp" \
  STUB_LOG="$default_dir/urls.log" STUB_LATEST_TAG=v0.9.0 \
  STUB_ARCHIVE_NAME="$STUB_ARCHIVE_NAME" STUB_ARCHIVE="$default_dir/$STUB_ARCHIVE_NAME" \
  STUB_CHECKSUMS="$default_dir/checksums.txt" HUM_VERSION=0.9.0 \
  /bin/sh "$root/install.sh" >"$default_dir/stdout" 2>"$default_dir/stderr"
[ -x "$default_dir/home/.local/bin/hum" ] || fail 'default install directory was not created'
[ "$(cat "$default_dir/home/.profile")" = unchanged ] || fail 'installer edited a shell startup file'
grep -q "Warning: $default_dir/home/.local/bin is not in PATH" "$default_dir/stderr" || fail 'missing PATH warning'
! grep -q sudo "$root/install.sh" || fail 'installer invokes sudo'
pass 'default and overridden destinations are created safely and PATH warning is emitted'

fault_case() {
  mode=$1
  case_dir="$test_root/fault-$mode"
  archive_name=hum-0.9.0-linux-x64.tar.gz
  mkdir -p "$case_dir/home" "$case_dir/tmp" "$case_dir/install"
  printf 'original target\n' >"$case_dir/install/hum"
  cp "$case_dir/install/hum" "$case_dir/original"
  if [ "$mode" = missing-tool ]; then
    make_tool_bin "$case_dir" Linux x86_64 no
  else
    make_tool_bin "$case_dir" Linux x86_64 yes
  fi
  make_fixture "$case_dir" "$archive_name" 0.9.0
  STUB_FAIL=
  case "$mode" in
    missing-entry) printf '%064d  ./other.tar.gz\n' 0 >"$case_dir/checksums.txt" ;;
    mismatch) printf '%064d  ./%s\n' 0 "$archive_name" >"$case_dir/checksums.txt" ;;
    download) STUB_FAIL=archive ;;
    malformed)
      printf 'not an archive\n' >"$case_dir/$archive_name"
      printf '%s  ./%s\n' "$(host_sha "$case_dir/$archive_name")" "$archive_name" >"$case_dir/checksums.txt"
      ;;
    missing-tool) ;;
  esac
  export STUB_FAIL
  STUB_ARCHIVE_NAME=$archive_name
  export STUB_ARCHIVE_NAME
  if run_installer "$case_dir" env HUM_VERSION=0.9.0; then
    fail "$mode unexpectedly succeeded"
  fi
  cmp "$case_dir/original" "$case_dir/install/hum" >/dev/null || fail "$mode changed the existing target"
  if find "$case_dir/tmp" -mindepth 1 -print -quit | grep -q .; then
    fail "$mode left temporary files behind"
  fi
  if find "$case_dir/install" -name '.hum-install.*' -print -quit | grep -q .; then
    fail "$mode left an install staging file behind"
  fi
  pass "$mode fails cleanly and preserves the target"
}

fault_case missing-entry
fault_case mismatch
fault_case download
fault_case malformed
fault_case missing-tool

printf 'All installer tests passed.\n'
