package host

import contracts "github.com/Herrscherd/herrscher-contracts"

func categorySet(categories ...contracts.Category) map[contracts.Category]bool {
	set := make(map[contracts.Category]bool, len(categories))
	for _, c := range categories {
		set[c] = true
	}
	return set
}
