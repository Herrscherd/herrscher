# Maintainer: Akayashuu <sauvageleo1@gmail.com>
pkgname=herrscher
pkgdesc="Self-hosted harness for running fleets of AI coding agents"
pkgver=0.63.0
pkgrel=1
arch=('x86_64' 'aarch64')
url="https://github.com/Herrscherd/herrscher"
license=('MIT')
depends=('glibc')
makedepends=('go' 'git')
provides=('herrscherd')
conflicts=('herrscherd')
replaces=('herrscherd')
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
	export GOWORK=off
	export CGO_ENABLED=0
	export GOFLAGS="-trimpath -mod=readonly -modcacherw"
	go build -ldflags "-s -w" -o "$pkgname" .
}

package() {
	cd "$srcdir/$pkgname"
	install -Dm755 "$pkgname" "$pkgdir/usr/bin/$pkgname"
	install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
