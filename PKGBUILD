# Maintainer: Akayashuu <sauvageleo1@gmail.com>
pkgname=herrscherd
pkgdesc="Herrscher host + Discord bot CLI: a modular Discord<->Claude harness"
pkgver=1.1.0
pkgrel=1
arch=('x86_64')
url="https://github.com/Herrscherd/herrscher"
license=('MIT')
depends=('glibc')
makedepends=('go' 'git')
# Pure-upstream build from GitHub master. For a local-checkout build instead,
# swap to: git+file:///home/shan/dev/herrscher#branch=master
source=("$pkgname::git+https://github.com/Herrscherd/herrscher.git#branch=master")
sha256sums=('SKIP')
options=('!debug')

pkgver() {
	cd "$srcdir/$pkgname"
	local d
	d=$(git describe --tags 2>/dev/null) &&
		echo "$d" | sed 's/^v//;s/-/.r/;s/-/./g' ||
		printf 'r%s.%s' "$(git rev-list --count HEAD)" "$(git rev-parse --short HEAD)"
}

build() {
	cd "$srcdir/$pkgname"
	# The clone has no go.work in its parents; pin it off and resolve the
	# published plugin modules from the proxy (same as a fresh GitHub build).
	export GOWORK=off
	export CGO_ENABLED=0
	export GOFLAGS="-trimpath -mod=readonly -modcacherw"
	go build -ldflags "-s -w" -o "$pkgname" .
}

package() {
	cd "$srcdir/$pkgname"
	install -Dm755 "$pkgname" "$pkgdir/usr/bin/$pkgname"
}
