// Package envx resolves the daemon's environment variables under the primary
// HERRSCHER_ prefix, transparently falling back to the legacy DCTL_ prefix.
//
// The daemon carried the legacy "dctl" name before the rebrand to Herrscher; its
// env vars kept that prefix. Reading HERRSCHER_<NAME> first and DCTL_<NAME> second
// lets new setups use the current names while pre-existing installs, service env
// files and shells that still export DCTL_* keep working unchanged.
package envx

import "os"

// Get returns the value of HERRSCHER_<suffix> if set and non-empty, else the
// value of the legacy DCTL_<suffix> (possibly empty). Pass the bare suffix,
// e.g. Get("INSTANCE_ID").
func Get(suffix string) string {
	if v := os.Getenv("HERRSCHER_" + suffix); v != "" {
		return v
	}
	return os.Getenv("DCTL_" + suffix)
}
