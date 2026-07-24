package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/yingliang-zhang/ananke/internal/store"
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
)

func TestP3FExternalSupervisorFakeRuntimePersistsReceiptAndCallbackWithoutExecution(t *testing.T) {
	ctx := context.Background()
	runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)

	assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
	if fake.deliveries() != 1 {
		t.Fatalf("fake deliveries = %d, want one receipt-only delivery", fake.deliveries())
	}
	if fake.deliveryAttempts() != 1 {
		t.Fatalf("fake transport delivery attempts = %d, want one", fake.deliveryAttempts())
	}
	boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil {
		t.Fatalf("GetExternalSupervisorRecoveryBoundary after submit: %v", err)
	}
	if boundary.Receipt == nil || boundary.Callback != nil || boundary.Cancellation != nil {
		t.Fatalf("post-submit recovery boundary = %+v, want receipt only", boundary)
	}
	assertP3CNoRealRuns(t, journal)

	assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
	if fake.deliveries() != 1 {
		t.Fatalf("idempotent delivery invoked fake supervisor %d times, want one", fake.deliveries())
	}
	if fake.deliveryAttempts() != 1 {
		t.Fatalf("duplicate submission reached fake transport %d times, want one", fake.deliveryAttempts())
	}
	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	if fake.reconciliations() != 1 {
		t.Fatalf("fake reconciliations = %d, want one explicit no-outcome reconciliation", fake.reconciliations())
	}
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil {
		t.Fatalf("GetExternalSupervisorRecoveryBoundary after empty reconciliation: %v", err)
	}
	if boundary.Callback != nil {
		t.Fatalf("empty reconciliation inferred callback: %+v", boundary.Callback)
	}

	fake.publishCallback(envelope.HandoffID, "completed")
	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil {
		t.Fatalf("GetExternalSupervisorRecoveryBoundary after callback: %v", err)
	}
	if boundary.Callback == nil || boundary.Callback.Result.TerminalState != "completed" || boundary.Callback.EnvelopeHash != envelope.EnvelopeHash || boundary.Callback.ReceiptIdentityHash != boundary.Receipt.ReceiptIdentityHash {
		t.Fatalf("durable typed callback = %+v, want current-root envelope/receipt-bound result", boundary.Callback)
	}
	assertP3CNoRealRuns(t, journal)
}

func TestP3FExternalSupervisorConcurrentSubmitAndRecoverPersistReceiptBeforeDuplicateFakeDelivery(t *testing.T) {
	ctx := context.Background()
	submitJournal, recoverJournal, envelope, fence := newP3FExternalSupervisorConcurrentJournals(t)
	fake := newP3FInProcessFakeSupervisor()
	gate := newP3FReceiptPersistenceGateAuthenticator(fake)
	submitRuntime, err := newExternalSupervisorHandoffRuntime(submitJournal, fake, gate, fake.currentRoot)
	if err != nil {
		t.Fatalf("construct submit runtime: %v", err)
	}
	recoverRuntime, err := newExternalSupervisorHandoffRuntime(recoverJournal, fake, gate, fake.currentRoot)
	if err != nil {
		t.Fatalf("construct recovery runtime: %v", err)
	}
	attempts := fake.observeDeliveryAttempts()
	outputs := make(chan externalSupervisorPublicOutput, 2)
	go func() { outputs <- submitRuntime.submit(ctx, envelope, fence) }()
	select {
	case <-gate.receiptVerificationStarted:
	case <-time.After(time.Second):
		gate.releaseReceiptPersistence()
		t.Fatal("submit did not reach the receipt-persistence boundary")
	}
	if attempt := <-attempts; attempt != 1 {
		gate.releaseReceiptPersistence()
		t.Fatalf("first fake delivery attempt = %d, want one", attempt)
	}

	recoveryStarted := make(chan struct{})
	go func() {
		close(recoveryStarted)
		outputs <- recoverRuntime.recover(ctx, envelope.HandoffID)
	}()
	<-recoveryStarted

	duplicateDelivery := false
	select {
	case attempt := <-attempts:
		duplicateDelivery = true
		if attempt != 2 {
			gate.releaseReceiptPersistence()
			t.Fatalf("duplicate fake delivery attempt = %d, want two", attempt)
		}
	case <-time.After(500 * time.Millisecond):
	}
	gate.releaseReceiptPersistence()
	for range 2 {
		assertP3FExternalSupervisorFailClosed(t, <-outputs)
	}
	if duplicateDelivery || fake.deliveryAttempts() != 1 || fake.deliveries() != 1 {
		t.Fatalf("receipt-persistence boundary allowed duplicate fake delivery: attempts=%d deliveries=%d", fake.deliveryAttempts(), fake.deliveries())
	}
	boundary, err := submitJournal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil {
		t.Fatalf("load concurrent receipt boundary: %v", err)
	}
	receipt, found := fake.receiptFor(envelope.HandoffID)
	if !found || boundary.Receipt == nil || *boundary.Receipt != receipt || boundary.Receipt.ReceiptIdentityHash != p3fExternalSupervisorHash("receipt:"+envelope.HandoffID) {
		t.Fatalf("concurrent durable receipt = %+v fake=%+v, want exact fake receipt identity", boundary.Receipt, receipt)
	}
	if boundary.Callback != nil || boundary.Cancellation != nil || fake.reconciliations() != 0 {
		t.Fatalf("concurrent receipt boundary inferred an outcome: %+v reconciliations=%d", boundary, fake.reconciliations())
	}
	assertP3CNoRealRuns(t, submitJournal)
}

func TestP3FExternalSupervisorFakeTransportRejectsUnsealedEnvelope(t *testing.T) {
	ctx := context.Background()
	_, fake, _, envelope, _ := newP3FExternalSupervisorFixture(t)

	unsealed := envelope
	unsealed.EnvelopeHash = ""
	if _, err := fake.Deliver(ctx, unsealed); err == nil {
		t.Fatal("fake transport accepted an unsealed envelope")
	}
	if fake.deliveries() != 0 {
		t.Fatalf("unsealed envelope reached fake supervisor %d times", fake.deliveries())
	}
}

func TestP3FExternalSupervisorFakeTransportRejectsCallbackBeforeReceiptAndNoResponseInference(t *testing.T) {
	ctx := context.Background()
	runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
	fake.withholdDeliveryResponse()

	assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
	boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil {
		t.Fatalf("load withheld-delivery boundary: %v", err)
	}
	if boundary.Receipt != nil || boundary.Callback != nil || boundary.Cancellation != nil {
		t.Fatalf("withheld delivery boundary = %+v, want no inferred durable authority", boundary)
	}

	fake.publishCallback(envelope.HandoffID, "completed")
	callback, found := fake.callbackFor(envelope.HandoffID)
	if !found {
		t.Fatal("fake transport did not retain an authenticated callback")
	}
	if _, err := journal.AcceptExternalSupervisorCallback(ctx, callback, fake.currentRoot(), fake); !errors.Is(err, store.ErrExternalSupervisorReceiptRequired) {
		t.Fatalf("callback before durable receipt error = %v, want %v", err, store.ErrExternalSupervisorReceiptRequired)
	}

	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil {
		t.Fatalf("load no-response recovery boundary: %v", err)
	}
	if boundary.Receipt != nil || boundary.Callback != nil {
		t.Fatalf("no-response recovery inferred durable result: %+v", boundary)
	}

	fake.releaseDeliveryResponse()
	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Receipt == nil || boundary.Callback != nil {
		t.Fatalf("recovered receipt boundary = %+v err=%v, want receipt without inferred callback", boundary, err)
	}
	assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
	boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
	if err != nil || boundary.Callback == nil || boundary.Callback.Result.TerminalState != "completed" {
		t.Fatalf("authenticated post-receipt callback = %+v err=%v", boundary.Callback, err)
	}
	assertP3CNoRealRuns(t, journal)
}

func TestP3FExternalSupervisorFakeTransportRejectsAuthenticatedReceiptAndCallbackDrift(t *testing.T) {
	ctx := context.Background()
	t.Run("receipt identity", func(t *testing.T) {
		runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
		assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
		receipt, found := fake.receiptFor(envelope.HandoffID)
		if !found {
			t.Fatal("fake transport did not retain an authenticated receipt")
		}
		drifted := receipt
		drifted.ReceiptIdentityHash = p3fExternalSupervisorHash("receipt-drift")
		fake.replaceReceipt(drifted)
		if _, err := journal.AcceptExternalSupervisorReceipt(ctx, drifted, fake.currentRoot(), fake); !errors.Is(err, store.ErrExternalSupervisorConflict) {
			t.Fatalf("authenticated receipt identity drift error = %v, want %v", err, store.ErrExternalSupervisorConflict)
		}
		assertP3CNoRealRuns(t, journal)
	})

	callbackCases := []struct {
		name   string
		mutate func(*store.ExternalSupervisorCallback, *store.ExternalSupervisorTrustRoot)
	}{
		{
			name: "trust root",
			mutate: func(callback *store.ExternalSupervisorCallback, root *store.ExternalSupervisorTrustRoot) {
				*root = store.ExternalSupervisorTrustRoot{RootID: "remote_supervisor_root_v2", TrustBundleHash: p3fExternalSupervisorHash("trust-bundle-v2")}
				callback.RootID = root.RootID
				callback.TrustBundleHash = root.TrustBundleHash
			},
		},
		{
			name: "envelope",
			mutate: func(callback *store.ExternalSupervisorCallback, _ *store.ExternalSupervisorTrustRoot) {
				callback.EnvelopeHash = p3fExternalSupervisorHash("envelope-drift")
				callback.Result.EnvelopeHash = callback.EnvelopeHash
			},
		},
		{
			name: "receipt",
			mutate: func(callback *store.ExternalSupervisorCallback, _ *store.ExternalSupervisorTrustRoot) {
				callback.ReceiptIdentityHash = p3fExternalSupervisorHash("callback-receipt-drift")
				callback.Result.ReceiptIdentityHash = callback.ReceiptIdentityHash
			},
		},
		{
			name: "attempt",
			mutate: func(callback *store.ExternalSupervisorCallback, _ *store.ExternalSupervisorTrustRoot) {
				callback.AttemptNumber++
			},
		},
	}
	for _, testCase := range callbackCases {
		t.Run(testCase.name, func(t *testing.T) {
			runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
			assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
			fake.publishCallback(envelope.HandoffID, "completed")
			callback, found := fake.callbackFor(envelope.HandoffID)
			if !found {
				t.Fatal("fake transport did not retain an authenticated callback")
			}
			root := fake.currentRoot()
			testCase.mutate(&callback, &root)
			fake.setCurrentRoot(root)
			fake.replaceCallback(callback)

			assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
			boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
			if err != nil {
				t.Fatalf("load drift boundary: %v", err)
			}
			if boundary.Callback != nil {
				t.Fatalf("authenticated %s drift became durable callback: %+v", testCase.name, boundary.Callback)
			}
			assertP3CNoRealRuns(t, journal)
		})
	}
}

func TestP3FExternalSupervisorProductionCoreIsolatesInterfaceAndAuthenticator(t *testing.T) {
	listed := p3fListLifecycleBuildFiles(t)
	production := make(map[string]*ast.File, len(listed.GoFiles))
	transportCount := 0
	runtimeCount := 0
	for _, source := range listed.GoFiles {
		parsed := p3fParseLifecycleSource(t, source)
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
	p3fAssertNoConcreteExternalSupervisorImplementations(t, production)
	p3fAssertFakeSupervisorIsTestOnly(t, listed)
}

type p3fLifecycleBuildFiles struct {
	GoFiles     []string
	TestGoFiles []string
}

type p3fMethodSignature struct {
	params  []string
	results []string
}

func p3fListLifecycleBuildFiles(t *testing.T) p3fLifecycleBuildFiles {
	t.Helper()
	command := exec.Command("go", "list", "-json", ".")
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list lifecycle package build files: %v", err)
	}
	var listed p3fLifecycleBuildFiles
	if err := json.Unmarshal(output, &listed); err != nil {
		t.Fatalf("decode lifecycle package build files: %v", err)
	}
	return listed
}

func p3fParseLifecycleSource(t *testing.T, source string) *ast.File {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
	if err != nil {
		t.Fatalf("parse lifecycle source %q: %v", source, err)
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
	for _, token := range p3fIdentifierTokens(name) {
		switch token {
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
		"Deliver":   {params: []string{"context.Context", "store.ExternalSupervisorEnvelope"}, results: []string{"store.ExternalSupervisorAcceptanceReceipt", "error"}},
		"Reconcile": {params: []string{"context.Context", "store.ExternalSupervisorAcceptanceReceipt"}, results: []string{"*store.ExternalSupervisorCallback", "error"}},
		"Cancel":    {params: []string{"context.Context", "store.ExternalSupervisorCancellation"}, results: []string{"error"}},
	}
	if len(contract.Methods.List) != len(want) {
		t.Fatalf("external supervisor transport methods = %d, want %d", len(contract.Methods.List), len(want))
	}
	for _, method := range contract.Methods.List {
		if len(method.Names) != 1 {
			t.Fatal("external supervisor transport has an unnamed or embedded method")
		}
		name := method.Names[0].Name
		signature, found := want[name]
		function, ok := method.Type.(*ast.FuncType)
		if !found || !ok || !p3fFuncTypeMatches(function, signature) {
			t.Fatalf("external supervisor transport method %q has authority-bearing or noncanonical signature", name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("external supervisor transport is missing methods %v", want)
	}
}

func p3fAssertExactExternalSupervisorRuntime(t *testing.T, typeSpec *ast.TypeSpec) {
	t.Helper()
	structure, ok := typeSpec.Type.(*ast.StructType)
	if !ok || structure.Fields == nil {
		t.Fatal("external supervisor runtime must be a struct")
	}
	want := map[string]string{
		"journal":       "*store.Store",
		"transport":     "externalSupervisorHandoffTransport",
		"authenticator": "store.ExternalSupervisorAuthenticator",
		"currentRoot":   "func() store.ExternalSupervisorTrustRoot",
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
		"Deliver":   {params: []string{"context.Context", "store.ExternalSupervisorEnvelope"}, results: []string{"store.ExternalSupervisorAcceptanceReceipt", "error"}},
		"Reconcile": {params: []string{"context.Context", "store.ExternalSupervisorAcceptanceReceipt"}, results: []string{"*store.ExternalSupervisorCallback", "error"}},
		"Cancel":    {params: []string{"context.Context", "store.ExternalSupervisorCancellation"}, results: []string{"error"}},
	}
	authenticator := map[string]p3fMethodSignature{
		"VerifyExternalSupervisorReceipt":  {params: []string{"context.Context", "store.ExternalSupervisorAcceptanceReceipt", "store.ExternalSupervisorTrustRoot"}, results: []string{"error"}},
		"VerifyExternalSupervisorCallback": {params: []string{"context.Context", "store.ExternalSupervisorCallback", "store.ExternalSupervisorTrustRoot"}, results: []string{"error"}},
	}
	for receiver, methodSet := range methods {
		if p3fMethodSetMatches(methodSet, transport) || p3fMethodSetMatches(methodSet, authenticator) {
			t.Fatalf("production receiver %q concretely implements the external-supervisor transport or authenticator", receiver)
		}
	}
}

func p3fAssertFakeSupervisorIsTestOnly(t *testing.T, listed p3fLifecycleBuildFiles) {
	t.Helper()
	const fakeSource = "external_supervisor_handoff_fake_test.go"
	if !p3fListedFile(listed.TestGoFiles, fakeSource) || p3fListedFile(listed.GoFiles, fakeSource) {
		t.Fatalf("fake supervisor build selection = production:%v tests:%v", listed.GoFiles, listed.TestGoFiles)
	}
	fakeFound := false
	for _, source := range listed.TestGoFiles {
		parsed := p3fParseLifecycleSource(t, source)
		for _, declaration := range parsed.Decls {
			generic, ok := declaration.(*ast.GenDecl)
			if !ok || generic.Tok != token.TYPE {
				continue
			}
			for _, specification := range generic.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if ok && typeSpec.Name.Name == "p3fInProcessFakeSupervisor" {
					if !strings.HasSuffix(source, "_test.go") {
						t.Fatalf("fake supervisor source %q is not test-only", source)
					}
					fakeFound = true
				}
			}
		}
	}
	if !fakeFound {
		t.Fatal("missing test-only in-process fake supervisor")
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
	case *ast.FuncType:
		if p3fFieldTypesMatch(typed.Params, nil) && p3fFieldTypesMatch(typed.Results, []string{"store.ExternalSupervisorTrustRoot"}) {
			return "func() store.ExternalSupervisorTrustRoot"
		}
	}
	return ""
}

func TestP3FExternalSupervisorFakeRuntimeRejectsPolicyRootFenceAndCancellationInference(t *testing.T) {
	ctx := context.Background()
	t.Run("policy drift", func(t *testing.T) {
		runtime, fake, _, envelope, fence := newP3FExternalSupervisorFixture(t)
		envelope.RouteMappingHash = p3fExternalSupervisorHash("different-route")
		sealed, err := store.SealExternalSupervisorEnvelope(envelope)
		if err != nil {
			t.Fatalf("seal policy-drift envelope: %v", err)
		}
		assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, sealed, fence))
		if fake.deliveries() != 0 {
			t.Fatalf("policy-drift envelope reached fake supervisor %d times", fake.deliveries())
		}
	})

	t.Run("current root binding", func(t *testing.T) {
		runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
		assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
		fake.publishCallback(envelope.HandoffID, "completed")
		fake.setCurrentRoot(store.ExternalSupervisorTrustRoot{RootID: "remote_supervisor_root_v2", TrustBundleHash: p3fExternalSupervisorHash("trust-bundle-v2")})
		assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
		boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
		if err != nil {
			t.Fatalf("GetExternalSupervisorRecoveryBoundary with stale callback root: %v", err)
		}
		if boundary.Callback != nil {
			t.Fatalf("stale-root callback became durable: %+v", boundary.Callback)
		}
		fake.setCurrentRoot(p3fExternalSupervisorRoot())
		assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
		boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
		if err != nil {
			t.Fatalf("GetExternalSupervisorRecoveryBoundary after current-root callback: %v", err)
		}
		if boundary.Callback == nil {
			t.Fatal("current-root callback was not durable")
		}
	})

	t.Run("cancellation and stale recovery", func(t *testing.T) {
		runtime, fake, journal, envelope, fence := newP3FExternalSupervisorFixture(t)
		assertP3FExternalSupervisorFailClosed(t, runtime.submit(ctx, envelope, fence))
		receipt, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
		if err != nil || receipt.Receipt == nil {
			t.Fatalf("load durable receipt: boundary=%+v err=%v", receipt, err)
		}
		cancellation := store.ExternalSupervisorCancellation{
			SchemaVersion:            store.ExternalSupervisorCancellationSchemaVersion,
			HandoffID:                envelope.HandoffID,
			EnvelopeHash:             envelope.EnvelopeHash,
			ReceiptIdentityHash:      receipt.Receipt.ReceiptIdentityHash,
			CancellationIdentityHash: p3fExternalSupervisorHash("cancellation-001"),
			AttemptNumber:            envelope.AttemptNumber,
		}
		assertP3FExternalSupervisorFailClosed(t, runtime.cancel(ctx, cancellation, fence))
		if fake.cancellations() != 1 {
			t.Fatalf("fake cancellations = %d, want one receipt-bound cancellation", fake.cancellations())
		}
		boundary, err := journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
		if err != nil {
			t.Fatalf("GetExternalSupervisorRecoveryBoundary after cancellation: %v", err)
		}
		if boundary.Cancellation == nil || boundary.Callback != nil {
			t.Fatalf("cancellation boundary = %+v, want cancellation identity without inferred callback", boundary)
		}
		assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
		boundary, err = journal.GetExternalSupervisorRecoveryBoundary(ctx, envelope.HandoffID)
		if err != nil {
			t.Fatalf("GetExternalSupervisorRecoveryBoundary after cancellation recovery: %v", err)
		}
		if boundary.Callback != nil {
			t.Fatalf("cancellation recovery inferred callback: %+v", boundary.Callback)
		}

		if _, err := journal.ReclaimLaunchClaim(ctx, store.LaunchClaimReclaimRequest{
			ExpectedFence: fence,
			Claim: store.LaunchClaimRequest{
				LaunchSpecHash: envelope.LaunchSpecHash,
				ClaimID:        "claim_external_supervisor_reclaimed",
				ClaimTokenHash: p3fExternalSupervisorHash("reclaimed-token"),
				OwnerID:        "external_supervisor_runtime",
				Attempt:        2,
			},
		}); err != nil {
			t.Fatalf("reclaim active private fence: %v", err)
		}
		assertP3FExternalSupervisorFailClosed(t, runtime.recover(ctx, envelope.HandoffID))
		if fake.reconciliations() != 1 {
			t.Fatalf("stale fence recovery reconciliations = %d, want only the prior explicit attempt", fake.reconciliations())
		}
	})
}

func TestP3FExternalSupervisorProductionBuildExcludesFakeSupervisor(t *testing.T) {
	command := exec.Command("go", "list", "-json", ".")
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list production lifecycle package: %v", err)
	}
	var listed struct {
		GoFiles     []string
		TestGoFiles []string
	}
	if err := json.Unmarshal(output, &listed); err != nil {
		t.Fatalf("decode production lifecycle package listing: %v", err)
	}
	const fakeSource = "external_supervisor_handoff_fake_test.go"
	if !p3fListedFile(listed.TestGoFiles, fakeSource) || p3fListedFile(listed.GoFiles, fakeSource) {
		t.Fatalf("fake supervisor build selection = production:%v tests:%v", listed.GoFiles, listed.TestGoFiles)
	}
	for _, name := range listed.GoFiles {
		if strings.Contains(name, "fake_supervisor") || strings.Contains(name, "fake_runtime") {
			t.Fatalf("fake supervisor source %q compiled into production", name)
		}
	}
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
		delegate:                   delegate,
		receiptVerificationStarted: make(chan struct{}),
		receiptPersistenceRelease:  make(chan struct{}),
	}
}

func (gate *p3fReceiptPersistenceGateAuthenticator) VerifyExternalSupervisorReceipt(ctx context.Context, receipt store.ExternalSupervisorAcceptanceReceipt, root store.ExternalSupervisorTrustRoot) error {
	gate.once.Do(func() {
		close(gate.receiptVerificationStarted)
		<-gate.receiptPersistenceRelease
	})
	return gate.delegate.VerifyExternalSupervisorReceipt(ctx, receipt, root)
}

func (gate *p3fReceiptPersistenceGateAuthenticator) VerifyExternalSupervisorCallback(ctx context.Context, callback store.ExternalSupervisorCallback, root store.ExternalSupervisorTrustRoot) error {
	return gate.delegate.VerifyExternalSupervisorCallback(ctx, callback, root)
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
		SchemaVersion:            store.ExternalSupervisorEnvelopeSchemaVersion,
		HandoffID:                "remote_handoff_p3f_001",
		IdempotencyKeyHash:       p3fExternalSupervisorHash("idempotency-p3f-001"),
		LaunchSpecHash:           admission.LaunchSpecHash,
		FenceBindingHash:         store.HashExternalSupervisorFenceBinding(fence),
		Deadline:                 "2026-07-30T12:00:00Z",
		AttemptNumber:            1,
		AttemptCap:               3,
		RouteMappingHash:         externalSupervisorRouteMappingHash,
		SourceSnapshotHash:       externalSupervisorP3dSourceSnapshotHash,
		SourceManifestHash:       externalSupervisorSourceManifestHash,
		RepositoryIdentity:       externalSupervisorRepositoryIdentity,
		SupervisorArtifactSHA256: externalSupervisorArtifactSHA256,
		BuildIdentityHash:        externalSupervisorBuildIdentityHash,
		ReleaseAttestationHash:   externalSupervisorReleaseAttestationHash,
		ReleaseApprovalHash:      externalSupervisorReleaseApprovalHash,
		EvidenceContractHash:     externalSupervisorEvidenceContractHash,
		EvidenceSchemaVersion:    "ananke.remote-supervisor-evidence.v1",
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
	runtime, err := newExternalSupervisorHandoffRuntime(journal, fake, fake, fake.currentRoot)
	if err != nil {
		t.Fatalf("construct external supervisor runtime: %v", err)
	}
	return runtime, fake, journal, envelope, fence
}

func p3fExternalSupervisorRoot() store.ExternalSupervisorTrustRoot {
	return store.ExternalSupervisorTrustRoot{RootID: "remote_supervisor_root_v1", TrustBundleHash: p3fExternalSupervisorHash("trust-bundle-v1")}
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
		t.Fatalf("marshal external supervisor output: %v", err)
	}
	if string(encoded) != `{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}` {
		t.Fatalf("external supervisor output JSON = %s, want exact closed shape", encoded)
	}
}
