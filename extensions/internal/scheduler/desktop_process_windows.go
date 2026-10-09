package scheduler

import (
	"os"
	"os/exec"
	"syscall"
)

func configureDesktopScript(command *exec.Cmd) {
	if os.Getenv("WB2A_DESKTOP") == "true" {
		// CREATE_NO_WINDOW must be applied to Python itself; the launcher setting
		// on its Go parent does not propagate to subsequent CreateProcess calls.
		command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	}
}
