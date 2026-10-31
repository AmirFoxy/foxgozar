//go:build !linux

package core

import "net"

// MarkedDialer: marks exist on Linux only; elsewhere (tests, development)
// a plain dialer.
func MarkedDialer(mark int) *net.Dialer { return &net.Dialer{} }
