package main

import (
	"context"
	"os/exec"
	"testing"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestTrustedSupervisorTransportBinaryBuildsAsSeparateBoundary(t *testing.T) {
	command := exec.Command("go", "build", "-o", t.TempDir()+"/ananke-trusted-supervisor-transport", ".")
	command.Env = nil
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build transport binary: %v\n%s", err, output)
	}
}

func TestTrustedSupervisorCommandDispatchesSubmitRecoverAndCancelExactly(t *testing.T) {
	runtime := &recordingCommandRuntime{}
	envelope := store.ExternalSupervisorEnvelope{HandoffID: "remote_handoff_command_001"}
	fence := store.LaunchFence{ClaimID: "claim_command_001"}
	cancellation := store.ExternalSupervisorCancellation{HandoffID: envelope.HandoffID}
	for _, request := range []invocation{
		{Operation: "submit", Envelope: &envelope, Fence: &fence},
		{Operation: "recover", HandoffID: envelope.HandoffID},
		{Operation: "cancel", Cancellation: &cancellation, Fence: &fence},
	} {
		if _, err := executeInvocation(context.Background(), runtime, request); err != nil {
			t.Fatalf("execute %s: %v", request.Operation, err)
		}
	}
	if runtime.submitEnvelope != envelope || runtime.submitFence != fence || runtime.recoverHandoffID != envelope.HandoffID ||
		runtime.cancelCancellation != cancellation || runtime.cancelFence != fence {
		t.Fatalf("command dispatch drift: %+v", runtime)
	}
	if _, err := executeInvocation(context.Background(), runtime, invocation{Operation: "recover", HandoffID: envelope.HandoffID, Fence: &fence}); err == nil {
		t.Fatal("recover accepted a fresh CLI fence/pin payload")
	}
}

type recordingCommandRuntime struct {
	submitEnvelope      store.ExternalSupervisorEnvelope
	submitFence         store.LaunchFence
	recoverHandoffID    string
	cancelCancellation  store.ExternalSupervisorCancellation
	cancelFence          store.LaunchFence
}

func (runtime *recordingCommandRuntime) Submit(_ context.Context, envelope store.ExternalSupervisorEnvelope, fence store.LaunchFence) lifecycle.ExternalSupervisorPublicOutput {
	runtime.submitEnvelope, runtime.submitFence = envelope, fence
	return lifecycle.ExternalSupervisorPublicOutput{}
}

func (runtime *recordingCommandRuntime) Recover(_ context.Context, handoffID string) lifecycle.ExternalSupervisorPublicOutput {
	runtime.recoverHandoffID = handoffID
	return lifecycle.ExternalSupervisorPublicOutput{}
}

func (runtime *recordingCommandRuntime) Cancel(_ context.Context, cancellation store.ExternalSupervisorCancellation, fence store.LaunchFence) lifecycle.ExternalSupervisorPublicOutput {
	runtime.cancelCancellation, runtime.cancelFence = cancellation, fence
	return lifecycle.ExternalSupervisorPublicOutput{}
}
