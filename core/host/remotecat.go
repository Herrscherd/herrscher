package host

import contracts "github.com/Herrscherd/herrscher-contracts"

// supportedRemoteCategories names the plugin categories that can run
// out-of-process today. Hosting (plugin-host) or resolving a category remotely
// is gated on membership here, so adding one is a registration plus its
// dial/skeleton wiring — not a `category == "memory"` branch scattered across
// the host. memory is the sole entry until Spec C grows it (orchestrator,
// then the streaming backend).
var supportedRemoteCategories = map[contracts.Category]bool{
	contracts.CategoryMemory: true,
}

// SupportedRemoteCategory reports whether c can be hosted or resolved remotely.
// An unsupported category stays in-process (the host warns and skips it).
func SupportedRemoteCategory(c contracts.Category) bool {
	return supportedRemoteCategories[c]
}
