package trustedsupervisor

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestProductionServerLifecycleGateClosesAcceptCompletedAfterClose(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	server, err := NewServer(serverConfigForTest(material, now))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	accepted, client := newLifecycleUnixConnectionPair(t, material.directory)

	acceptStarted := make(chan struct{})
	releaseAccept := make(chan struct{})
	var acceptCalls atomic.Int32
	admitted := make(chan struct{}, 1)
	server.lifecycleHooks = serverLifecycleHooks{
		acceptUnix: func(*net.UnixListener) (*net.UnixConn, error) {
			if acceptCalls.Add(1) != 1 {
				return nil, net.ErrClosed
			}
			close(acceptStarted)
			<-releaseAccept
			return accepted, nil
		},
		afterAdmission: func(net.Conn) { admitted <- struct{}{} },
	}

	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	waitForLifecycleSignal(t, acceptStarted, "accept hook")

	if err := server.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(releaseAccept)

	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Serve after Close: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve retained an accept completed after Close")
	}
	select {
	case <-admitted:
		t.Fatal("connection accepted after shutdown was admitted")
	default:
	}
	assertLifecycleConnectionClosed(t, client)
	if err := server.Close(); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}
}

func TestProductionServerLifecycleGateWaitsForAdmittedWorkerBeforeResourceRelease(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	server, err := NewServer(serverConfigForTest(material, now))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	privateKeyAlias := server.material.privateKey

	admitted := make(chan struct{})
	releaseAdmission := make(chan struct{})
	beforeWait := make(chan struct{})
	var admitOnce sync.Once
	var waitOnce sync.Once
	server.lifecycleHooks = serverLifecycleHooks{
		afterAdmission: func(net.Conn) {
			admitOnce.Do(func() { close(admitted) })
			<-releaseAdmission
		},
		beforeWait: func() { waitOnce.Do(func() { close(beforeWait) }) },
	}

	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	client, err := net.DialTimeout("unix", material.socketPath, time.Second)
	if err != nil {
		t.Fatalf("dial production server: %v", err)
	}
	defer client.Close()
	waitForLifecycleSignal(t, admitted, "admission hook")

	closeDone := make(chan error, 1)
	go func() { closeDone <- server.Close() }()
	waitForLifecycleSignal(t, beforeWait, "pre-wait hook")

	if bytes.Count(privateKeyAlias, []byte{0}) == len(privateKeyAlias) {
		t.Fatal("private key was zeroed before the admitted worker completed")
	}
	if err := server.journal.db.Ping(); err != nil {
		t.Fatalf("journal closed before the admitted worker completed: %v", err)
	}
	if err := server.socketDir.Validate(); err != nil {
		t.Fatalf("directory anchor closed before the admitted worker completed: %v", err)
	}
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before admitted worker launch: %v", err)
	default:
	}

	close(releaseAdmission)
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not finish after the admitted worker exited")
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Serve shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not stop after Close")
	}
	assertZeroedLifecycleAlias(t, privateKeyAlias, "private key after Close")
	if err := server.journal.db.Ping(); err == nil {
		t.Fatal("journal remained open after admitted worker exit")
	}
	if server.socketDir.file != nil {
		t.Fatal("directory anchor remained open after admitted worker exit")
	}
}

func TestProductionServerLifecycleGateConcurrentCloseIsIdempotentAndSocketReusable(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	server, err := NewServer(serverConfigForTest(material, now))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	beforeRelease := make(chan struct{})
	releaseResources := make(chan struct{})
	var releaseCalls atomic.Int32
	server.lifecycleHooks = serverLifecycleHooks{
		beforeReleaseResources: func() {
			if releaseCalls.Add(1) == 1 {
				close(beforeRelease)
			}
			<-releaseResources
		},
	}

	const closers = 16
	results := make(chan error, closers)
	for index := 0; index < closers; index++ {
		go func() { results <- server.Close() }()
	}
	waitForLifecycleSignal(t, beforeRelease, "resource-release hook")
	select {
	case err := <-results:
		t.Fatalf("concurrent Close returned before resource release: %v", err)
	default:
	}
	close(releaseResources)
	for index := 0; index < closers; index++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("concurrent Close %d: %v", index, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("concurrent Close %d did not return", index)
		}
	}
	if got := releaseCalls.Load(); got != 1 {
		t.Fatalf("resource release ran %d times, want once", got)
	}
	if _, err := os.Lstat(material.socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned socket remained after Close: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("later idempotent Close: %v", err)
	}

	replacement, err := NewServer(serverConfigForTest(material, now))
	if err != nil {
		t.Fatalf("socket was not reusable after clean Close: %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatalf("replacement Close: %v", err)
	}
}

func TestProductionServerLifecycleGateRefusesSocketReplacementDuringClose(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	material := newServerTestMaterial(t, now)
	server, err := NewServer(serverConfigForTest(material, now))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	privateKeyAlias := server.material.privateKey

	ownedPath := material.socketPath + ".owned"
	if err := os.Rename(material.socketPath, ownedPath); err != nil {
		t.Fatalf("move owned socket: %v", err)
	}
	replacement, err := net.ListenUnix("unix", &net.UnixAddr{Name: material.socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("publish replacement socket: %v", err)
	}
	replacement.SetUnlinkOnClose(false)
	t.Cleanup(func() {
		_ = replacement.Close()
		_ = os.Remove(material.socketPath)
		_ = os.Remove(ownedPath)
	})
	if err := os.Chmod(material.socketPath, 0o600); err != nil {
		t.Fatalf("chmod replacement socket: %v", err)
	}
	before, err := os.Lstat(material.socketPath)
	if err != nil {
		t.Fatalf("stat replacement socket: %v", err)
	}
	beforeStat := before.Sys().(*syscall.Stat_t)

	if err := server.Close(); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("Close replacement error = %v, want %v", err, ErrAuthentication)
	}
	after, err := os.Lstat(material.socketPath)
	if err != nil {
		t.Fatalf("replacement socket was removed: %v", err)
	}
	afterStat := after.Sys().(*syscall.Stat_t)
	if uint64(afterStat.Dev) != uint64(beforeStat.Dev) || afterStat.Ino != beforeStat.Ino {
		t.Fatal("replacement socket identity changed during Close")
	}
	if information, err := os.Lstat(ownedPath); err != nil || information.Mode()&os.ModeSocket == 0 {
		t.Fatalf("moved owned socket = %v, %v; want retained socket", information, err)
	}
	assertZeroedLifecycleAlias(t, privateKeyAlias, "private key after replacement refusal")
	if err := server.Close(); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("idempotent replacement Close error = %v, want %v", err, ErrAuthentication)
	}
}

func newLifecycleUnixConnectionPair(t *testing.T, directory string) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	path := filepath.Join(directory, "lifecycle-pair.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen for lifecycle pair: %v", err)
	}
	listener.SetUnlinkOnClose(false)
	accepted := make(chan *net.UnixConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- connection
	}()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		_ = listener.Close()
		t.Fatalf("dial lifecycle pair: %v", err)
	}
	var server *net.UnixConn
	select {
	case server = <-accepted:
	case err := <-acceptErr:
		_ = client.Close()
		_ = listener.Close()
		t.Fatalf("accept lifecycle pair: %v", err)
	case <-time.After(3 * time.Second):
		_ = client.Close()
		_ = listener.Close()
		t.Fatal("accept lifecycle pair timed out")
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close lifecycle pair listener: %v", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remove lifecycle pair socket: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})
	return server, client
}

func waitForLifecycleSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func assertLifecycleConnectionClosed(t *testing.T, connection *net.UnixConn) {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set lifecycle client deadline: %v", err)
	}
	var one [1]byte
	count, err := connection.Read(one[:])
	if count != 0 || !errors.Is(err, os.ErrDeadlineExceeded) && !errors.Is(err, net.ErrClosed) && err == nil {
		t.Fatalf("connection accepted after Close remained usable: read=%d err=%v", count, err)
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatal("connection accepted after Close remained open")
	}
}

func assertZeroedLifecycleAlias(t *testing.T, alias []byte, name string) {
	t.Helper()
	for index, value := range alias {
		if value != 0 {
			t.Fatalf("%s byte %d = %d, want zero", name, index, value)
		}
	}
}
