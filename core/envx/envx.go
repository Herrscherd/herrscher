// Package envx resolves the daemon's environment variables under the
// HERRSCHER_ prefix.
package envx

import "os"

// Get returns the value of HERRSCHER_<suffix> (possibly empty). Pass the bare
// suffix, e.g. Get("INSTANCE_ID").
func Get(suffix string) string {
	return os.Getenv("HERRSCHER_" + suffix)
}
