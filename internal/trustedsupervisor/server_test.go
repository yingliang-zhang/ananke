package trustedsupervisor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	_ "modernc.org/sqlite"
)

type serverTestMaterial struct {
	bundlePath           string
	fixture              signedAuthorizationFixture
	journalPath          string
	repositoryPolicyPath string
	executionPolicyPath  string
	keyBundlePath        string
	privateText          string
	directory            string
	socketPath           string
}

func TestProductionServerSigningMaterialRejectsWrongKeyBundleMismatchSymlinkAndMode(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	loaded, err := loadServerSigningMaterial(material.bundlePath, material.keyBundlePath, uint32(os.Getuid()), now)
	if err != nil {
		t.Fatalf("load valid signing material: %v", err)
	}
	privateKeyAlias := loaded.privateKey
	loaded.Close()
	assertZeroedPrivateBuffer(t, privateKeyAlias, "private key close alias")
	if loaded.privateKey != nil {
		t.Fatal("closed signing material retained its private key slice")
	}

	t.Run("wrong private key", func(t *testing.T) {
		wrong := deterministicServerTestKey("wrong-peer")
		path := writeServerPrivateBundle(t, material.directory, "wrong-key.json", material.fixture.bundle,
			material.fixture.bundle.SupervisorPeer.Certificate.IssuerRootID,
			material.fixture.bundle.SupervisorPeer.Certificate.SubjectPublicKey,
			material.fixture.bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256,
			"ed25519-private:"+hex.EncodeToString(wrong))
		if _, err := loadServerSigningMaterial(material.bundlePath, path, uint32(os.Getuid()), now); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("wrong-key error = %v, want %v", err, ErrAuthentication)
		}
	})

	t.Run("bundle hash mismatch", func(t *testing.T) {
		path := writeServerPrivateBundleWithTrustHash(t, material.directory, "wrong-bundle.json", material.fixture.bundle,
			testHash("other-trust-bundle"), material.privateText)
		if _, err := loadServerSigningMaterial(material.bundlePath, path, uint32(os.Getuid()), now); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("bundle-mismatch error = %v, want %v", err, ErrAuthentication)
		}
	})

	t.Run("root mismatch", func(t *testing.T) {
		path := writeServerPrivateBundle(t, material.directory, "wrong-root.json", material.fixture.bundle,
			material.fixture.bundle.ReleaseRoots.Successor.RootID,
			material.fixture.bundle.SupervisorPeer.Certificate.SubjectPublicKey,
			material.fixture.bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256,
			material.privateText)
		if _, err := loadServerSigningMaterial(material.bundlePath, path, uint32(os.Getuid()), now); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("root-mismatch error = %v, want %v", err, ErrAuthentication)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		path := filepath.Join(material.directory, "key-link.json")
		if err := os.Symlink(material.keyBundlePath, path); err != nil {
			t.Fatal(err)
		}
		if _, err := loadServerSigningMaterial(material.bundlePath, path, uint32(os.Getuid()), now); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("symlink error = %v, want %v", err, ErrAuthentication)
		}
	})

	t.Run("group readable", func(t *testing.T) {
		path := writeServerPrivateBundleWithTrustHash(t, material.directory, "wide-key.json", material.fixture.bundle,
			material.fixture.bundle.TrustBundleHash, material.privateText)
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatal(err)
		}
		if _, err := loadServerSigningMaterial(material.bundlePath, path, uint32(os.Getuid()), now); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("wide-mode error = %v, want %v", err, ErrAuthentication)
		}
	})
}

func TestProductionServerSigningMaterialZeroizesPrivateBufferAliasesOnCloseAndError(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)

	t.Run("successful load and close", func(t *testing.T) {
		var rawAlias, encodedAlias, decodedAlias []byte
		loaded, err := loadServerSigningMaterialWithHooks(material.bundlePath, material.keyBundlePath, uint32(os.Getuid()), now, privateSigningMaterialHooks{
			afterPrivateFileRead: func(value []byte) { rawAlias = value },
			afterPrivateField:    func(value []byte) { encodedAlias = value },
			afterPrivateKey:      func(value ed25519.PrivateKey) { decodedAlias = value },
		})
		if err != nil {
			t.Fatalf("load valid signing material: %v", err)
		}
		if len(rawAlias) == 0 || len(encodedAlias) != len(material.privateText) || len(decodedAlias) != ed25519.PrivateKeySize {
			t.Fatalf("captured aliases have lengths raw=%d encoded=%d decoded=%d", len(rawAlias), len(encodedAlias), len(decodedAlias))
		}
		assertZeroedPrivateBuffer(t, rawAlias, "raw private bundle after load")
		assertZeroedPrivateBuffer(t, encodedAlias, "encoded private field after load")
		if !bytes.Equal(decodedAlias, loaded.privateKey) {
			t.Fatal("decoded private-key alias does not reference the loaded key")
		}
		if bytes.Count(decodedAlias, []byte{0}) == len(decodedAlias) {
			t.Fatal("decoded private-key alias was vacuously zero before Close")
		}
		loaded.Close()
		assertZeroedPrivateBuffer(t, decodedAlias, "decoded private key after Close")
	})

	for _, testCase := range []struct {
		name string
		path func(*testing.T) string
	}{
		{
			name: "identity mismatch after decode",
			path: func(t *testing.T) string {
				wrong := deterministicServerTestKey("zeroization-wrong-peer")
				return writeServerPrivateBundle(t, material.directory, "zeroization-wrong-key.json", material.fixture.bundle,
					material.fixture.bundle.SupervisorPeer.Certificate.IssuerRootID,
					material.fixture.bundle.SupervisorPeer.Certificate.SubjectPublicKey,
					material.fixture.bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256,
					"ed25519-private:"+hex.EncodeToString(wrong))
			},
		},
		{
			name: "unknown field after decode",
			path: func(t *testing.T) string {
				value := map[string]any{
					"schema_version": privateSigningKeyBundleSchemaVersion, "trust_bundle_hash": material.fixture.bundle.TrustBundleHash,
					"keys": []any{map[string]any{
						"role": "independent_supervisor_protocol_adapter", "root_id": material.fixture.bundle.SupervisorPeer.Certificate.IssuerRootID,
						"public_key":  material.fixture.bundle.SupervisorPeer.Certificate.SubjectPublicKey,
						"spki_sha256": material.fixture.bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256,
						"private_key": material.privateText, "unexpected": "rejected",
					}},
				}
				return writeServerPrivateBundleValue(t, material.directory, "zeroization-unknown-field.json", value)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var rawAlias, encodedAlias, decodedAlias []byte
			_, err := loadServerSigningMaterialWithHooks(material.bundlePath, testCase.path(t), uint32(os.Getuid()), now, privateSigningMaterialHooks{
				afterPrivateFileRead: func(value []byte) { rawAlias = value },
				afterPrivateField:    func(value []byte) { encodedAlias = value },
				afterPrivateKey:      func(value ed25519.PrivateKey) { decodedAlias = value },
			})
			if !errors.Is(err, ErrAuthentication) {
				t.Fatalf("load error = %v, want %v", err, ErrAuthentication)
			}
			if len(rawAlias) == 0 || len(encodedAlias) == 0 || len(decodedAlias) != ed25519.PrivateKeySize {
				t.Fatalf("error aliases have lengths raw=%d encoded=%d decoded=%d", len(rawAlias), len(encodedAlias), len(decodedAlias))
			}
			assertZeroedPrivateBuffer(t, rawAlias, "raw private bundle after error")
			assertZeroedPrivateBuffer(t, encodedAlias, "encoded private field after error")
			assertZeroedPrivateBuffer(t, decodedAlias, "decoded private key after error")
			if strings.Contains(err.Error(), material.privateText) || strings.Contains(err.Error(), strings.TrimPrefix(material.privateText, privateSigningKeyPrefix)) {
				t.Fatalf("private signing value leaked in error: %v", err)
			}
		})
	}
}

func assertZeroedPrivateBuffer(t *testing.T, alias []byte, name string) {
	t.Helper()
	for index, value := range alias {
		if value != 0 {
			t.Fatalf("%s byte %d = %d, want zero", name, index, value)
		}
	}
}

func TestServerJournalExactReplaySurvivesRestartWithoutResigningAndConflictsFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-journal.sqlite")
	journal, err := openServerJournal(path)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	record := serverJournalRequest{
		RequestHash:         testHash("request-one"),
		Operation:           operationDeliver,
		OperationKey:        "deliver:" + testHash("envelope-one"),
		RequestNonceHash:    testHash("request-nonce-one"),
		ResponseNonceHash:   testHash("response-nonce-one"),
		AdditionalNonceHash: testHash("delivery-nonce-one"),
		RequestBytes:        []byte(`{"request":"one"}`),
	}
	signCount := 0
	response, replay, err := journal.transact(context.Background(), record, func() ([]byte, error) {
		signCount++
		return []byte(`{"response":"signed-once"}`), nil
	})
	if err != nil || replay || signCount != 1 {
		t.Fatalf("first journal transaction = %q replay=%t signs=%d err=%v", response, replay, signCount, err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal, err = openServerJournal(path)
	if err != nil {
		t.Fatalf("reopen journal: %v", err)
	}
	defer journal.Close()
	replayed, replay, err := journal.transact(context.Background(), record, func() ([]byte, error) {
		signCount++
		return nil, errors.New("must not re-sign")
	})
	if err != nil || !replay || signCount != 1 || !bytes.Equal(replayed, response) {
		t.Fatalf("restarted replay = %q replay=%t signs=%d err=%v, want byte-identical %q", replayed, replay, signCount, err, response)
	}

	conflicts := []serverJournalRequest{
		{RequestHash: testHash("request-two"), Operation: operationDeliver, OperationKey: record.OperationKey, RequestNonceHash: testHash("request-nonce-two"), ResponseNonceHash: testHash("response-nonce-two"), RequestBytes: []byte(`{"request":"two"}`)},
		{RequestHash: testHash("request-three"), Operation: operationReconcile, OperationKey: "reconcile:" + testHash("receipt-two"), RequestNonceHash: record.RequestNonceHash, ResponseNonceHash: testHash("response-nonce-three"), RequestBytes: []byte(`{"request":"three"}`)},
		{RequestHash: testHash("request-four"), Operation: operationCancel, OperationKey: "cancel:" + testHash("cancellation-two"), RequestNonceHash: testHash("request-nonce-four"), ResponseNonceHash: record.ResponseNonceHash, RequestBytes: []byte(`{"request":"four"}`)},
		{RequestHash: testHash("request-five"), Operation: operationReconcile, OperationKey: "reconcile:" + testHash("receipt-five"), RequestNonceHash: record.ResponseNonceHash, ResponseNonceHash: testHash("response-nonce-five"), RequestBytes: []byte(`{"request":"five"}`)},
		{RequestHash: testHash("request-six"), Operation: operationDeliver, OperationKey: "deliver:" + testHash("envelope-six"), RequestNonceHash: testHash("request-nonce-six"), ResponseNonceHash: testHash("response-nonce-six"), AdditionalNonceHash: record.RequestNonceHash, RequestBytes: []byte(`{"request":"six"}`)},
		{RequestHash: record.RequestHash, Operation: record.Operation, OperationKey: record.OperationKey, RequestNonceHash: record.RequestNonceHash, ResponseNonceHash: record.ResponseNonceHash, RequestBytes: []byte(`{"request":"changed"}`)},
	}
	for index, conflict := range conflicts {
		if _, _, err := journal.transact(context.Background(), conflict, func() ([]byte, error) {
			signCount++
			return []byte(`{"must":"not-commit"}`), nil
		}); !errors.Is(err, ErrReplay) {
			t.Fatalf("conflict %d error = %v, want %v", index, err, ErrReplay)
		}
	}
	if signCount != 1 {
		t.Fatalf("conflicts invoked signer %d times, want once total", signCount)
	}
	if _, err := journal.db.Exec(`UPDATE trusted_supervisor_requests SET response_bytes = response_bytes`); err == nil {
		t.Fatal("immutable request row accepted UPDATE")
	}
	if _, err := journal.db.Exec(`DELETE FROM trusted_supervisor_replays`); err == nil {
		t.Fatal("immutable replay row accepted DELETE")
	}
	var replayRows int
	if err := journal.db.QueryRow(`SELECT COUNT(*) FROM trusted_supervisor_replays`).Scan(&replayRows); err != nil || replayRows != 1 {
		t.Fatalf("replay row count = %d, %v; want 1", replayRows, err)
	}
}

func TestProductionServerBoundsSocketFramesAndSameUIDImpostorBeforeRealClient(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	running := startInProcessProductionServer(t, material, now)
	defer running.stop(t)

	information, err := os.Lstat(material.socketPath)
	if err != nil || information.Mode()&os.ModeSocket == 0 || information.Mode().Perm() != 0o600 {
		t.Fatalf("published socket = %v, %v; want mode-0600 Unix socket", information, err)
	}
	directoryInformation, err := os.Stat(material.directory)
	if err != nil || directoryInformation.Mode().Perm() != 0o700 {
		t.Fatalf("operator directory = %v, %v; want mode 0700", directoryInformation, err)
	}

	for name, payload := range map[string][]byte{
		"oversized":     oversizedServerTestFrame(maxFrameBytes + 1),
		"noncanonical":  framedServerTestPayload([]byte("{ \"schema_version\": \"bad\" }")),
		"same_uid_fake": framedServerTestPayload([]byte("{}")),
	} {
		t.Run(name, func(t *testing.T) {
			connection, err := net.DialTimeout("unix", material.socketPath, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			_ = connection.SetDeadline(time.Now().Add(time.Second))
			if _, err := connection.Write(payload); err != nil {
				_ = connection.Close()
				t.Fatal(err)
			}
			if closeWriter, ok := connection.(interface{ CloseWrite() error }); ok {
				_ = closeWriter.CloseWrite()
			}
			response, _ := io.ReadAll(connection)
			_ = connection.Close()
			if len(response) != 0 {
				t.Fatalf("rejected %s received %d response bytes", name, len(response))
			}
		})
	}

	client := newServerTestClient(t, material, int32(os.Getpid()), now)
	if _, err := client.Deliver(context.Background(), material.fixture.envelope); err != nil {
		t.Fatalf("real client after rejected impostors: %v", err)
	}
}

func TestProductionServerExactWireReplayAfterRestartIsByteIdentical(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	first := startInProcessProductionServer(t, material, now)
	capture := &wireCapture{}
	config := signedTestConfig(material.socketPath, int32(os.Getpid()), material.fixture.bundle, now, nil)
	config.ExpectedPredecessorReleaseIdentity = predecessorReleaseIdentityFromEnvelope(material.fixture.envelope)
	config.DialContext = capture.dial
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Deliver(context.Background(), material.fixture.envelope); err != nil {
		t.Fatalf("initial delivery: %v", err)
	}
	requestBytes, responseBytes := capture.snapshot()
	if len(requestBytes) < 4 || int(binary.BigEndian.Uint32(requestBytes[:4])) != len(requestBytes)-4 {
		t.Fatalf("captured projected request frame is malformed: %d bytes", len(requestBytes))
	}
	assertNoAuthorityFields(t, requestBytes[4:], material.fixture.envelope.RepositoryIdentity)
	assertClosedWireEnvelopeReference(t, requestBytes[4:], material.fixture.envelope.EnvelopeHash)
	first.stop(t)

	second := startInProcessProductionServer(t, material, now)
	defer second.stop(t)
	connection, err := net.DialTimeout("unix", material.socketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := connection.Write(requestBytes); err != nil {
		t.Fatal(err)
	}
	if closeWriter, ok := connection.(interface{ CloseWrite() error }); ok {
		_ = closeWriter.CloseWrite()
	}
	replayed, err := io.ReadAll(connection)
	_ = connection.Close()
	if err != nil || !bytes.Equal(replayed, responseBytes) {
		t.Fatalf("restart replay bytes equal=%t err=%v\n got=%x\nwant=%x", bytes.Equal(replayed, responseBytes), err, replayed, responseBytes)
	}

	conflictingClient := newServerTestClient(t, material, int32(os.Getpid()), now)
	if _, err := conflictingClient.Deliver(context.Background(), material.fixture.envelope); err == nil {
		t.Fatal("same operation with fresh nonces did not fail closed")
	}
}

func TestProductionServerSeparateProcessClientSubmitCrashRestartReconcileCancel(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	productionBinary := buildProductionServerCommand(t)
	testOnlyBinary := buildCompileTimeTestServerCommand(t)

	first := startProductionServerProcess(t, testOnlyBinary, material)
	client := newServerTestClient(t, material, int32(first.command.Process.Pid), now)
	receipt, err := client.Deliver(context.Background(), material.fixture.envelope)
	if err != nil {
		first.killAndWait(t)
		t.Fatalf("separate-process Deliver: %v\n%s", err, first.output.String())
	}
	first.killAndWait(t)

	second := startProductionServerProcess(t, testOnlyBinary, material)
	client = newServerTestClient(t, material, int32(second.command.Process.Pid), now)
	var callback *store.ExternalSupervisorAuthenticatedCallback
	reconcileDeadline := time.Now().Add(10 * time.Second)
	for callback == nil {
		callback, err = client.Reconcile(context.Background(), material.fixture.envelope, receipt)
		if err != nil {
			second.killAndWait(t)
			t.Fatalf("restart Reconcile = %+v, %v\n%s", callback, err, second.output.String())
		}
		if callback == nil {
			if time.Now().After(reconcileDeadline) {
				second.killAndWait(t)
				t.Fatal("restart audit remained nonterminal")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if callback.Callback.ResultSchemaVersion != store.ExternalSupervisorAuditNotRunResultSchemaVersion ||
		callback.Callback.TerminalState != store.ExternalSupervisorWaitingForHumanState {
		second.killAndWait(t)
		t.Fatalf("callback inferred an audit outcome: %+v", callback.Callback)
	}
	cancellation, err := store.SealExternalSupervisorCancellation(store.ExternalSupervisorCancellation{
		SchemaVersion: store.ExternalSupervisorCancellationSchemaVersion,
		HandoffID:     material.fixture.envelope.HandoffID, EnvelopeHash: material.fixture.envelope.EnvelopeHash,
		ReceiptIdentityHash: receipt.Receipt.ReceiptHash, AttemptNumber: material.fixture.envelope.AttemptNumber,
	})
	if err != nil {
		second.killAndWait(t)
		t.Fatal(err)
	}
	if _, err := client.Cancel(context.Background(), material.fixture.envelope, receipt, cancellation); err == nil || strings.Contains(err.Error(), "incomplete frame") {
		second.killAndWait(t)
		t.Fatalf("restart cancel conflict error = %v; want canonical conflict", err)
	}
	second.terminateAndWait(t)
	waitForServerSocketAbsence(t, material.socketPath)

	journal, err := sql.Open("sqlite", "file:"+material.journalPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	for table, want := range map[string]int{"trusted_supervisor_requests": 2, "trusted_supervisor_replays": 0} {
		var got int
		if err := journal.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s count = %d, %v; want %d", table, got, err, want)
		}
	}
	assertPrivateKeyAbsent(t, []string{material.journalPath, material.journalPath + "-wal", material.journalPath + "-shm"}, material.privateText)
	productionBinaryBytes, err := os.ReadFile(productionBinary)
	if err != nil {
		t.Fatal(err)
	}
	testOnlyBinaryBytes, err := os.ReadFile(testOnlyBinary)
	if err != nil {
		t.Fatal(err)
	}
	privateRaw, err := hex.DecodeString(strings.TrimPrefix(material.privateText, "ed25519-private:"))
	if err != nil {
		t.Fatal(err)
	}
	const testOnlyRuntimeAuthorityMarker = "ananke-compile-time-test-only-runtime-authority-v1"
	if bytes.Contains(productionBinaryBytes, []byte(testOnlyRuntimeAuthorityMarker)) || !bytes.Contains(testOnlyBinaryBytes, []byte(testOnlyRuntimeAuthorityMarker)) {
		t.Fatal("compile-time test runtime authority marker was not confined to the tagged test binary")
	}
	if bytes.Contains(productionBinaryBytes, privateRaw) || bytes.Contains(productionBinaryBytes, []byte("ananke-test-ed25519:")) ||
		bytes.Contains(productionBinaryBytes, []byte("processSignedUnixServer")) || bytes.Contains(productionBinaryBytes, []byte("credential-must-not-leak")) ||
		bytes.Contains(productionBinaryBytes, []byte("bounded read-only audit report")) {
		t.Fatal("production server binary contains a test key, fake server, or fake wrapper marker")
	}
	if first.output.Len() != 0 || second.output.Len() != 0 {
		t.Fatalf("production server emitted unexpected output; first=%q second=%q", first.output.String(), second.output.String())
	}
}

func TestProductionServerRejectsUnsafeSocketDirectoryAndJournalReplacement(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	if err := os.Chmod(material.directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := newServerForTest(serverConfigForTest(material, now)); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("wide socket directory error = %v, want %v", err, ErrAuthentication)
	}
	if err := os.Chmod(material.directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(material.directory, "journal-target.sqlite")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, material.journalPath); err != nil {
		t.Fatal(err)
	}
	if _, err := newServerForTest(serverConfigForTest(material, now)); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("journal symlink error = %v, want %v", err, ErrAuthentication)
	}
}

type runningTestServer struct {
	cancel context.CancelFunc
	done   chan error
	server *Server
}

func startInProcessProductionServer(t *testing.T, material serverTestMaterial, now time.Time) *runningTestServer {
	t.Helper()
	server, err := newServerForTest(serverConfigForTest(material, now))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	running := &runningTestServer{cancel: cancel, done: make(chan error, 1), server: server}
	go func() { running.done <- server.Serve(ctx) }()
	waitForServerSocket(t, material.socketPath)
	return running
}

func (running *runningTestServer) stop(t *testing.T) {
	t.Helper()
	running.cancel()
	_ = running.server.Close()
	select {
	case err := <-running.done:
		if err != nil {
			t.Fatalf("server shutdown: %v", err)
		}
	case <-time.After(integrationTestExchangeBudget):
		t.Fatal("server did not stop")
	}
}

func newServerTestClient(t *testing.T, material serverTestMaterial, processID int32, now time.Time) *Client {
	t.Helper()
	config := signedTestConfig(material.socketPath, processID, material.fixture.bundle, now, nil)
	config.ExpectedPredecessorReleaseIdentity = predecessorReleaseIdentityFromEnvelope(material.fixture.envelope)
	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func serverConfigForTest(material serverTestMaterial, now time.Time) ServerConfig {
	return ServerConfig{
		SocketPath: material.socketPath, TrustBundlePath: material.bundlePath, PrivateKeyBundlePath: material.keyBundlePath,
		RepositoryPolicyPath: material.repositoryPolicyPath, ExecutionPolicyPath: material.executionPolicyPath,
		JournalPath:                        material.journalPath,
		ExpectedPredecessorReleaseIdentity: predecessorReleaseIdentityFromEnvelope(material.fixture.envelope),
		ExpectedClientUserID:               uint32(os.Getuid()), MaxFrameBytes: maxFrameBytes,
		ConnectionTimeout: integrationTestExchangeBudget, Now: func() time.Time { return now },
		testBrokerDependencies: fakeAuditBrokerDependencies(),
	}
}

func newServerTestMaterial(t *testing.T, now time.Time) serverTestMaterial {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "ananke-production-supervisor-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		makeAuditTestDirectoriesRemovable(directory)
		_ = os.RemoveAll(directory)
	})
	fixture := newProcessSignedAuthorizationFixture(t, now, "production_server")
	bundleBytes, err := marshalCanonical(fixture.bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(directory, "public-trust-bundle.json")
	if err := os.WriteFile(bundlePath, bundleBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	privateText := "ed25519-private:" + hex.EncodeToString(fixture.keys["peer"])
	keyBundlePath := writeServerPrivateBundle(t, directory, "private-signing-key-bundle.json", fixture.bundle,
		fixture.bundle.SupervisorPeer.Certificate.IssuerRootID,
		fixture.bundle.SupervisorPeer.Certificate.SubjectPublicKey,
		fixture.bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256,
		privateText)
	repositoryPolicyPath := writeServerRepositoryPolicy(t, directory, "repository-policy.json",
		fixture.envelope.RepositoryIdentity, "code.example/operator/second-repository")
	executionPolicyPath := writeServerExecutionPolicyForTest(t, directory, fixture.envelope)
	return serverTestMaterial{
		bundlePath: bundlePath, fixture: fixture, journalPath: filepath.Join(directory, "server-journal.sqlite"),
		keyBundlePath: keyBundlePath, privateText: privateText, directory: directory,
		repositoryPolicyPath: repositoryPolicyPath, executionPolicyPath: executionPolicyPath,
		socketPath: filepath.Join(directory, "supervisor.sock"),
	}
}

func writeServerPrivateBundle(t *testing.T, directory, name string, bundle store.ExternalSupervisorTrustBundle, rootID, publicKey, spki, privateText string) string {
	t.Helper()
	value := map[string]any{
		"schema_version":    privateSigningKeyBundleSchemaVersion,
		"trust_bundle_hash": bundle.TrustBundleHash,
		"keys": []any{map[string]any{
			"role": "independent_supervisor_protocol_adapter", "root_id": rootID,
			"public_key": publicKey, "spki_sha256": spki, "private_key": privateText,
		}},
	}
	return writeServerPrivateBundleValue(t, directory, name, value)
}

func writeServerPrivateBundleWithTrustHash(t *testing.T, directory, name string, bundle store.ExternalSupervisorTrustBundle, trustHash, privateText string) string {
	t.Helper()
	value := map[string]any{
		"schema_version": privateSigningKeyBundleSchemaVersion, "trust_bundle_hash": trustHash,
		"keys": []any{map[string]any{
			"role": "independent_supervisor_protocol_adapter", "root_id": bundle.SupervisorPeer.Certificate.IssuerRootID,
			"public_key":  bundle.SupervisorPeer.Certificate.SubjectPublicKey,
			"spki_sha256": bundle.SupervisorPeer.Certificate.SubjectKeySPKISHA256, "private_key": privateText,
		}},
	}
	return writeServerPrivateBundleValue(t, directory, name, value)
}

func writeServerPrivateBundleValue(t *testing.T, directory, name string, value any) string {
	t.Helper()
	encoded, err := marshalCanonical(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func deterministicServerTestKey(name string) ed25519.PrivateKey {
	digest := sha256.Sum256([]byte("ananke-production-server-test:" + name))
	return ed25519.NewKeyFromSeed(digest[:])
}

func oversizedServerTestFrame(size uint32) []byte {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], size)
	return header[:]
}

func framedServerTestPayload(payload []byte) []byte {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	return append(header[:], payload...)
}

type wireCapture struct {
	mu       sync.Mutex
	request  bytes.Buffer
	response bytes.Buffer
}

func (capture *wireCapture) dial(ctx context.Context, network, address string) (net.Conn, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return &capturingConnection{Conn: connection, capture: capture}, nil
}

func (capture *wireCapture) snapshot() ([]byte, []byte) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return append([]byte(nil), capture.request.Bytes()...), append([]byte(nil), capture.response.Bytes()...)
}

type capturingConnection struct {
	net.Conn
	capture *wireCapture
}

func (connection *capturingConnection) Write(value []byte) (int, error) {
	written, err := connection.Conn.Write(value)
	connection.capture.mu.Lock()
	_, _ = connection.capture.request.Write(value[:written])
	connection.capture.mu.Unlock()
	return written, err
}

func (connection *capturingConnection) Read(value []byte) (int, error) {
	read, err := connection.Conn.Read(value)
	connection.capture.mu.Lock()
	_, _ = connection.capture.response.Write(value[:read])
	connection.capture.mu.Unlock()
	return read, err
}

func (connection *capturingConnection) CloseWrite() error {
	if closeWriter, ok := connection.Conn.(interface{ CloseWrite() error }); ok {
		return closeWriter.CloseWrite()
	}
	return nil
}

func (connection *capturingConnection) SyscallConn() (syscall.RawConn, error) {
	raw, ok := connection.Conn.(syscall.Conn)
	if !ok {
		return nil, errors.New("captured connection has no raw descriptor")
	}
	return raw.SyscallConn()
}

type productionServerProcess struct {
	command *exec.Cmd
	output  bytes.Buffer
}

func buildProductionServerCommand(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "ananke-trusted-supervisor")
	command := exec.Command("go", "build", "-o", binary, "./cmd/ananke-trusted-supervisor")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build production server: %v\n%s", err, output)
	}
	return binary
}

func buildCompileTimeTestServerCommand(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "ananke-trusted-supervisor-test-only")
	command := exec.Command("go", "build", "-tags", "ananke_test_runtime_authority", "-o", binary, "./cmd/ananke-trusted-supervisor")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build compile-time test server: %v\n%s", err, output)
	}
	return binary
}

func startProductionServerProcess(t *testing.T, binary string, material serverTestMaterial) *productionServerProcess {
	t.Helper()
	_ = os.Remove(material.socketPath)
	process := &productionServerProcess{}
	process.command = exec.Command(binary,
		"--socket", material.socketPath,
		"--trust-bundle", material.bundlePath,
		"--private-key-bundle", material.keyBundlePath,
		"--journal", material.journalPath,
		"--repository-policy", material.repositoryPolicyPath,
		"--execution-policy", material.executionPolicyPath,
		"--expected-client-uid", strconv.Itoa(os.Getuid()),
		"--runtime-uid", strconv.Itoa(os.Getuid()+1),
		"--runtime-gid", strconv.Itoa(os.Getgid()+1),
		"--timeout", "2s",
		"--max-frame-bytes", strconv.Itoa(int(maxFrameBytes)),
	)
	process.command.Stdout, process.command.Stderr = &process.output, &process.output
	if err := process.command.Start(); err != nil {
		t.Fatalf("start production server: %v", err)
	}
	waitForServerSocket(t, material.socketPath)
	return process
}

const fakeBrokerServerProcessEnvironment = "ANANKE_TEST_FAKE_BROKER_SERVER_PROCESS"

func startFakeBrokerServerProcess(t *testing.T, material serverTestMaterial, now time.Time) *productionServerProcess {
	t.Helper()
	_ = os.Remove(material.socketPath)
	predecessor := predecessorReleaseIdentityFromEnvelope(material.fixture.envelope)
	process := &productionServerProcess{}
	process.command = exec.Command(os.Args[0], "-test.run=^TestFakeBrokerServerProcess$")
	process.command.Env = append(os.Environ(),
		fakeBrokerServerProcessEnvironment+"=1",
		"ANANKE_TEST_SOCKET="+material.socketPath,
		"ANANKE_TEST_TRUST_BUNDLE="+material.bundlePath,
		"ANANKE_TEST_PRIVATE_KEY_BUNDLE="+material.keyBundlePath,
		"ANANKE_TEST_JOURNAL="+material.journalPath,
		"ANANKE_TEST_REPOSITORY_POLICY="+material.repositoryPolicyPath,
		"ANANKE_TEST_EXECUTION_POLICY="+material.executionPolicyPath,
		"ANANKE_TEST_SUPERVISOR_ARTIFACT="+predecessor.SupervisorArtifactSHA256,
		"ANANKE_TEST_BUILD_IDENTITY="+predecessor.BuildIdentityHash,
		"ANANKE_TEST_RELEASE_ATTESTATION="+predecessor.ReleaseAttestationHash,
		"ANANKE_TEST_RELEASE_APPROVAL="+predecessor.ReleaseApprovalHash,
		"ANANKE_TEST_NOW="+now.Format(time.RFC3339Nano),
	)
	process.command.Stdout, process.command.Stderr = &process.output, &process.output
	if err := process.command.Start(); err != nil {
		t.Fatalf("start fake-broker server process: %v", err)
	}
	waitForServerSocket(t, material.socketPath)
	return process
}

func TestFakeBrokerServerProcess(t *testing.T) {
	if os.Getenv(fakeBrokerServerProcessEnvironment) != "1" {
		return
	}
	now, err := time.Parse(time.RFC3339Nano, os.Getenv("ANANKE_TEST_NOW"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := newServerForTest(ServerConfig{
		SocketPath: os.Getenv("ANANKE_TEST_SOCKET"), TrustBundlePath: os.Getenv("ANANKE_TEST_TRUST_BUNDLE"),
		PrivateKeyBundlePath: os.Getenv("ANANKE_TEST_PRIVATE_KEY_BUNDLE"), JournalPath: os.Getenv("ANANKE_TEST_JOURNAL"),
		RepositoryPolicyPath: os.Getenv("ANANKE_TEST_REPOSITORY_POLICY"), ExecutionPolicyPath: os.Getenv("ANANKE_TEST_EXECUTION_POLICY"),
		ExpectedPredecessorReleaseIdentity: store.ExternalSupervisorPredecessorReleaseIdentity{
			SupervisorArtifactSHA256: os.Getenv("ANANKE_TEST_SUPERVISOR_ARTIFACT"), BuildIdentityHash: os.Getenv("ANANKE_TEST_BUILD_IDENTITY"),
			ReleaseAttestationHash: os.Getenv("ANANKE_TEST_RELEASE_ATTESTATION"), ReleaseApprovalHash: os.Getenv("ANANKE_TEST_RELEASE_APPROVAL"),
		},
		ExpectedClientUserID: uint32(os.Getuid()), MaxFrameBytes: maxFrameBytes, ConnectionTimeout: 2 * time.Second,
		Now: func() time.Time { return now }, testBrokerDependencies: fakeAuditBrokerDependencies(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	if err := server.Serve(ctx); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func (process *productionServerProcess) killAndWait(t *testing.T) {
	t.Helper()
	if err := process.command.Process.Kill(); err != nil {
		t.Fatalf("kill production server: %v", err)
	}
	if err := process.command.Wait(); err == nil {
		t.Fatal("killed production server exited successfully")
	}
}

func (process *productionServerProcess) terminateAndWait(t *testing.T) {
	t.Helper()
	if err := process.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("terminate production server: %v", err)
	}
	if err := process.command.Wait(); err != nil {
		t.Fatalf("graceful production server exit: %v\n%s", err, process.output.String())
	}
}

func waitForServerSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		information, err := os.Lstat(path)
		if err == nil && information.Mode()&os.ModeSocket != 0 && information.Mode().Perm() == 0o600 {
			return
		}
		if err == nil && information.Mode()&os.ModeSocket == 0 {
			t.Fatalf("server path is not a socket: %v", information.Mode())
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inspect server socket: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("server socket not ready: %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForServerSocketAbsence(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("server socket remains after shutdown: %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertPrivateKeyAbsent(t *testing.T, paths []string, privateText string) {
	t.Helper()
	privateRaw, err := hex.DecodeString(strings.TrimPrefix(privateText, "ed25519-private:"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read leakage target %s: %v", path, err)
		}
		if bytes.Contains(contents, []byte(privateText)) || bytes.Contains(contents, privateRaw) {
			t.Fatalf("private signing material leaked into %s", path)
		}
	}
}
