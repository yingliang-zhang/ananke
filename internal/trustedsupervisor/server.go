package trustedsupervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

const (
	maxServerConnections   = 32
	maxUnixSocketPathBytes = 103
)

type serverLifecycleState uint8

const (
	serverLifecycleOpen serverLifecycleState = iota
	serverLifecycleClosing
	serverLifecycleClosed
)

type serverLifecycleHooks struct {
	acceptUnix             func(*net.UnixListener) (*net.UnixConn, error)
	afterAdmission         func(net.Conn)
	beforeWait             func()
	beforeReleaseResources func()
}

// ServerConfig contains only operator-owned local paths, bounded transport
// limits, and the expected client UID. Private key values never enter config or
// argv.
type ServerConfig struct {
	SocketPath                         string
	TrustBundlePath                    string
	PrivateKeyBundlePath               string
	JournalPath                        string
	RepositoryPolicyPath               string
	ExpectedPredecessorReleaseIdentity store.ExternalSupervisorPredecessorReleaseIdentity
	ExpectedClientUserID               uint32
	MaxFrameBytes                      uint32
	ConnectionTimeout                  time.Duration
	Now                                func() time.Time
}

// Server owns one production local Unix listener and a separate durable replay
// journal. It has no execution, source, artifact, evidence, OMP, repair, or Run
// capability.
type Server struct {
	config           ServerConfig
	listener         *net.UnixListener
	journal          *serverJournal
	material         *serverSigningMaterial
	repositoryPolicy *repositoryPolicy
	socketDir        *pinnedOperatorDirectory
	socketDevice     uint64
	socketInode      uint64
	semaphore        chan struct{}
	lifecycleMu      sync.Mutex
	lifecycleState   serverLifecycleState
	lifecycleDone    chan struct{}
	connections      map[net.Conn]struct{}
	workers          sync.WaitGroup
	lifecycleHooks   serverLifecycleHooks
	closeErr         error
}

type pinnedOperatorDirectory struct {
	file     *os.File
	path     string
	device   uint64
	inode    uint64
	ownerUID uint32
}

func NewServer(config ServerConfig) (*Server, error) {
	if config.SocketPath == "" || !filepath.IsAbs(config.SocketPath) || strings.IndexByte(config.SocketPath, 0) >= 0 || len(config.SocketPath) > maxUnixSocketPathBytes {
		return nil, fmt.Errorf("%w: absolute bounded Unix socket path required", ErrProtocol)
	}
	if config.JournalPath == "" || !filepath.IsAbs(config.JournalPath) || strings.IndexByte(config.JournalPath, 0) >= 0 {
		return nil, fmt.Errorf("%w: absolute server journal path required", ErrProtocol)
	}
	if config.MaxFrameBytes < minFrameBytes || config.MaxFrameBytes > maxFrameBytes {
		return nil, fmt.Errorf("%w: configured server frame limit", ErrLimit)
	}
	if config.ConnectionTimeout <= 0 || config.ConnectionTimeout > maxTimeout {
		return nil, fmt.Errorf("%w: configured server connection timeout", ErrDeadline)
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	now := config.Now()
	if now.IsZero() || now.Location() != time.UTC {
		return nil, authenticationError("server clock must return UTC")
	}
	ownerUID := uint32(os.Getuid())
	socketDirectory, err := openPinnedOperatorDirectory(filepath.Dir(config.SocketPath), ownerUID)
	if err != nil {
		return nil, err
	}
	closeSocketDirectory := true
	defer func() {
		if closeSocketDirectory {
			_ = socketDirectory.Close()
		}
	}()
	journalDirectory, err := openPinnedOperatorDirectory(filepath.Dir(config.JournalPath), ownerUID)
	if err != nil {
		return nil, err
	}
	if err := journalDirectory.Close(); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(config.SocketPath); err == nil {
		return nil, authenticationError("configured Unix socket path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, authenticationError("inspect configured Unix socket path")
	}
	material, err := loadServerSigningMaterial(config.TrustBundlePath, config.PrivateKeyBundlePath, ownerUID, now)
	if err != nil {
		return nil, err
	}
	closeMaterial := true
	defer func() {
		if closeMaterial {
			material.Close()
		}
	}()
	verifier, err := newEd25519Verifier(material.bundle, config.ExpectedPredecessorReleaseIdentity)
	if err != nil {
		return nil, err
	}
	material.verifier = verifier
	policy, err := loadRepositoryPolicy(config.RepositoryPolicyPath, ownerUID)
	if err != nil {
		return nil, err
	}
	journal, err := openServerJournal(config.JournalPath)
	if err != nil {
		return nil, err
	}
	closeJournal := true
	defer func() {
		if closeJournal {
			_ = journal.Close()
		}
	}()
	if err := socketDirectory.Validate(); err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: config.SocketPath, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen on configured Unix socket: %w", err)
	}
	listener.SetUnlinkOnClose(false)
	closeListener := true
	defer func() {
		if closeListener {
			_ = listener.Close()
			_ = os.Remove(config.SocketPath)
		}
	}()
	if err := os.Chmod(config.SocketPath, 0o600); err != nil {
		return nil, fmt.Errorf("publish private Unix socket: %w", err)
	}
	if err := socketDirectory.Validate(); err != nil {
		return nil, err
	}
	information, err := os.Lstat(config.SocketPath)
	if err != nil || information.Mode()&os.ModeSocket == 0 || information.Mode().Perm() != 0o600 || information.Mode()&os.ModeSymlink != 0 {
		return nil, authenticationError("published Unix socket type or mode")
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != ownerUID {
		return nil, authenticationError("published Unix socket owner")
	}
	server := &Server{
		config: config, listener: listener, journal: journal, material: material, repositoryPolicy: policy, socketDir: socketDirectory,
		socketDevice: uint64(status.Dev), socketInode: status.Ino, semaphore: make(chan struct{}, maxServerConnections),
		connections: make(map[net.Conn]struct{}), lifecycleDone: make(chan struct{}),
	}
	closeSocketDirectory = false
	closeMaterial = false
	closeJournal = false
	closeListener = false
	return server, nil
}

func (server *Server) Serve(ctx context.Context) error {
	if server == nil || ctx == nil {
		return ErrProtocol
	}
	stopClose := context.AfterFunc(ctx, func() { _ = server.Close() })
	defer stopClose()
	for {
		acceptUnix := server.listener.AcceptUnix
		if server.lifecycleHooks.acceptUnix != nil {
			acceptUnix = func() (*net.UnixConn, error) {
				return server.lifecycleHooks.acceptUnix(server.listener)
			}
		}
		connection, err := acceptUnix()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) || server.shutdownStarted() {
				return server.Close()
			}
			return fmt.Errorf("accept configured Unix client: %w", err)
		}
		admitted, closing := server.admitConnection(connection)
		if !admitted {
			_ = connection.Close()
			if closing {
				return server.Close()
			}
			continue
		}
		if server.lifecycleHooks.afterAdmission != nil {
			server.lifecycleHooks.afterAdmission(connection)
		}
		go server.runConnection(ctx, connection)
	}
}

func (server *Server) Close() error {
	if server == nil {
		return nil
	}
	server.lifecycleMu.Lock()
	if server.lifecycleState != serverLifecycleOpen {
		done := server.lifecycleDone
		server.lifecycleMu.Unlock()
		<-done
		return server.closeErr
	}
	server.lifecycleState = serverLifecycleClosing
	connections := make([]net.Conn, 0, len(server.connections))
	for connection := range server.connections {
		connections = append(connections, connection)
	}
	server.lifecycleMu.Unlock()

	if server.listener != nil {
		_ = server.listener.Close()
	}
	for _, connection := range connections {
		_ = connection.Close()
	}
	if server.lifecycleHooks.beforeWait != nil {
		server.lifecycleHooks.beforeWait()
	}
	server.workers.Wait()
	if server.lifecycleHooks.beforeReleaseResources != nil {
		server.lifecycleHooks.beforeReleaseResources()
	}

	var closeErr error
	if err := server.removeOwnedSocket(); err != nil {
		closeErr = err
	}
	if server.journal != nil {
		if err := server.journal.Close(); closeErr == nil {
			closeErr = err
		}
	}
	if server.material != nil {
		server.material.Close()
	}
	if server.socketDir != nil {
		if err := server.socketDir.Close(); closeErr == nil {
			closeErr = err
		}
	}

	server.lifecycleMu.Lock()
	server.closeErr = closeErr
	server.lifecycleState = serverLifecycleClosed
	close(server.lifecycleDone)
	server.lifecycleMu.Unlock()
	return closeErr
}

func (server *Server) serveConnection(parent context.Context, connection *net.UnixConn) error {
	defer connection.Close()
	ctx, cancel := context.WithTimeout(parent, server.config.ConnectionTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return fmt.Errorf("%w: set server connection deadline", ErrDeadline)
	}
	stopCancellation := context.AfterFunc(ctx, func() { _ = connection.SetDeadline(time.Now()) })
	defer stopCancellation()
	if _, err := inspectConnectedClient(ctx, connection, server.config.ExpectedClientUserID); err != nil {
		return err
	}
	requestBytes, err := readFrame(connection, server.config.MaxFrameBytes)
	if err != nil {
		return classifyIOError(err)
	}
	var trailing [1]byte
	if count, trailingErr := connection.Read(trailing[:]); count != 0 || (trailingErr != nil && !errors.Is(trailingErr, io.EOF)) {
		if trailingErr != nil {
			return classifyIOError(trailingErr)
		}
		return fmt.Errorf("%w: trailing request bytes", ErrProtocol)
	}
	var request wireRequest
	if err := decodeCanonical(requestBytes, &request); err != nil {
		return err
	}
	operationKey, err := server.validateRequest(ctx, request)
	if err != nil {
		return err
	}
	additionalNonceHash := ""
	if request.Delivery != nil {
		additionalNonceHash = request.Delivery.NonceHash
	} else if request.Receipt != nil {
		additionalNonceHash, err = canonicalHash(map[string]any{
			"schema_version":        "ananke.local-trusted-supervisor-receipt-operation-exclusivity.v1",
			"receipt_identity_hash": request.Receipt.Receipt.ReceiptHash,
		})
		if err != nil {
			return err
		}
	}
	responseBytes, _, err := server.journal.transact(ctx, serverJournalRequest{
		RequestHash: request.RequestHash, Operation: request.Operation, OperationKey: operationKey,
		RequestNonceHash: request.RequestNonceHash, ResponseNonceHash: request.ResponseNonceHash,
		AdditionalNonceHash: additionalNonceHash,
		RequestBytes:        requestBytes,
	}, func() ([]byte, error) {
		response, err := server.buildResponse(ctx, request)
		if err != nil {
			return nil, err
		}
		encoded, err := marshalCanonical(response)
		if err != nil {
			return nil, fmt.Errorf("%w: canonical server response", ErrProtocol)
		}
		return encoded, nil
	})
	if err != nil {
		return err
	}
	return writeFrame(connection, responseBytes, server.config.MaxFrameBytes)
}

func (server *Server) admitConnection(connection *net.UnixConn) (admitted bool, closing bool) {
	server.lifecycleMu.Lock()
	defer server.lifecycleMu.Unlock()
	if server.lifecycleState != serverLifecycleOpen {
		return false, true
	}
	select {
	case server.semaphore <- struct{}{}:
		server.connections[connection] = struct{}{}
		server.workers.Add(1)
		return true, false
	default:
		return false, false
	}
}

func (server *Server) runConnection(ctx context.Context, connection *net.UnixConn) {
	defer server.workers.Done()
	defer func() { <-server.semaphore }()
	defer func() {
		server.lifecycleMu.Lock()
		delete(server.connections, connection)
		server.lifecycleMu.Unlock()
	}()
	_ = server.serveConnection(ctx, connection)
}

func (server *Server) shutdownStarted() bool {
	server.lifecycleMu.Lock()
	defer server.lifecycleMu.Unlock()
	return server.lifecycleState != serverLifecycleOpen
}

func (server *Server) removeOwnedSocket() error {
	if server.socketDir == nil || server.config.SocketPath == "" {
		return nil
	}
	if err := server.socketDir.Validate(); err != nil {
		return err
	}
	information, err := os.Lstat(server.config.SocketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return authenticationError("inspect Unix socket during shutdown")
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	if !ok || information.Mode()&os.ModeSocket == 0 || uint64(status.Dev) != server.socketDevice || status.Ino != server.socketInode {
		return authenticationError("Unix socket replaced before shutdown")
	}
	if err := os.Remove(server.config.SocketPath); err != nil {
		return fmt.Errorf("remove owned Unix socket: %w", err)
	}
	return fsyncDirectory(filepath.Dir(server.config.SocketPath))
}

func openPinnedOperatorDirectory(path string, ownerUID uint32) (*pinnedOperatorDirectory, error) {
	if path == "" || !filepath.IsAbs(path) || strings.IndexByte(path, 0) >= 0 {
		return nil, authenticationError("absolute operator directory required")
	}
	information, err := os.Lstat(path)
	if err != nil || information.Mode()&os.ModeSymlink != 0 || !information.IsDir() || information.Mode().Perm() != 0o700 {
		return nil, authenticationError("operator directory type or mode")
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != ownerUID {
		return nil, authenticationError("operator directory owner")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, authenticationError("open operator directory")
	}
	file := os.NewFile(uintptr(fd), filepath.Base(path))
	if file == nil {
		_ = unix.Close(fd)
		return nil, authenticationError("open operator directory descriptor")
	}
	directory := &pinnedOperatorDirectory{file: file, path: path, device: uint64(status.Dev), inode: status.Ino, ownerUID: ownerUID}
	if err := directory.Validate(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return directory, nil
}

func (directory *pinnedOperatorDirectory) Validate() error {
	if directory == nil || directory.file == nil {
		return authenticationError("operator directory identity")
	}
	information, err := os.Lstat(directory.path)
	if err != nil || information.Mode()&os.ModeSymlink != 0 || !information.IsDir() || information.Mode().Perm() != 0o700 {
		return authenticationError("operator directory replacement")
	}
	pathStat, ok := information.Sys().(*syscall.Stat_t)
	if !ok || pathStat.Uid != directory.ownerUID || uint64(pathStat.Dev) != directory.device || pathStat.Ino != directory.inode {
		return authenticationError("operator directory replacement")
	}
	var descriptorStat unix.Stat_t
	if err := unix.Fstat(int(directory.file.Fd()), &descriptorStat); err != nil || descriptorStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		descriptorStat.Uid != directory.ownerUID || descriptorStat.Mode&0o777 != 0o700 ||
		uint64(descriptorStat.Dev) != directory.device || descriptorStat.Ino != directory.inode {
		return authenticationError("operator directory descriptor replacement")
	}
	return nil
}

func (directory *pinnedOperatorDirectory) Close() error {
	if directory == nil || directory.file == nil {
		return nil
	}
	err := directory.file.Close()
	directory.file = nil
	return err
}
