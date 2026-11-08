package host

import contracts "github.com/Herrscherd/herrscher-contracts"

// SupportedRemoteCategory reports whether c can be hosted or resolved remotely.
func SupportedRemoteCategory(c contracts.Category) bool {
	switch c {
	case contracts.CategoryMemory, contracts.CategoryOrchestrator, contracts.CategoryBackend:
		return true
	case contracts.CategoryGateway, contracts.CategorySkills, contracts.CategoryCompactor:
		return false
	}
	return false
}
