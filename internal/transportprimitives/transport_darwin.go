//go:build darwin

package transportprimitives

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// ErrTransport is the sentinel for Unix-transport violations.
var ErrTransport = errors.New("transport error")

const localPeerPID = 0x002

// UnixPeerCredentials holds the connected Unix peer metadata and the
// derived channel-binding hash.
type UnixPeerCredentials struct {
	BindingHash   string
	PeerUserID    uint32
	PeerProcessID int32
}

// BindUnixPeer validates the connected Unix socket peer credentials
// against the expected user/process IDs and derives a per-connection
// channel-binding hash over the payload hash.
func BindUnixPeer(connection net.Conn, expectedUserID uint32, expectedProcessID int32, payloadHash string) (UnixPeerCredentials, error) {
	rawConnection, ok := connection.(syscall.Conn)
	if !ok {
		return UnixPeerCredentials{}, fmt.Errorf("%w: Unix connection has no raw descriptor", ErrTransport)
	}
	raw, err := rawConnection.SyscallConn()
	if err != nil {
		return UnixPeerCredentials{}, fmt.Errorf("%w: acquire Unix descriptor", ErrTransport)
	}
	var credentials *unix.Xucred
	var processID int
	var credentialErr, processErr error
	if err := raw.Control(func(descriptor uintptr) {
		credentials, credentialErr = unix.GetsockoptXucred(int(descriptor), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		processID, processErr = unix.GetsockoptInt(int(descriptor), unix.SOL_LOCAL, localPeerPID)
	}); err != nil {
		return UnixPeerCredentials{}, fmt.Errorf("%w: inspect Unix peer", ErrTransport)
	}
	if credentialErr != nil || processErr != nil || credentials == nil || credentials.Uid != expectedUserID || int32(processID) != expectedProcessID {
		return UnixPeerCredentials{}, fmt.Errorf("%w: unexpected connected Unix peer credentials", ErrTransport)
	}
	primaryGroupID := uint32(0)
	if credentials.Ngroups > 0 {
		primaryGroupID = credentials.Groups[0]
	}
	binding, err := CanonicalHash(map[string]any{
		"binding_schema_version": "ananke.local-unix-peer-channel-binding.v2",
		"peer_primary_group_id":  primaryGroupID,
		"peer_process_id":        processID,
		"peer_user_id":           credentials.Uid,
		"request_payload_hash":   payloadHash,
	})
	if err != nil {
		return UnixPeerCredentials{}, fmt.Errorf("%w: derive channel binding", ErrTransport)
	}
	return UnixPeerCredentials{BindingHash: binding, PeerUserID: credentials.Uid, PeerProcessID: int32(processID)}, nil
}

// SocketFileIdentity records the device and inode of a validated Unix socket.
type SocketFileIdentity struct {
	Device uint64
	Inode  uint64
}

// ValidateSocketFile verifies that socketPath is a private (0600) Unix
// socket owned by expectedUserID and returns its device/inode identity.
func ValidateSocketFile(socketPath string, expectedUserID uint32) (SocketFileIdentity, error) {
	information, err := os.Lstat(socketPath)
	if err != nil {
		return SocketFileIdentity{}, fmt.Errorf("%w: inspect configured Unix socket: %v", ErrTransport, err)
	}
	if information.Mode()&os.ModeSocket == 0 || information.Mode().Perm()&0o077 != 0 {
		return SocketFileIdentity{}, fmt.Errorf("%w: configured endpoint is not a private Unix socket", ErrTransport)
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != expectedUserID {
		return SocketFileIdentity{}, fmt.Errorf("%w: configured Unix socket owner", ErrTransport)
	}
	return SocketFileIdentity{Device: uint64(status.Dev), Inode: status.Ino}, nil
}
