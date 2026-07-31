//go:build !darwin

package transportprimitives

import (
	"errors"
	"fmt"
	"net"
)

// ErrTransport is the sentinel for Unix-transport violations.
var ErrTransport = errors.New("transport error")

// UnixPeerCredentials holds the connected Unix peer metadata and the
// derived channel-binding hash.
type UnixPeerCredentials struct {
	BindingHash   string
	PeerUserID    uint32
	PeerProcessID int32
}

// BindUnixPeer returns an error on unsupported platforms.
func BindUnixPeer(net.Conn, uint32, int32, string) (UnixPeerCredentials, error) {
	return UnixPeerCredentials{}, fmt.Errorf("%w: Unix peer credentials unsupported on this platform", ErrTransport)
}

// SocketFileIdentity records the device and inode of a validated Unix socket.
type SocketFileIdentity struct {
	Device uint64
	Inode  uint64
}

// ValidateSocketFile returns an error on unsupported platforms.
func ValidateSocketFile(string, uint32) (SocketFileIdentity, error) {
	return SocketFileIdentity{}, fmt.Errorf("%w: Unix peer credentials unsupported on this platform", ErrTransport)
}
