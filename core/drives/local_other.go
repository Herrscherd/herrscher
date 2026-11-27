//go:build !unix

package drives

import "os"

const (
	sourceOpenFlags  = os.O_RDONLY
	stagingOpenFlags = 0
)

func isSymlinkRefusal(error) bool {
	return false
}
