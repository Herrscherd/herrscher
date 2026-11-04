package state

// TreeChain lists name and its ancestors in all, from name upward; a repeated name ends the walk.
func TreeChain(all []Session, name string) []string {
	parents := make(map[string]string, len(all))
	for idx := range all {
		parents[all[idx].Name] = all[idx].Parent
	}
	chain := []string{name}
	seen := map[string]bool{name: true}
	current := name
	for {
		parent := parents[current]
		if parent == "" {
			return chain
		}
		chain = append(chain, parent)
		if seen[parent] {
			return chain
		}
		seen[parent] = true
		current = parent
	}
}

// TreeRoot returns the topmost session of name tree that exists in all.
func TreeRoot(all []Session, name string) string {
	known := make(map[string]bool, len(all))
	for idx := range all {
		known[all[idx].Name] = true
	}
	root := name
	for _, member := range TreeChain(all, name) {
		if known[member] {
			root = member
		}
	}
	return root
}
