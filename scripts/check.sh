#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
export GOWORK=off

capped() {
	if [ -n "${CI:-}" ]; then
		"$@"
	else
		systemd-run --user --scope -p MemoryMax=6G -q "$@"
	fi
}

regenerated_copy=$(mktemp -d)
trap 'rm -rf "$regenerated_copy"' EXIT

regenerated_from_scratch() {
	cp -a . "$regenerated_copy"
	local generated_file
	find "$regenerated_copy" -name '*.go' -type f -not -path '*/.git/*' -exec awk '
		FNR == 1 { before_package = 1 }
		/^package / { before_package = 0 }
		before_package && /^\/\/ Code generated .* DO NOT EDIT\.$/ { print FILENAME; nextfile }
	' {} + | while IFS= read -r generated_file; do
		rm -- "$generated_file"
	done
	(cd "$regenerated_copy" && capped go generate ./...)
	diff -r --exclude=.git . "$regenerated_copy"
}

no_dash_punctuation() {
	if git grep -nP '[\x{2013}\x{2014}]' -- ':!go.sum'; then
		echo "em or en dash used as punctuation" >&2
		return 1
	fi
}

golangci_lint=github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
govulncheck=golang.org/x/vuln/cmd/govulncheck@v1.9.0

go mod tidy -diff
go mod verify
capped go run "$golangci_lint" fmt --diff
capped go run "$golangci_lint" run ./...
go run ./tools/commentcheck .
go run ./tools/i18ncheck -baseline tools/i18ncheck/baseline.txt .
regenerated_from_scratch
no_dash_punctuation
capped go test -race -shuffle=on ./...
CGO_ENABLED=0 capped go build ./...
CGO_ENABLED=0 GOOS=windows capped go vet ./...
CGO_ENABLED=0 GOOS=darwin capped go build ./...
capped go run "$govulncheck" ./...
