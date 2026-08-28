//go:build alpine

// We could include all linux builds here, but as soon as this solutions is only needed for an alpine image, simply use tag: linux || darwin || freebsd || openbsd || netbsd || dragonfly || solaris || aix

package main

import (
	"log/slog"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
)

// reapZombies runs for the lifetime of the process and reaps any zombie
// children whenever SIGCHLD is delivered.
//
// Why this is needed
// ──────────────────
// In Alpine-based containers the Go binary often runs as PID 1.  PID 1 is
// special: the kernel never auto-reaps its direct children, so every process
// that exits while still having Go as its parent becomes a zombie until PID 1
// calls wait(2).  Go's runtime does not do this automatically.
//
// The immediate culprit is the Docker / Kubernetes health-check command, which
// on Alpine is typically:
//
//	wget -q -O /dev/null https://localhost:8080/config
//
// Busybox wget forks an ssl_client helper process for each TLS connection.
// When wget exits it does not always reap ssl_client synchronously, leaving
// it as a zombie child of PID 1 (this Go process).  One zombie appears per
// health-check cycle — hence the steady accumulation every ~5 minutes.
//
// The fix: listen for SIGCHLD and call waitpid(-1, WNOHANG) in a tight loop
// until there are no more children to reap.

func reapZombies() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, unix.SIGCHLD)
	defer signal.Stop(ch)

	for range ch {
		for {
			var ws unix.WaitStatus

			pid, err := unix.Wait4(-1, &ws, unix.WNOHANG, nil)
			if pid <= 0 || err != nil {
				break
			}

			slog.Debug("reaped zombie child", "pid", pid, "status", ws)
		}
	}
}
