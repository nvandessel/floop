//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// notifySignals registers OS signal handlers for graceful shutdown.
// On Unix systems, this includes both SIGINT and SIGTERM.
func notifySignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
}
