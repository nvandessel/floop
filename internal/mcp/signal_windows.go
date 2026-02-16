//go:build windows

package mcp

import (
	"os"
	"os/signal"
)

// notifySignals registers OS signal handlers for graceful shutdown.
// On Windows, only os.Interrupt (Ctrl+C) is supported; SIGTERM does not exist.
func notifySignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt)
}
