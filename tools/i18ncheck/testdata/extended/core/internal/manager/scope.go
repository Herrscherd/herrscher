package manager

import "errors"

var errNoLease = errors.New("manager has no active lease")
