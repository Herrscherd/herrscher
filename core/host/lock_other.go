//go:build !unix

package host

import "os"

// tryLock always succeeds off unix: there is no portable advisory lock here, and
// a guard that refused to start on a platform it cannot check would cost more
// than the double-answer it prevents. The daemon runs unsupervised on unix in
// practice — the systemd and launchd units are what start it.
func tryLock(*os.File) (bool, error) { return true, nil }
