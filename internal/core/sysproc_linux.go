package core

import "syscall"

// The cores die with zgzd, so a crashed zgzd never leaves a core behind
// that the next zgzd does not know about.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
