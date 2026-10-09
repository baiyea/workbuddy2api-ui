package main

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestDesktopParentShutdown(t *testing.T) {
	for _, command := range []string{"shutdown\n", "shutdown\r\n", ""} {
		t.Run(command, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go watchDesktopParent(reader, cancel)
			if command == "" {
				writer.Close()
			} else {
				if _, err := io.WriteString(writer, command); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("parent shutdown did not cancel core lifecycle")
			}
		})
	}
}
func TestDesktopParentIgnoresUnknownCommands(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchDesktopParent(reader, cancel)
	if _, err := io.WriteString(writer, "status\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal("unknown command cancelled lifecycle")
	default:
	}
	writer.Close()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("EOF ignored")
	}
}
