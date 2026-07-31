//go:build darwin

package trustedsupervisor

import (
	"context"
	"fmt"
	"net"

	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
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
	creds, err := transportprimitives.BindUnixPeer(connection, expectedUserID, expectedProcessID, payloadHash)
	if err != nil {
		return AuthenticatedChannel{}, fmt.Errorf("%w: %v", ErrAuthentication, err)
	}
	if err := ctx.Err(); err != nil {
		return AuthenticatedChannel{}, fmt.Errorf("%w: %v", ErrDeadline, err)
	}
	return AuthenticatedChannel{BindingHash: creds.BindingHash, PeerUserID: creds.PeerUserID, PeerProcessID: creds.PeerProcessID}, nil
}

func validateSocketFile(socketPath string, expectedUserID uint32) (socketFileIdentity, error) {
	identity, err := transportprimitives.ValidateSocketFile(socketPath, expectedUserID)
	if err != nil {
		return socketFileIdentity{}, fmt.Errorf("%w: %v", ErrAuthentication, err)
	}
	return socketFileIdentity{device: identity.Device, inode: identity.Inode}, nil
}
