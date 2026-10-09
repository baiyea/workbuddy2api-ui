package scheduler

import (
	"context"
	"testing"
)

func TestDesktopScriptHasNoConsoleWindow(t *testing.T) {
	for _, mode := range []string{"", "false", "true"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("WB2A_DESKTOP", mode)
			command := newScriptCmd(context.Background(), "python.exe", "task.py").(*scriptCmd).cmd
			if mode != "true" {
				if command.SysProcAttr != nil {
					t.Fatal("non-desktop script process settings changed")
				}
				return
			}
			if command.SysProcAttr == nil || !command.SysProcAttr.HideWindow || command.SysProcAttr.CreationFlags&0x08000000 == 0 {
				t.Fatal("desktop Python process can create a visible console window")
			}
		})
	}
}
