//go:build !linux

package core

import "syscall"

func sysProcAttr() *syscall.SysProcAttr { return nil }
