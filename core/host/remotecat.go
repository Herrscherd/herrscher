package host

import contracts "github.com/Herrscherd/herrscher-contracts"

var supportedRemoteCategories = map[contracts.Category]bool{
	contracts.CategoryMemory:       true,
	contracts.CategoryOrchestrator: true,
	contracts.CategoryBackend:      true,
}

// SupportedRemoteCategory reports whether c can be hosted or resolved remotely.
// An unsupported category stays in-process (the host warns and skips it).
func SupportedRemoteCategory(c contracts.Category) bool {
	return supportedRemoteCategories[c]
}
