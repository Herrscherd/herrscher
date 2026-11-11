//go:build !unix

package runtimeguard

func instanceLockHeld(string) bool { return false }
