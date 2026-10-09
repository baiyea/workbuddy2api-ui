//go:build !windows

package scheduler

import "os/exec"

func configureDesktopScript(command *exec.Cmd) {}
