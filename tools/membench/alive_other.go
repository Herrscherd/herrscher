//go:build !unix

package main

func processAlive(int) bool {
	return true
}
