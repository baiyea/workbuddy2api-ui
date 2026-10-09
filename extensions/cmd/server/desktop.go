package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func coreLifecycle() (context.Context, context.CancelFunc) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	if os.Getenv("WB2A_DESKTOP") == "true" {
		go watchDesktopParent(os.Stdin, cancel)
	}
	return ctx, cancel
}

// Only the native launcher owns stdin. EOF also covers a crashed launcher;
// malformed/failed reads fail closed instead of leaving an orphan service.
func watchDesktopParent(input io.Reader, cancel context.CancelFunc) {
	defer cancel()
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		if scanner.Text() == "shutdown" {
			return
		}
	}
}
