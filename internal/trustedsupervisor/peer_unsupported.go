//go:build !darwin

package trustedsupervisor

import (
	"context"
	"fmt"
	"net"

	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
)

type unixPeerChannelBinder struct{}
type socketFileIdentity struct{}

func (unixPeerChannelBinder) Bind(context.Context, net.Conn, uint32, int32, string) (AuthenticatedChannel, error) {
	return AuthenticatedChannel{}, fmt.Errorf("%w: Unix peer credentials unsupported on this platform", ErrAuthentication)
}

func validateSocketFile(string, uint32) (socketFileIdentity, error) {
	return socketFileIdentity{}, fmt.Errorf("%w: Unix peer credentials unsupported on this platform", ErrAuthentication)
}

// Ensure transportprimitives is referenced even on unsupported platforms.
var _ = transportprimitives.ErrTransport
