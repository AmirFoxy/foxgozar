package core

import "syscall"

// The cores die with foxgozar, so a crashed foxgozar never leaves a core behind
// that the next foxgozar does not know about.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
