#!/bin/sh
set -eu

repo=Herrscherd/herrscher

fail() {
	echo "herrscher install: $*" >&2
	exit 1
}

if [ -z "${HERRSCHER_INSTALL_DIR:-}" ] && [ -z "${HOME:-}" ]; then
	fail "HOME is not set: set HERRSCHER_INSTALL_DIR to the directory to install into"
fi
dir=${HERRSCHER_INSTALL_DIR:-$HOME/.local/bin}

need() {
	command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

need curl
need tar
need uname

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "no prebuilt binary for $(uname -s): use go install github.com/$repo@latest" ;;
esac

case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "no prebuilt binary for $(uname -m): use go install github.com/$repo@latest" ;;
esac

tag=${HERRSCHER_VERSION:-}
if [ -z "$tag" ]; then
	latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
		fail "could not reach the latest release of $repo: set HERRSCHER_VERSION to a tag"
	tag=${latest##*/}
fi
case $tag in
v[0-9]*) ;;
*) fail "could not find the latest release (got '$tag')" ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
	sum="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
	sum="shasum -a 256"
else
	fail "sha256sum or shasum is required"
fi

name="herrscher_${tag}_${os}_${arch}"
base="https://github.com/$repo/releases/download/$tag"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

curl -fsSL -o "$work/$name.tar.gz" "$base/$name.tar.gz" || fail "no archive $name.tar.gz in $tag"
curl -fsSL -o "$work/checksums.txt" "$base/checksums.txt" || fail "no checksums.txt in $tag"

expected=$(awk -v file="$name.tar.gz" '$2 == file { print $1 }' "$work/checksums.txt")
[ -n "$expected" ] || fail "checksums.txt has no line for $name.tar.gz"
actual=$(cd "$work" && $sum "$name.tar.gz" | awk '{ print $1 }')
[ "$expected" = "$actual" ] || fail "checksum mismatch for $name.tar.gz"

tar -C "$work" -xzf "$work/$name.tar.gz"
mkdir -p "$dir"
install -m 755 "$work/$name/herrscher" "$dir/herrscher"

echo "herrscher $tag installed in $dir"
case ":$PATH:" in
*":$dir:"*) ;;
*) echo "add $dir to your PATH" ;;
esac
echo "next: herrscher setup"
