//go:build !unix

package mcpweight

func spawnGrandchildFor(_ string) error {
	return nil
}
