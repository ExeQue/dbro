//go:build !linux

package main

// unlockWindowSize is only needed for GTK on Linux.
func unlockWindowSize() {}
