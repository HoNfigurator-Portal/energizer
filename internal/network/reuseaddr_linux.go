//go:build linux

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
			var opErr error
			err := c.Control(func(fd uintptr) {
				opErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			})
			if err != nil {
				return err
			}
			return opErr
		},
	}
}
