package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestP3FExternalSupervisorAuthenticatedRuntimePersistsReceiptAndCallbackWithoutExecution(t *testing.T) {
	ctx := context.Background()
	runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
	assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
	if fake.deliveries() != 1 || fake.deliveryAttempts() != 1 {
		t.Fatalf("delivery counts = %d/%d, want 1/1", fake.deliveries(), fake.deliveryAttempts())
	}
	boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Receipt == nil || boundary.Callback != nil || boundary.Cancellation != nil {
		t.Fatalf("receipt boundary = %+v, %v", boundary, err)
	}
	if boundary.Receipt.Delivery.PredecessorEnvelopeHash != envelope.EnvelopeHash || boundary.Receipt.Receipt.EnvelopeHash != envelope.EnvelopeHash ||
		boundary.Receipt.Delivery.ReleaseAttestationHash != boundary.Receipt.Authorization.ReleaseAttestation.AttestationHash ||
		boundary.Receipt.Delivery.ReleaseApprovalHash != boundary.Receipt.Authorization.ReleaseApproval.ApprovalHash ||
		boundary.Receipt.Receipt.ReleaseApprovalHash != boundary.Receipt.Authorization.ReleaseApproval.ApprovalHash ||
		boundary.Receipt.Delivery.TrustBundleHash == "" {
		t.Fatalf("receipt lost predecessor/dynamic authorization bindings: %+v", boundary.Receipt)
	}
	assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
	if fake.deliveryAttempts() != 1 {
		t.Fatalf("durable receipt replay reached transport %d times", fake.deliveryAttempts())
	}
	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	if fake.reconciliations() != 1 {
		t.Fatalf("pending reconciliation count = %d, want 1", fake.reconciliations())
	}
	fake.publishCallback(envelope.HandoffID, "completed")
	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Callback == nil || boundary.Callback.Callback.TerminalState != "completed" ||
		boundary.Callback.Callback.EnvelopeHash != envelope.EnvelopeHash || boundary.Callback.Callback.ReceiptHash != boundary.Receipt.Receipt.ReceiptHash {
		t.Fatalf("callback boundary = %+v, %v", boundary, err)
	}
	assertP3CNoRealRuns(t, journal)
}

func TestP3FExternalSupervisorAuthenticatedRuntimePersistsCancellationAndRejectsLaterCallback(t *testing.T) {
	ctx := context.Background()
	runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
	assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
	boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Receipt == nil {
		t.Fatalf("receipt boundary = %+v, %v", boundary, err)
	}
	cancellation, err := store.SealExternalSupervisorCancellation(store.ExternalSupervisorCancellation{
		SchemaVersion: store.ExternalSupervisorCancellationSchemaVersion, HandoffID: envelope.HandoffID,
		EnvelopeHash: envelope.EnvelopeHash, ReceiptIdentityHash: boundary.Receipt.Receipt.ReceiptHash, AttemptNumber: envelope.AttemptNumber,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertP3FExternalSupervisorFailClosed(t, runtime.cancel(ctx, cancellation, fence))
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Cancellation == nil || boundary.Cancellation.Cancellation != cancellation || boundary.Callback != nil || fake.cancellations() != 1 {
		t.Fatalf("cancellation boundary = %+v, %v cancellations=%d", boundary, err, fake.cancellations())
	}
	fake.publishCallback(envelope.HandoffID, "completed")
	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Callback != nil {
		t.Fatalf("callback crossed durable cancellation conflict: %+v, %v", boundary.Callback, err)
	}
	assertP3CNoRealRuns(t, journal)
}

func TestP3FExternalSupervisorRuntimeRejectsPredecessorReleaseIdentityDriftBeforeStaging(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*store.ExternalSupervisorEnvelope)
	}{
		{name: "artifact", mutate: func(value *store.ExternalSupervisorEnvelope) {
			value.SupervisorArtifactSHA256 = p3fExternalSupervisorHash("fresh-artifact-drift")
		}},
		{name: "build", mutate: func(value *store.ExternalSupervisorEnvelope) {
			value.BuildIdentityHash = p3fExternalSupervisorHash("fresh-build-drift")
		}},
		{name: "release attestation", mutate: func(value *store.ExternalSupervisorEnvelope) {
			value.ReleaseAttestationHash = p3fExternalSupervisorHash("fresh-release-attestation-drift")
		}},
		{name: "release approval", mutate: func(value *store.ExternalSupervisorEnvelope) {
			value.ReleaseApprovalHash = p3fExternalSupervisorHash("fresh-release-approval-drift")
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			runtime, _, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
			drifted := envelope
			testCase.mutate(&drifted)
			sealed, err := store.SealExternalSupervisorEnvelope(drifted)
			if err != nil {
				t.Fatal(err)
			}
			if validExternalSupervisorEnvelope(sealed) {
				t.Fatal("drifted predecessor release identity passed frozen envelope validation")
			}
			assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, sealed, fence))
			if _, err := journal.GetExternalSupervisorHandoff(ctx, envelope.HandoffID); err == nil {
				t.Fatal("predecessor release identity drift became a staged handoff")
			}
		})
	}
}

func TestP3FExternalSupervisorRuntimeInfersNothingFromMissingResponse(t *testing.T) {
	ctx := context.Background()
	runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
	fake.withholdDeliveryResponse()
	assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
	boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Receipt != nil || boundary.Callback != nil || boundary.Cancellation != nil {
		t.Fatalf("missing response inferred authority: %+v, %v", boundary, err)
	}
	fake.releaseDeliveryResponse()
	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Receipt == nil || boundary.Callback != nil {
		t.Fatalf("recovered receipt boundary = %+v, %v", boundary, err)
	}
}

func TestP3FExternalSupervisorConcurrentSubmitAndRecoverPersistOneAuthenticatedReceipt(t *testing.T) {
	ctx := context.Background()
	submitJournal, recoverJournal, envelope, fence := newP3FExternalSupervisorConcurrentJournals(t)
	fake := newP3FInProcessFakeSupervisor()
	gate := newP3FReceiptPersistenceGateAuthenticator(fake)
	submitRuntime, err := newExternalSupervisorHandoffRuntime(submitJournal, fake, gate)
	if err != nil {
		t.Fatalf("construct submit runtime: %v", err)
	}
	recoverRuntime, err := newExternalSupervisorHandoffRuntime(recoverJournal, fake, gate)
	if err != nil {
		t.Fatalf("construct recovery runtime: %v", err)
	}
	outputs := make(chan externalSupervisorPublicOutput, 2)
	go func() { outputs <- submitRuntime.submit(ctx, envelope, fence) }()
	select {
	case <-gate.receiptVerificationStarted:
	case <-time.After(time.Second):
		gate.releaseReceiptPersistence()
		t.Fatal("submit did not reach authenticated receipt persistence boundary")
	}
	recoveryStarted := make(chan struct{})
	go func() {
		close(recoveryStarted)
		outputs <- recoverRuntime.recover(ctx, envelope.HandoffID)
	}()
	<-recoveryStarted
	select {
	case output := <-outputs:
		gate.releaseReceiptPersistence()
		t.Fatalf("concurrent operation escaped the locked receipt boundary: %+v", output)
	case <-time.After(100 * time.Millisecond):
	}
	if fake.deliveryAttempts() != 1 || fake.deliveries() != 1 {
		gate.releaseReceiptPersistence()
		t.Fatalf("concurrent submit/recover deliveries = attempts:%d accepted:%d, want 1/1", fake.deliveryAttempts(), fake.deliveries())
	}
	gate.releaseReceiptPersistence()
	for range 2 {
		select {
		case output := <-outputs:
			assertP3FExternalSupervisorFailClosed(t, output)
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent submit/recover did not complete after receipt persistence")
		}
	}
	boundary, err := submitJournal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Receipt == nil || boundary.Callback != nil || boundary.Cancellation != nil || fake.reconciliations() != 0 {
		t.Fatalf("concurrent authenticated boundary = %+v, err=%v reconciliations=%d", boundary, err, fake.reconciliations())
	}
	if boundary.Receipt.Receipt.EnvelopeHash != envelope.EnvelopeHash || boundary.Receipt.Delivery.PredecessorEnvelopeHash != envelope.EnvelopeHash {
		t.Fatalf("concurrent receipt lost exact durable envelope binding: %+v", boundary.Receipt)
	}
	assertP3CNoRealRuns(t, submitJournal)
}

func TestP3FExternalSupervisorAuthenticatedCallbackDriftNeverBecomesDurable(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*store.ExternalSupervisorProtocolCallback)
	}{
		{name: "envelope", mutate: func(callback *store.ExternalSupervisorProtocolCallback) {
			callback.EnvelopeHash = p3fExternalSupervisorHash("callback-envelope-drift")
		}},
		{name: "receipt", mutate: func(callback *store.ExternalSupervisorProtocolCallback) {
			callback.ReceiptHash = p3fExternalSupervisorHash("callback-receipt-drift")
		}},
		{name: "delivery", mutate: func(callback *store.ExternalSupervisorProtocolCallback) {
			callback.DeliveryHash = p3fExternalSupervisorHash("callback-delivery-drift")
		}},
		{name: "attempt", mutate: func(callback *store.ExternalSupervisorProtocolCallback) { callback.AttemptNumber++ }},
		{name: "route", mutate: func(callback *store.ExternalSupervisorProtocolCallback) {
			callback.RouteMappingHash = p3fExternalSupervisorHash("callback-route-drift")
		}},
		{name: "trust root", mutate: func(callback *store.ExternalSupervisorProtocolCallback) {
			callback.TrustRootID = "independent_supervisor_release_root_v2"
		}},
		{name: "signer", mutate: func(callback *store.ExternalSupervisorProtocolCallback) {
			callback.SignerKeySPKISHA256 = p3fExternalSupervisorHash("callback-signer-drift")
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
			assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
			fake.publishCallback(envelope.HandoffID, "completed")
			callback, found := fake.callbackFor(envelope.HandoffID)
			if !found {
				t.Fatal("test-only fake lost published callback")
			}
			testCase.mutate(&callback.Callback)
			callback.Callback = mustP3FSealProtocolCallback(callback.Callback)
			callback.CallbackAuthentication = p3fDummyMessageAuthentication(
				"callback", callback.Callback.CallbackHash, callback.Callback.NonceHash,
				callback.Callback.CallbackChannelBindingHash, callback.Callback.IssuedAt,
			)
			fake.replaceCallback(callback)
			assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
			boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
			if err != nil || boundary.Callback != nil || boundary.Receipt == nil {
				t.Fatalf("authenticated %s drift boundary = %+v, %v", testCase.name, boundary, err)
			}
			assertP3CNoRealRuns(t, journal)
		})
	}
}

func TestP3FExternalSupervisorReclaimedFenceBlocksStaleRecoveryAndCancellation(t *testing.T) {
	ctx := context.Background()
	runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
	assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
	boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Receipt == nil {
		t.Fatalf("load pre-reclaim receipt: %+v, %v", boundary, err)
	}
	if _, err := journal.ReclaimLaunchClaim(ctx, store.LaunchClaimReclaimRequest{
		ExpectedFence: fence,
		Claim: store.LaunchClaimRequest{
			LaunchSpecHash: envelope.LaunchSpecHash, ClaimID: "claim_external_supervisor_reclaimed",
			ClaimTokenHash: p3fExternalSupervisorHash("reclaimed-token"), OwnerID: "external_supervisor_runtime", Attempt: 2,
		},
	}); err != nil {
		t.Fatalf("reclaim external-supervisor fence: %v", err)
	}
	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	cancellation, err := store.SealExternalSupervisorCancellation(store.ExternalSupervisorCancellation{
		SchemaVersion: store.ExternalSupervisorCancellationSchemaVersion, HandoffID: envelope.HandoffID,
		EnvelopeHash: envelope.EnvelopeHash, ReceiptIdentityHash: boundary.Receipt.Receipt.ReceiptHash, AttemptNumber: envelope.AttemptNumber,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertP3FExternalSupervisorFailClosed(t, runtime.cancel(ctx, cancellation, fence))
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Callback != nil || boundary.Cancellation != nil || fake.reconciliations() != 0 || fake.cancellations() != 0 {
		t.Fatalf("reclaimed stale-fence boundary = %+v, err=%v reconciliations=%d cancellations=%d", boundary, err, fake.reconciliations(), fake.cancellations())
	}
	assertP3CNoRealRuns(t, journal)
}

func TestP3FExternalSupervisorFakeRejectsUnsealedEnvelope(t *testing.T) {
	_, fake, _, envelope, _ := newP3FExternalSupervisorFixture(t)
	envelope.EnvelopeHash = ""
	if _, err := fake.Deliver(context.Background(), envelope); err == nil {
		t.Fatal("test-only fake accepted an unsealed envelope")
	}
	if fake.deliveries() != 0 || fake.deliveryAttempts() != 0 {
		t.Fatalf("unsealed envelope reached fake: deliveries=%d attempts=%d", fake.deliveries(), fake.deliveryAttempts())
	}
}

func TestP3FExternalSupervisorRoutePolicyDriftIsRejectedBeforeTransport(t *testing.T) {
	runtime, fake, _, envelope, fence := newP3FExternalSupervisorFixture(t)
	envelope.RouteMappingHash = p3fExternalSupervisorHash("different-route")
	drifted, err := store.SealExternalSupervisorEnvelope(envelope)
	if err != nil {
		t.Fatalf("seal route-policy-drift envelope: %v", err)
	}
	assertP3FExternalSupervisorFailClosed(t, runtime.submit(context.Background(), drifted, fence))
	if fake.deliveryAttempts() != 0 || fake.deliveries() != 0 {
		t.Fatalf("route-policy drift reached transport: attempts=%d deliveries=%d", fake.deliveryAttempts(), fake.deliveries())
	}
}

func TestP3FExternalSupervisorProductionCoreIsolatesInterfaceAndAuthenticator(t *testing.T) {
	listed := p3fListBuildFiles(t, ".")
	production := make(map[string]*ast.File, len(listed.GoFiles))
	transportCount := 0
	runtimeCount := 0
	for _, source := range listed.GoFiles {
		parsed := p3fParseSource(t, source)
		production[source] = parsed
		p3fAssertExternalSupervisorProductionSourceIsolated(t, source, parsed)
		for _, declaration := range parsed.Decls {
			generic, ok := declaration.(*ast.GenDecl)
			if !ok || generic.Tok != token.TYPE {
				continue
			}
			for _, specification := range generic.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				switch typeSpec.Name.Name {
				case "externalSupervisorHandoffTransport":
					transportCount++
					p3fAssertExactExternalSupervisorTransport(t, typeSpec)
				case "externalSupervisorHandoffRuntime":
					runtimeCount++
					p3fAssertExactExternalSupervisorRuntime(t, typeSpec)
				}
			}
		}
	}
	if transportCount != 1 || runtimeCount != 1 {
		t.Fatalf("production external-supervisor declarations = transport:%d runtime:%d, want exactly one each across %v", transportCount, runtimeCount, listed.GoFiles)
	}
	p3fAssertExactExternalSupervisorAuthenticator(t, p3fParseSource(t, filepath.Join("..", "store", "external_supervisor_handoff.go")))
	p3fAssertNoConcreteExternalSupervisorImplementations(t, production)
	p3fAssertSelectedSupervisorFakesAndServersAreTestOnly(t, listed)
}

type p3fBuildFiles struct {
	GoFiles     []string
	TestGoFiles []string
}

type p3fMethodSignature struct {
	params  []string
	results []string
}

func p3fListBuildFiles(t *testing.T, directory string) p3fBuildFiles {
	t.Helper()
	command := exec.Command("go", "list", "-json", ".")
	command.Dir = directory
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list package build files in %q: %v", directory, err)
	}
	var listed p3fBuildFiles
	if err := json.Unmarshal(output, &listed); err != nil {
		t.Fatalf("decode package build files in %q: %v", directory, err)
	}
	return listed
}

func p3fParseSource(t *testing.T, source string) *ast.File {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
	if err != nil {
		t.Fatalf("parse source %q: %v", source, err)
	}
	return parsed
}

func p3fAssertExternalSupervisorProductionSourceIsolated(t *testing.T, source string, file *ast.File) {
	t.Helper()
	if !p3fExternalSupervisorSource(file) {
		return
	}
	for _, imported := range file.Imports {
		path := strings.Trim(imported.Path.Value, "\"")
		if p3fForbiddenExternalSupervisorImport(path) {
			t.Fatalf("production external-supervisor source %q imports endpoint/process/network package %q", source, path)
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && p3fAuthorityBearingIdentifier(identifier.Name) {
			t.Fatalf("production external-supervisor source %q exposes authority-bearing identifier %q", source, identifier.Name)
		}
		return true
	})
}

func p3fExternalSupervisorSource(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && strings.HasPrefix(identifier.Name, "externalSupervisor") {
			found = true
		}
		return !found
	})
	return found
}

func p3fForbiddenExternalSupervisorImport(path string) bool {
	lower := strings.ToLower(path)
	if path == "net" || strings.HasPrefix(path, "net/") || path == "os" || path == "os/exec" || path == "syscall" || strings.HasPrefix(path, "golang.org/x/sys") {
		return true
	}
	return strings.Contains(lower, "http") || strings.Contains(lower, "grpc") || strings.Contains(lower, "websocket")
}

func p3fAuthorityBearingIdentifier(name string) bool {
	for _, word := range p3fIdentifierTokens(name) {
		switch word {
		case "endpoint", "credential", "credentials", "executable", "argv", "args", "argument", "arguments", "env", "environment", "path", "command", "program", "process", "socket", "url", "uri":
			return true
		}
	}
	return false
}

func p3fIdentifierTokens(name string) []string {
	tokens := make([]string, 0, 2)
	start := 0
	for index, character := range name {
		if character == '_' || character == '-' {
			if start < index {
				tokens = append(tokens, strings.ToLower(name[start:index]))
			}
			start = index + 1
			continue
		}
		if index > start && character >= 'A' && character <= 'Z' && name[index-1] >= 'a' && name[index-1] <= 'z' {
			tokens = append(tokens, strings.ToLower(name[start:index]))
			start = index
		}
	}
	if start < len(name) {
		tokens = append(tokens, strings.ToLower(name[start:]))
	}
	return tokens
}

func p3fAssertExactExternalSupervisorTransport(t *testing.T, typeSpec *ast.TypeSpec) {
	t.Helper()
	contract, ok := typeSpec.Type.(*ast.InterfaceType)
	if !ok || contract.Methods == nil {
		t.Fatal("external supervisor transport must be an interface")
	}
	want := map[string]p3fMethodSignature{
		"Deliver":   {params: []string{"context.Context", "store.ExternalSupervisorEnvelope"}, results: []string{"store.ExternalSupervisorAuthenticatedReceipt", "error"}},
		"Reconcile": {params: []string{"context.Context", "store.ExternalSupervisorEnvelope", "store.ExternalSupervisorAuthenticatedReceipt"}, results: []string{"*store.ExternalSupervisorAuthenticatedCallback", "error"}},
		"Cancel":    {params: []string{"context.Context", "store.ExternalSupervisorEnvelope", "store.ExternalSupervisorAuthenticatedReceipt", "store.ExternalSupervisorCancellation"}, results: []string{"store.ExternalSupervisorAuthenticatedCancellation", "error"}},
	}
	p3fAssertExactInterfaceMethods(t, "external supervisor transport", contract, want)
}

func p3fAssertExactExternalSupervisorRuntime(t *testing.T, typeSpec *ast.TypeSpec) {
	t.Helper()
	structure, ok := typeSpec.Type.(*ast.StructType)
	if !ok || structure.Fields == nil {
		t.Fatal("external supervisor runtime must be a struct")
	}
	want := map[string]string{
		"journal": "*store.Store", "transport": "externalSupervisorHandoffTransport", "authenticator": "store.ExternalSupervisorAuthenticator",
	}
	if len(structure.Fields.List) != len(want) {
		t.Fatalf("external supervisor runtime fields = %d, want exact fields %v", len(structure.Fields.List), want)
	}
	for _, field := range structure.Fields.List {
		if len(field.Names) != 1 {
			t.Fatal("external supervisor runtime has embedded or grouped fields")
		}
		name := field.Names[0].Name
		wantType, found := want[name]
		if !found || p3fExpressionName(field.Type) != wantType {
			t.Fatalf("external supervisor runtime field %q type = %q, want exact %q", name, p3fExpressionName(field.Type), wantType)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("external supervisor runtime is missing fields %v", want)
	}
}

func p3fAssertExactExternalSupervisorAuthenticator(t *testing.T, file *ast.File) {
	t.Helper()
	found := 0
	for _, declaration := range file.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok || generic.Tok != token.TYPE {
			continue
		}
		for _, specification := range generic.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "ExternalSupervisorAuthenticator" {
				continue
			}
			contract, ok := typeSpec.Type.(*ast.InterfaceType)
			if !ok || contract.Methods == nil {
				t.Fatal("external supervisor authenticator must be an interface")
			}
			found++
			p3fAssertExactInterfaceMethods(t, "external supervisor authenticator", contract, map[string]p3fMethodSignature{
				"VerifyExternalSupervisorEnvelope":     {params: []string{"context.Context", "ExternalSupervisorEnvelope"}, results: []string{"error"}},
				"VerifyExternalSupervisorReceipt":      {params: []string{"context.Context", "ExternalSupervisorEnvelope", "ExternalSupervisorAuthenticatedReceipt"}, results: []string{"error"}},
				"VerifyExternalSupervisorCallback":     {params: []string{"context.Context", "ExternalSupervisorEnvelope", "ExternalSupervisorAuthenticatedReceipt", "ExternalSupervisorAuthenticatedCallback"}, results: []string{"error"}},
				"VerifyExternalSupervisorCancellation": {params: []string{"context.Context", "ExternalSupervisorEnvelope", "ExternalSupervisorAuthenticatedReceipt", "ExternalSupervisorAuthenticatedCancellation"}, results: []string{"error"}},
			})
		}
	}
	if found != 1 {
		t.Fatalf("production external-supervisor authenticator declarations = %d, want exactly one", found)
	}
}

func p3fAssertExactInterfaceMethods(t *testing.T, name string, contract *ast.InterfaceType, want map[string]p3fMethodSignature) {
	t.Helper()
	if len(contract.Methods.List) != len(want) {
		t.Fatalf("%s methods = %d, want %d", name, len(contract.Methods.List), len(want))
	}
	for _, method := range contract.Methods.List {
		if len(method.Names) != 1 {
			t.Fatalf("%s has an unnamed or embedded method", name)
		}
		methodName := method.Names[0].Name
		signature, exists := want[methodName]
		function, ok := method.Type.(*ast.FuncType)
		if !exists || !ok || !p3fFuncTypeMatches(function, signature) {
			t.Fatalf("%s method %q has an authority-bearing or noncanonical signature", name, methodName)
		}
		delete(want, methodName)
	}
	if len(want) != 0 {
		t.Fatalf("%s is missing methods %v", name, want)
	}
}

func p3fAssertNoConcreteExternalSupervisorImplementations(t *testing.T, sources map[string]*ast.File) {
	t.Helper()
	methods := make(map[string]map[string][]*ast.FuncDecl)
	for _, file := range sources {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || len(function.Recv.List) != 1 {
				continue
			}
			receiver := p3fReceiverName(function.Recv.List[0].Type)
			if receiver == "" {
				continue
			}
			if methods[receiver] == nil {
				methods[receiver] = make(map[string][]*ast.FuncDecl)
			}
			methods[receiver][function.Name.Name] = append(methods[receiver][function.Name.Name], function)
		}
	}
	transport := map[string]p3fMethodSignature{
		"Deliver":   {params: []string{"context.Context", "store.ExternalSupervisorEnvelope"}, results: []string{"store.ExternalSupervisorAuthenticatedReceipt", "error"}},
		"Reconcile": {params: []string{"context.Context", "store.ExternalSupervisorEnvelope", "store.ExternalSupervisorAuthenticatedReceipt"}, results: []string{"*store.ExternalSupervisorAuthenticatedCallback", "error"}},
		"Cancel":    {params: []string{"context.Context", "store.ExternalSupervisorEnvelope", "store.ExternalSupervisorAuthenticatedReceipt", "store.ExternalSupervisorCancellation"}, results: []string{"store.ExternalSupervisorAuthenticatedCancellation", "error"}},
	}
	authenticator := map[string]p3fMethodSignature{
		"VerifyExternalSupervisorEnvelope":     {params: []string{"context.Context", "store.ExternalSupervisorEnvelope"}, results: []string{"error"}},
		"VerifyExternalSupervisorReceipt":      {params: []string{"context.Context", "store.ExternalSupervisorEnvelope", "store.ExternalSupervisorAuthenticatedReceipt"}, results: []string{"error"}},
		"VerifyExternalSupervisorCallback":     {params: []string{"context.Context", "store.ExternalSupervisorEnvelope", "store.ExternalSupervisorAuthenticatedReceipt", "store.ExternalSupervisorAuthenticatedCallback"}, results: []string{"error"}},
		"VerifyExternalSupervisorCancellation": {params: []string{"context.Context", "store.ExternalSupervisorEnvelope", "store.ExternalSupervisorAuthenticatedReceipt", "store.ExternalSupervisorAuthenticatedCancellation"}, results: []string{"error"}},
	}
	for receiver, methodSet := range methods {
		if p3fMethodSetMatches(methodSet, transport) || p3fMethodSetMatches(methodSet, authenticator) {
			t.Fatalf("production receiver %q concretely implements the external-supervisor transport or authenticator", receiver)
		}
	}
}

func p3fAssertSelectedSupervisorFakesAndServersAreTestOnly(t *testing.T, lifecycleFiles p3fBuildFiles) {
	t.Helper()
	selections := []struct {
		name   string
		listed p3fBuildFiles
		files  []string
	}{
		{name: "lifecycle", listed: lifecycleFiles, files: []string{"external_supervisor_handoff_fake_test.go", "external_supervisor_unix_transport_test.go"}},
		{name: "trusted supervisor transport", listed: p3fListBuildFiles(t, filepath.Join("..", "trustedsupervisor")), files: []string{"client_test.go", "process_e2e_test.go"}},
	}
	for _, selection := range selections {
		for _, source := range selection.files {
			if p3fListedFile(selection.listed.GoFiles, source) || !p3fListedFile(selection.listed.TestGoFiles, source) {
				t.Fatalf("%s test-only source %q build selection = production:%v tests:%v", selection.name, source, selection.listed.GoFiles, selection.listed.TestGoFiles)
			}
		}
	}
}

func p3fMethodSetMatches(methods map[string][]*ast.FuncDecl, want map[string]p3fMethodSignature) bool {
	for name, signature := range want {
		matched := false
		for _, function := range methods[name] {
			if p3fFuncTypeMatches(function.Type, signature) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func p3fReceiverName(expression ast.Expr) string {
	if pointer, ok := expression.(*ast.StarExpr); ok {
		expression = pointer.X
	}
	name, _ := expression.(*ast.Ident)
	if name == nil {
		return ""
	}
	return name.Name
}

func p3fFuncTypeMatches(function *ast.FuncType, want p3fMethodSignature) bool {
	return p3fFieldTypesMatch(function.Params, want.params) && p3fFieldTypesMatch(function.Results, want.results)
}

func p3fFieldTypesMatch(fields *ast.FieldList, want []string) bool {
	got := make([]string, 0, len(want))
	if fields != nil {
		for _, field := range fields.List {
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for range count {
				got = append(got, p3fExpressionName(field.Type))
			}
		}
	}
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func p3fExpressionName(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return p3fExpressionName(typed.X) + "." + typed.Sel.Name
	case *ast.StarExpr:
		return "*" + p3fExpressionName(typed.X)
	}
	return ""
}

type p3fReceiptPersistenceGateAuthenticator struct {
	delegate                   store.ExternalSupervisorAuthenticator
	receiptVerificationStarted chan struct{}
	receiptPersistenceRelease  chan struct{}
	once                       sync.Once
	releaseOnce                sync.Once
}

func newP3FReceiptPersistenceGateAuthenticator(delegate store.ExternalSupervisorAuthenticator) *p3fReceiptPersistenceGateAuthenticator {
	return &p3fReceiptPersistenceGateAuthenticator{
		delegate: delegate, receiptVerificationStarted: make(chan struct{}), receiptPersistenceRelease: make(chan struct{}),
	}
}

func (gate *p3fReceiptPersistenceGateAuthenticator) VerifyExternalSupervisorEnvelope(ctx context.Context, envelope store.ExternalSupervisorEnvelope) error {
	return gate.delegate.VerifyExternalSupervisorEnvelope(ctx, envelope)
}

func (gate *p3fReceiptPersistenceGateAuthenticator) VerifyExternalSupervisorReceipt(ctx context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt) error {
	gate.once.Do(func() {
		close(gate.receiptVerificationStarted)
		<-gate.receiptPersistenceRelease
	})
	return gate.delegate.VerifyExternalSupervisorReceipt(ctx, envelope, receipt)
}

func (gate *p3fReceiptPersistenceGateAuthenticator) VerifyExternalSupervisorCallback(ctx context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, callback store.ExternalSupervisorAuthenticatedCallback) error {
	return gate.delegate.VerifyExternalSupervisorCallback(ctx, envelope, receipt, callback)
}

func (gate *p3fReceiptPersistenceGateAuthenticator) VerifyExternalSupervisorCancellation(ctx context.Context, envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, cancellation store.ExternalSupervisorAuthenticatedCancellation) error {
	return gate.delegate.VerifyExternalSupervisorCancellation(ctx, envelope, receipt, cancellation)
}

func (gate *p3fReceiptPersistenceGateAuthenticator) releaseReceiptPersistence() {
	gate.releaseOnce.Do(func() { close(gate.receiptPersistenceRelease) })
}

func newP3FExternalSupervisorConcurrentJournals(t *testing.T) (*store.Store, *store.Store, store.ExternalSupervisorEnvelope, store.LaunchFence) {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "p3f-external-supervisor.sqlite")
	submitJournal, err := store.Open(databasePath)
	if err != nil {
		t.Fatalf("open submit journal: %v", err)
	}
	t.Cleanup(func() { _ = submitJournal.Close() })
	seedP3aApprovedRevision(t, submitJournal)
	envelope, fence := stageP3FExternalSupervisorFixture(t, newFencedLaunchOrchestrator(submitJournal))
	recoverJournal, err := store.Open(databasePath)
	if err != nil {
		t.Fatalf("open recovery journal: %v", err)
	}
	t.Cleanup(func() { _ = recoverJournal.Close() })
	return submitJournal, recoverJournal, envelope, fence
}

func stageP3FExternalSupervisorFixture(t *testing.T, orchestration *fencedLaunchOrchestrator) (store.ExternalSupervisorEnvelope, store.LaunchFence) {
	t.Helper()
	ctx := context.Background()
	admission := p3aAdmissionRequest()
	action, err := orchestration.admit(ctx, admission, p3cClaimRequest(admission.LaunchSpecHash))
	if err != nil {
		t.Fatalf("admit P3f external supervisor fence: %v", err)
	}
	action, err = orchestration.recordTrustedMaterializationReady(ctx, p3aMaterializationRequest(action.Boundary.Claim.Fence))
	if err != nil {
		t.Fatalf("record trusted materialization: %v", err)
	}
	action, err = orchestration.admitRunIntent(ctx, store.LaunchRunIntentRequest{
		Fence: action.Boundary.Claim.Fence, MaterializationID: "materialization_p3a_001", RunID: "run_p3f_external_supervisor_001", Attempt: 1,
	})
	if err != nil {
		t.Fatalf("admit external supervisor run intent: %v", err)
	}
	fence := action.Boundary.Claim.Fence
	envelope := store.ExternalSupervisorEnvelope{
		SchemaVersion: store.ExternalSupervisorEnvelopeSchemaVersion, HandoffID: "remote_handoff_p3f_001",
		IdempotencyKeyHash: p3fExternalSupervisorHash("idempotency-p3f-001"), LaunchSpecHash: admission.LaunchSpecHash,
		FenceBindingHash: store.HashExternalSupervisorFenceBinding(fence), Deadline: externalSupervisorDeadline,
		AttemptNumber: 1, AttemptCap: externalSupervisorAttemptCap, RouteMappingHash: externalSupervisorRouteMappingHash,
		SourceSnapshotHash: externalSupervisorP3dSourceSnapshotHash, SourceManifestHash: externalSupervisorSourceManifestHash,
		RepositoryIdentity: externalSupervisorRepositoryIdentity, SupervisorArtifactSHA256: externalSupervisorArtifactSHA256,
		BuildIdentityHash: externalSupervisorBuildIdentityHash, ReleaseAttestationHash: externalSupervisorReleaseAttestationHash,
		ReleaseApprovalHash: externalSupervisorReleaseApprovalHash, EvidenceContractHash: externalSupervisorEvidenceContractHash,
		EvidenceSchemaVersion: externalSupervisorEvidenceSchemaVersion,
	}
	sealed, err := store.SealExternalSupervisorEnvelope(envelope)
	if err != nil {
		t.Fatalf("seal P3f external supervisor envelope: %v", err)
	}
	return sealed, fence
}

func newP3FExternalSupervisorFixture(t *testing.T) (*externalSupervisorHandoffRuntime, *p3fInProcessFakeSupervisor, *store.Store, store.ExternalSupervisorEnvelope, store.LaunchFence) {
	t.Helper()
	orchestration, journal := newP3CTestOrchestration(t)
	envelope, fence := stageP3FExternalSupervisorFixture(t, orchestration)
	fake := newP3FInProcessFakeSupervisor()
	runtime, err := newExternalSupervisorHandoffRuntime(journal, fake, fake)
	if err != nil {
		t.Fatalf("construct external supervisor runtime: %v", err)
	}
	return runtime, fake, journal, envelope, fence
}

func p3fExternalSupervisorHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func assertP3FExternalSupervisorFailClosed(t *testing.T, output externalSupervisorPublicOutput) {
	t.Helper()
	if output.SchemaVersion != "ananke.omp-production-output.v1" || output.State != "waiting_for_human" || output.VerificationState != "not_run" || output.Result != nil || len(output.Events) != 0 {
		t.Fatalf("external supervisor output = %+v, want normalized waiting_for_human", output)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}` {
		t.Fatalf("external supervisor output JSON = %s", encoded)
	}
}
