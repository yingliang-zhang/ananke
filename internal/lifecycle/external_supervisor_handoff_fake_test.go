package lifecycle

import (
	"context"
	"errors"
	"sync"

	"github.com/yingliang-zhang/ananke/internal/store"
)

// p3fInProcessFakeSupervisor is the sole test-only in-process transport and
// supervisor. It cannot open a connection, spawn a child, or access inputs.
type p3fInProcessFakeSupervisor struct {
	mu                       sync.Mutex
	root                     store.ExternalSupervisorTrustRoot
	receipts                 map[string]store.ExternalSupervisorAcceptanceReceipt
	callbacks                map[string]store.ExternalSupervisorCallback
	deliveryCount            int
	deliveryAttemptCount     int
	withheldDeliveryResponse bool
	reconcileCount           int
	cancelCount              int
	deliveryAttemptObserver  func(int)
}

func newP3FInProcessFakeSupervisor() *p3fInProcessFakeSupervisor {
	return &p3fInProcessFakeSupervisor{
		root:      p3fExternalSupervisorRoot(),
		receipts:  make(map[string]store.ExternalSupervisorAcceptanceReceipt),
		callbacks: make(map[string]store.ExternalSupervisorCallback),
	}
}

func (fake *p3fInProcessFakeSupervisor) Deliver(_ context.Context, envelope store.ExternalSupervisorEnvelope) (store.ExternalSupervisorAcceptanceReceipt, error) {
	if err := store.ValidateExternalSupervisorEnvelope(envelope); err != nil {
		return store.ExternalSupervisorAcceptanceReceipt{}, errors.New("fake supervisor requires a sealed envelope")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.deliveryAttemptCount++
	observer := fake.deliveryAttemptObserver
	if observer != nil {
		observer(fake.deliveryAttemptCount)
	}
	if receipt, found := fake.receipts[envelope.HandoffID]; found {
		if receipt.EnvelopeHash != envelope.EnvelopeHash || receipt.AttemptNumber != envelope.AttemptNumber {
			return store.ExternalSupervisorAcceptanceReceipt{}, errors.New("fake supervisor receipt conflict")
		}
		if fake.withheldDeliveryResponse {
			return store.ExternalSupervisorAcceptanceReceipt{}, errors.New("fake supervisor withheld delivery response")
		}
		return receipt, nil
	}
	fake.deliveryCount++
	receipt := store.ExternalSupervisorAcceptanceReceipt{
		SchemaVersion:       store.ExternalSupervisorReceiptSchemaVersion,
		HandoffID:           envelope.HandoffID,
		EnvelopeHash:        envelope.EnvelopeHash,
		ReceiptIdentityHash: p3fExternalSupervisorHash("receipt:" + envelope.HandoffID),
		AttemptNumber:       envelope.AttemptNumber,
		RootID:              fake.root.RootID,
		TrustBundleHash:     fake.root.TrustBundleHash,
		SignatureHash:       p3fExternalSupervisorHash("receipt-signature:" + envelope.HandoffID),
	}
	fake.receipts[envelope.HandoffID] = receipt
	if fake.withheldDeliveryResponse {
		return store.ExternalSupervisorAcceptanceReceipt{}, errors.New("fake supervisor withheld delivery response")
	}
	return receipt, nil
}

func (fake *p3fInProcessFakeSupervisor) withholdDeliveryResponse() {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.withheldDeliveryResponse = true
}

func (fake *p3fInProcessFakeSupervisor) releaseDeliveryResponse() {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.withheldDeliveryResponse = false
}

func (fake *p3fInProcessFakeSupervisor) receiptFor(handoffID string) (store.ExternalSupervisorAcceptanceReceipt, bool) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	receipt, found := fake.receipts[handoffID]
	return receipt, found
}

func (fake *p3fInProcessFakeSupervisor) replaceReceipt(receipt store.ExternalSupervisorAcceptanceReceipt) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.receipts[receipt.HandoffID] = receipt
}

func (fake *p3fInProcessFakeSupervisor) Reconcile(_ context.Context, receipt store.ExternalSupervisorAcceptanceReceipt) (*store.ExternalSupervisorCallback, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.reconcileCount++
	callback, found := fake.callbacks[receipt.HandoffID]
	if !found {
		return nil, nil
	}
	copy := callback
	return &copy, nil
}

func (fake *p3fInProcessFakeSupervisor) Cancel(_ context.Context, cancellation store.ExternalSupervisorCancellation) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if receipt, found := fake.receipts[cancellation.HandoffID]; !found || receipt.ReceiptIdentityHash != cancellation.ReceiptIdentityHash {
		return errors.New("fake supervisor cancellation missing receipt")
	}
	fake.cancelCount++
	return nil
}

func (fake *p3fInProcessFakeSupervisor) VerifyExternalSupervisorReceipt(_ context.Context, receipt store.ExternalSupervisorAcceptanceReceipt, root store.ExternalSupervisorTrustRoot) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	known, found := fake.receipts[receipt.HandoffID]
	if !found || known != receipt || fake.root != root {
		return errors.New("fake supervisor receipt authentication failed")
	}
	return nil
}

func (fake *p3fInProcessFakeSupervisor) VerifyExternalSupervisorCallback(_ context.Context, callback store.ExternalSupervisorCallback, root store.ExternalSupervisorTrustRoot) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	known, found := fake.callbacks[callback.HandoffID]
	if !found || known != callback || fake.root != root {
		return errors.New("fake supervisor callback authentication failed")
	}
	return nil
}

func (fake *p3fInProcessFakeSupervisor) publishCallback(handoffID, terminalState string) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	receipt := fake.receipts[handoffID]
	fake.callbacks[handoffID] = store.ExternalSupervisorCallback{
		SchemaVersion:        store.ExternalSupervisorCallbackSchemaVersion,
		HandoffID:            handoffID,
		EnvelopeHash:         receipt.EnvelopeHash,
		ReceiptIdentityHash:  receipt.ReceiptIdentityHash,
		CallbackIdentityHash: p3fExternalSupervisorHash("callback:" + handoffID),
		AttemptNumber:        receipt.AttemptNumber,
		RootID:               fake.root.RootID,
		TrustBundleHash:      fake.root.TrustBundleHash,
		SignatureHash:        p3fExternalSupervisorHash("callback-signature:" + handoffID),
		Result: store.ExternalSupervisorResult{
			SchemaVersion:        store.ExternalSupervisorResultSchemaVersion,
			TerminalState:        terminalState,
			EnvelopeHash:         receipt.EnvelopeHash,
			ReceiptIdentityHash:  receipt.ReceiptIdentityHash,
			EvidenceIdentityHash: p3fExternalSupervisorHash("evidence:" + handoffID),
		},
	}
}

func (fake *p3fInProcessFakeSupervisor) callbackFor(handoffID string) (store.ExternalSupervisorCallback, bool) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	callback, found := fake.callbacks[handoffID]
	return callback, found
}

func (fake *p3fInProcessFakeSupervisor) replaceCallback(callback store.ExternalSupervisorCallback) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.callbacks[callback.HandoffID] = callback
}

func (fake *p3fInProcessFakeSupervisor) setCurrentRoot(root store.ExternalSupervisorTrustRoot) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.root = root
}

func (fake *p3fInProcessFakeSupervisor) currentRoot() store.ExternalSupervisorTrustRoot {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.root
}

func (fake *p3fInProcessFakeSupervisor) deliveries() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.deliveryCount
}

func (fake *p3fInProcessFakeSupervisor) deliveryAttempts() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.deliveryAttemptCount
}

func (fake *p3fInProcessFakeSupervisor) observeDeliveryAttempts() <-chan int {
	attempts := make(chan int, 2)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.deliveryAttemptObserver = func(attempt int) { attempts <- attempt }
	return attempts
}

func (fake *p3fInProcessFakeSupervisor) reconciliations() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.reconcileCount
}

func (fake *p3fInProcessFakeSupervisor) cancellations() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.cancelCount
}
