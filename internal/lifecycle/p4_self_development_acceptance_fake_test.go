package lifecycle

import (
	"context"
	"errors"
	"sync"

	"github.com/yingliang-zhang/ananke/internal/store"
)

// p4AcceptanceFakeVerifier is an in-process test double. Its only input and
// output are the closed P4 value types; it has no transport, process, OMP,
// source, artifact, or repair capability.
type p4AcceptanceFakeVerifier struct {
	mu       sync.Mutex
	expected store.P4VerifierRequest
	response store.P4VerifierResponse
	calls    int
}

func newP4AcceptanceFakeVerifier(fact store.P4EvidenceAdmission) *p4AcceptanceFakeVerifier {
	return &p4AcceptanceFakeVerifier{
		expected: fact.VerifierRequest,
		response: store.P4VerifierResponse{
			Output: fact.VerifierOutput,
			Replay: fact.VerifierReplay,
		},
	}
}

func (fake *p4AcceptanceFakeVerifier) VerifyP4Evidence(_ context.Context, request store.P4VerifierRequest) (store.P4VerifierResponse, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if request != fake.expected {
		return store.P4VerifierResponse{}, errors.New("P4 acceptance fake verifier rejected a non-canonical request")
	}
	fake.calls++
	return fake.response, nil
}

func (fake *p4AcceptanceFakeVerifier) callCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.calls
}
