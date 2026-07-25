package lifecycle

import (
	"context"
	"testing"

	"github.com/yingliang-zhang/ananke/internal/store"
	"github.com/yingliang-zhang/ananke/internal/trustedsupervisor"
)

// Compile-time proof that the production Unix client is injectable through the
// existing P3f external-supervisor seam without adding endpoint authority to it.
var _ externalSupervisorHandoffTransport = (*trustedsupervisor.Client)(nil)
var _ store.ExternalSupervisorAuthenticator = (*trustedsupervisor.Client)(nil)

func TestP3FUnixTransportInjectionPreservesClosedProjection(t *testing.T) {
	client, err := trustedsupervisor.NewClient(trustedsupervisor.Config{})
	if err == nil || client != nil {
		t.Fatal("empty operator transport configuration did not fail closed")
	}
	assertP3FExternalSupervisorFailClosed(t, (*externalSupervisorHandoffRuntime)(nil).recover(context.Background(), "remote_handoff_absent"))
}
