#!/usr/bin/env bash
set -euo pipefail

if [ $# -ne 2 ]; then
	echo "usage: $0 <tag> <output directory>" >&2
	exit 2
fi

tag=$1
out=$2
targets=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

mkdir -p "$out"
out=$(cd "$out" && pwd)

for target in "${targets[@]}"; do
	goos=${target%/*}
	goarch=${target#*/}
	name="herrscher_${tag}_${goos}_${goarch}"
	binary=herrscher
	if [ "$goos" = windows ]; then
		binary=herrscher.exe
	fi
	mkdir -p "$stage/$name"
	GOWORK=off CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
		go build -trimpath -ldflags "-s -w" -o "$stage/$name/$binary" .
	cp LICENSE README.md "$stage/$name/"
	if [ "$goos" = windows ]; then
		(cd "$stage" && zip -qr "$out/$name.zip" "$name")
	else
		tar -C "$stage" -czf "$out/$name.tar.gz" "$name"
	fi
done

(cd "$out" && sha256sum herrscher_"${tag}"_* > checksums.txt)
