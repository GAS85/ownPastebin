//go:build !alpine

// We could exclude all linux builds here, but as soon as this solutions is only needed for an alpine image, simply use tag: !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !solaris && !aix

package main

func reapZombies() {
	// No SIGCHLD/Wait4 equivalent is needed on non-Unix platforms.
}
