package host

import contracts "github.com/Herrscherd/herrscher-contracts"

// GateFor reports how finely a vendor can enforce an approval policy, and why
// it cannot when it cannot. It reads the compiled backends' manifests, which is
// what makes it answerable before any session exists: the operator is told at
// create time whether the mode they asked for will bite, and asking a live
// backend would mean spawning a vendor CLI for a session that has not started.
//
// An unknown or unnamed vendor is not an error here. A session can legitimately
// run a free-form cmd whose backend nothing on this side names, and the honest
// answer for it is the same as for a backend that enforces nothing.
func GateFor(vendor string) (grain, why string) {
	if vendor == "" {
		return "", "no backend is named on this session, so nothing here can say what it enforces"
	}
	for _, p := range contracts.Default.Backends() {
		if p.Manifest.Kind != vendor {
			continue
		}
		c := p.Manifest.Capabilities
		return string(c.Gate), c.GateWhy
	}
	return "", "no backend named " + vendor + " is compiled into this daemon"
}
