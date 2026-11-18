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

e2e_budget_seconds=120
e2e_cgroup_env=(PATH HOME GOPATH GOCACHE GOMODCACHE GOFLAGS GOWORK CGO_ENABLED GOTOOLCHAIN)

e2e_capped() {
	if [ -z "${CI:-}" ]; then
		capped "$@"
		return
	fi
	local name assignments=()
	for name in "${e2e_cgroup_env[@]}"; do
		if [ -n "${!name+x}" ]; then
			assignments+=("$name=${!name}")
		fi
	done
	sudo env "${assignments[@]}" systemd-run --scope --quiet -p MemoryMax=6G -p MemorySwapMax=0 --uid="$(id -u)" --gid="$(id -g)" -- "$@"
}

e2e_cgroup_control() {
	e2e_capped sh -c 'test "$(cat "/sys/fs/cgroup$(sed -n "s/^0:://p" /proc/self/cgroup)/memory.max")" = 6442450944'
}

regenerated_copy=$(mktemp -d)
lint_tools=$(mktemp -d)
trap 'rm -rf "$regenerated_copy" "$lint_tools"' EXIT

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

e2e_only_tests() {
	local pkg=$1
	local untagged tagged only
	if ! untagged=$(capped go test -list '^Test' "$pkg"); then
		echo "$pkg: go test -list failed without tags" >&2
		return 1
	fi
	if ! tagged=$(capped go test -tags e2e -list '^Test' "$pkg"); then
		echo "$pkg: go test -list failed with -tags e2e" >&2
		return 1
	fi
	only=$(comm -13 <(grep '^Test' <<<"$untagged" | sort -u) <(grep '^Test' <<<"$tagged" | sort -u))
	if [ -z "$only" ]; then
		echo "$pkg: no test exists only under -tags e2e, the e2e pass would run nothing" >&2
		return 1
	fi
	printf '%s\n' "$only"
}

golangci_lint=github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
govulncheck=golang.org/x/vuln/cmd/govulncheck@v1.9.0

if [ "$(uname -s)" = Linux ]; then
	host_only=$(e2e_only_tests ./core/host/)
	e2e_only_tests ./e2e/... > /dev/null
	if [ -n "${CI:-}" ]; then
		e2e_cgroup_control
	fi
	e2e_capped go test -tags e2e -run '^$' ./e2e/... ./core/host/ .
	started=$SECONDS
	e2e_capped go test -tags e2e -count=1 -timeout 2m ./e2e/...
	e2e_capped go test -tags e2e -count=1 -timeout 2m -v -run "^($(paste -sd'|' <<<"$host_only"))\$" ./core/host/
	e2e_capped go test -tags e2e -count=1 -timeout 2m -run '^TestE2EAttachedWindow' .
	elapsed=$((SECONDS - started))
	if [ "$elapsed" -gt "$e2e_budget_seconds" ]; then
		echo "the e2e suite took ${elapsed}s, over the ${e2e_budget_seconds}s budget" >&2
		exit 1
	fi
else
	echo "skipping the e2e scenarios: they run on Linux only" >&2
fi

GOBIN=$lint_tools capped go install "$golangci_lint"
go mod tidy -diff
go mod verify
capped go run "$golangci_lint" fmt --diff
capped go run "$golangci_lint" run --build-tags e2e ./...
capped go run "$golangci_lint" run ./...
GOOS=windows capped "$lint_tools/golangci-lint" run ./...
go run ./tools/commentcheck .
go run ./tools/i18ncheck .
regenerated_from_scratch
no_dash_punctuation
capped go test -race -shuffle=on ./...
CGO_ENABLED=0 capped go build ./...
CGO_ENABLED=0 GOOS=windows capped go vet ./...
CGO_ENABLED=0 GOOS=darwin capped go build ./...
capped go run "$govulncheck" ./...
