//go:build windows

package network

import (
	"net"
	"syscall"
)

// ReuseAddrListenConfig returns a net.ListenConfig that sets SO_REUSEADDR
// on the socket before binding. This allows immediate rebinding to ports
// that are in TIME_WAIT state after a previous process was killed.
func ReuseAddrListenConfig() net.ListenConfig {
	return net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			})
		},
	}
}
