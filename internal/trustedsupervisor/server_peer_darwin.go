//go:build darwin

package trustedsupervisor

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

type connectedClientIdentity struct {
	ProcessID    int32
	PrimaryGroup uint32
	UserID       uint32
}

func inspectConnectedClient(ctx context.Context, connection net.Conn, expectedUserID uint32) (connectedClientIdentity, error) {
	if ctx == nil || ctx.Err() != nil {
		return connectedClientIdentity{}, ErrDeadline
	}
	rawConnection, ok := connection.(syscall.Conn)
	if !ok {
		return connectedClientIdentity{}, authenticationError("Unix client descriptor")
	}
	raw, err := rawConnection.SyscallConn()
	if err != nil {
		return connectedClientIdentity{}, authenticationError("acquire Unix client descriptor")
	}
	var credentials *unix.Xucred
	var processID int
	var credentialErr, processErr error
	if err := raw.Control(func(descriptor uintptr) {
		credentials, credentialErr = unix.GetsockoptXucred(int(descriptor), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		processID, processErr = unix.GetsockoptInt(int(descriptor), unix.SOL_LOCAL, localPeerPID)
	}); err != nil {
		return connectedClientIdentity{}, authenticationError("inspect Unix client")
	}
	if credentialErr != nil || processErr != nil || credentials == nil || credentials.Uid != expectedUserID || processID <= 0 {
		return connectedClientIdentity{}, authenticationError("unexpected connected Unix client credentials")
	}
	if err := unix.Kill(processID, 0); err != nil {
		return connectedClientIdentity{}, authenticationError("connected Unix client process")
	}
	primaryGroup := uint32(0)
	if credentials.Ngroups > 0 {
		primaryGroup = credentials.Groups[0]
	}
	return connectedClientIdentity{ProcessID: int32(processID), PrimaryGroup: primaryGroup, UserID: credentials.Uid}, nil
}
