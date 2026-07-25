//go:build !darwin

package trustedsupervisor

import (
	"context"
	"net"
)

type connectedClientIdentity struct {
	ProcessID    int32
	PrimaryGroup uint32
	UserID       uint32
}

func inspectConnectedClient(context.Context, net.Conn, uint32) (connectedClientIdentity, error) {
	return connectedClientIdentity{}, authenticationError("Unix client credentials unsupported on this platform")
}
