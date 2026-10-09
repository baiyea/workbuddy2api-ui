//go:build !windows

package scheduler

import (
	"context"
	"testing"
)

func TestDesktopScriptKeepsNativeProcessDefaults(t *testing.T) {
	for _, mode := range []string{"", "false", "true"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("WB2A_DESKTOP", mode)
			command := newScriptCmd(context.Background(), "python3", "task.py").(*scriptCmd).cmd
			if command.SysProcAttr != nil {
				t.Fatal("non-Windows script process settings changed")
			}
		})
	}
}
