//go:build darwin

package trustedsupervisor

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const localPeerPID = 0x002

type unixPeerChannelBinder struct{}

type socketFileIdentity struct {
	device uint64
	inode  uint64
}

func (unixPeerChannelBinder) Bind(ctx context.Context, connection net.Conn, expectedUserID uint32, expectedProcessID int32, payloadHash string) (AuthenticatedChannel, error) {
	if err := ctx.Err(); err != nil {
		return AuthenticatedChannel{}, fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	rawConnection, ok := connection.(syscall.Conn)
	if !ok {
		return AuthenticatedChannel{}, authenticationError("Unix connection has no raw descriptor")
	}
	raw, err := rawConnection.SyscallConn()
	if err != nil {
		return AuthenticatedChannel{}, authenticationError("acquire Unix descriptor")
	}
	var credentials *unix.Xucred
	var processID int
	var credentialErr, processErr error
	if err := raw.Control(func(descriptor uintptr) {
		credentials, credentialErr = unix.GetsockoptXucred(int(descriptor), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		processID, processErr = unix.GetsockoptInt(int(descriptor), unix.SOL_LOCAL, localPeerPID)
	}); err != nil {
		return AuthenticatedChannel{}, authenticationError("inspect Unix peer")
	}
	if credentialErr != nil || processErr != nil || credentials == nil || credentials.Uid != expectedUserID || int32(processID) != expectedProcessID {
		return AuthenticatedChannel{}, authenticationError("unexpected connected Unix peer credentials")
	}
	if err := ctx.Err(); err != nil {
		return AuthenticatedChannel{}, fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	primaryGroupID := uint32(0)
	if credentials.Ngroups > 0 {
		primaryGroupID = credentials.Groups[0]
	}
	binding, err := canonicalHash(map[string]any{
		"binding_schema_version": "ananke.local-unix-peer-channel-binding.v2",
		"peer_primary_group_id":  primaryGroupID,
		"peer_process_id":        processID,
		"peer_user_id":           credentials.Uid,
		"request_payload_hash":   payloadHash,
	})
	if err != nil {
		return AuthenticatedChannel{}, authenticationError("derive channel binding")
	}
	return AuthenticatedChannel{BindingHash: binding, PeerUserID: credentials.Uid, PeerProcessID: int32(processID)}, nil
}

func validateSocketFile(socketPath string, expectedUserID uint32) (socketFileIdentity, error) {
	information, err := os.Lstat(socketPath)
	if err != nil {
		return socketFileIdentity{}, fmt.Errorf("%w: inspect configured Unix socket: %v", ErrAuthentication, err)
	}
	if information.Mode()&os.ModeSocket == 0 || information.Mode().Perm()&0o077 != 0 {
		return socketFileIdentity{}, authenticationError("configured endpoint is not a private Unix socket")
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != expectedUserID {
		return socketFileIdentity{}, authenticationError("configured Unix socket owner")
	}
	return socketFileIdentity{device: uint64(status.Dev), inode: status.Ino}, nil
}
