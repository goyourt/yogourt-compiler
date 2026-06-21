//go:build windows

package compiler

import (
	"os"
	"os/exec"
)

func runtimeStopSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func prepareRuntimeCommand(_ *exec.Cmd) {}

func interruptRuntimeProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}

func killRuntimeProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
