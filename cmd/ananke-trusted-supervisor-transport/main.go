// Command ananke-trusted-supervisor-transport composes the existing fail-closed
// external-supervisor runtime with the production local Unix transport. It
// creates no run, repair, OMP session, child, source access, or artifact access.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"github.com/yingliang-zhang/ananke/internal/store"
	"github.com/yingliang-zhang/ananke/internal/trustedsupervisor"
)

const maxInvocationBytes = 64 * 1024

type invocation struct {
	Cancellation *store.ExternalSupervisorCancellation `json:"cancellation,omitempty"`
	Envelope     *store.ExternalSupervisorEnvelope     `json:"envelope,omitempty"`
	Fence        *store.LaunchFence                    `json:"fence,omitempty"`
	HandoffID    string                                `json:"handoff_id,omitempty"`
	Operation    string                                `json:"operation"`
}

type commandRuntime interface {
	Submit(context.Context, store.ExternalSupervisorEnvelope, store.LaunchFence) lifecycle.ExternalSupervisorPublicOutput
	Recover(context.Context, string) lifecycle.ExternalSupervisorPublicOutput
	Cancel(context.Context, store.ExternalSupervisorCancellation, store.LaunchFence) lifecycle.ExternalSupervisorPublicOutput
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "trusted-supervisor transport: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("ananke-trusted-supervisor-transport", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	storePath := flags.String("store", "", "path to the existing Ananke SQLite store")
	socketPath := flags.String("socket", "", "operator-selected trusted-supervisor Unix socket")
	peerUserID := flags.Int64("peer-uid", -1, "expected trusted-supervisor Unix user ID")
	peerProcessID := flags.Int64("peer-pid", -1, "expected trusted-supervisor process ID")
	trustBundlePath := flags.String("trust-bundle", "", "operator-selected canonical public trust bundle")
	timeout := flags.Duration("timeout", 2*time.Second, "per-exchange deadline (maximum 10s)")
	frameLimit := flags.Uint("max-frame-bytes", maxInvocationBytes, "frame limit (1024..65536)")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *storePath == "" || *socketPath == "" || *trustBundlePath == "" || *peerUserID < 0 || *peerUserID > int64(^uint32(0)) || *peerProcessID <= 0 || *peerProcessID > int64(^uint32(0)>>1) {
		return fmt.Errorf("--store, --socket, --trust-bundle, valid --peer-uid, and valid --peer-pid are required")
	}
	if *frameLimit > uint(^uint32(0)) {
		return fmt.Errorf("invalid frame limit")
	}
	bundleBytes, err := os.ReadFile(*trustBundlePath)
	if err != nil {
		return fmt.Errorf("read public trust bundle: %w", err)
	}
	bundle, err := trustedsupervisor.DecodeTrustBundle(bundleBytes)
	if err != nil {
		return fmt.Errorf("authenticate public trust bundle: %w", err)
	}
	client, err := trustedsupervisor.NewClient(trustedsupervisor.Config{
		TrustBundle:                        bundle,
		ExpectedUserID:                     uint32(*peerUserID),
		ExpectedProcessID:                  int32(*peerProcessID),
		ExpectedPredecessorReleaseIdentity: lifecycle.ExternalSupervisorPredecessorReleaseIdentity(),
		MaxFrameBytes:                      uint32(*frameLimit),
		SocketPath:                         *socketPath,
		Timeout:                            *timeout,
	})
	if err != nil {
		return err
	}
	request, err := readInvocation(input)
	if err != nil {
		return err
	}
	journal, err := store.Open(*storePath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer journal.Close()
	runtime, err := lifecycle.NewExternalSupervisorHandoffRuntime(journal, client, client)
	if err != nil {
		return err
	}
	result, err := executeInvocation(ctx, runtime, request)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode closed result: %w", err)
	}
	if _, err := output.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write closed result: %w", err)
	}
	return nil
}

func executeInvocation(ctx context.Context, runtime commandRuntime, request invocation) (lifecycle.ExternalSupervisorPublicOutput, error) {
	switch request.Operation {
	case "submit":
		if request.Envelope == nil || request.Fence == nil || request.Cancellation != nil || request.HandoffID != "" {
			return lifecycle.ExternalSupervisorPublicOutput{}, fmt.Errorf("submit requires only envelope and full fence")
		}
		return runtime.Submit(ctx, *request.Envelope, *request.Fence), nil
	case "recover":
		if request.HandoffID == "" || request.Envelope != nil || request.Fence != nil || request.Cancellation != nil {
			return lifecycle.ExternalSupervisorPublicOutput{}, fmt.Errorf("recover requires only handoff_id")
		}
		return runtime.Recover(ctx, request.HandoffID), nil
	case "cancel":
		if request.Cancellation == nil || request.Fence == nil || request.Envelope != nil || request.HandoffID != "" {
			return lifecycle.ExternalSupervisorPublicOutput{}, fmt.Errorf("cancel requires only cancellation and full fence")
		}
		return runtime.Cancel(ctx, *request.Cancellation, *request.Fence), nil
	default:
		return lifecycle.ExternalSupervisorPublicOutput{}, fmt.Errorf("operation must be submit, recover, or cancel")
	}
}

func readInvocation(input io.Reader) (invocation, error) {
	contents, err := io.ReadAll(io.LimitReader(input, maxInvocationBytes+1))
	if err != nil {
		return invocation{}, fmt.Errorf("read invocation: %w", err)
	}
	if len(contents) == 0 || len(contents) > maxInvocationBytes {
		return invocation{}, fmt.Errorf("invocation must be 1..%d bytes", maxInvocationBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var request invocation
	if err := decoder.Decode(&request); err != nil {
		return invocation{}, fmt.Errorf("decode invocation: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return invocation{}, fmt.Errorf("invocation contains trailing JSON")
	}
	return request, nil
}
