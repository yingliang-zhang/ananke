package trustedsupervisor

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditHTTPGatewayRejectsPrivateOrReboundResolution(t *testing.T) {
	for _, addresses := range [][]net.IPAddr{
		{{IP: net.ParseIP("127.0.0.1")}},
		{{IP: net.ParseIP("10.0.0.1")}},
		{{IP: net.ParseIP("169.254.1.1")}},
		{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("192.168.1.2")}},
	} {
		var dialed atomic.Bool
		gateway, err := startAuditHTTPGateway(
			context.Background(),
			"custom:sudo",
			executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
			time.Minute,
			auditBrokerDependencies{
				LookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) { return addresses, nil },
				DialContext: func(context.Context, string, string) (net.Conn, error) {
					dialed.Store(true)
					return nil, errors.New("must not dial")
				},
			},
		)
		if gateway != nil {
			_ = gateway.Close()
		}
		if !errors.Is(err, ErrAuthentication) || dialed.Load() {
			t.Fatalf("unsafe resolution %v = gateway %v, error %v, dialed %v", addresses, gateway, err, dialed.Load())
		}
	}
}

func TestAuditHTTPGatewayRejectsEveryUpstreamRedirect(t *testing.T) {
	for _, status := range []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests atomic.Int32
			var targetAuthorization atomic.Bool
			var requestMu sync.Mutex
			initialMethod := ""
			initialPath := ""
			server, roots := newAuditGatewayTLSServerForTest(t, "coding.sudoai.cc", func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.URL.Path == "/redirect-target" {
					targetAuthorization.Store(request.Header.Get("Authorization") != "")
					writer.WriteHeader(http.StatusNoContent)
					return
				}
				requestMu.Lock()
				initialMethod = request.Method
				initialPath = request.URL.Path
				requestMu.Unlock()
				writer.Header().Set("Location", "/redirect-target")
				writer.WriteHeader(status)
			})
			gateway, err := startAuditHTTPGateway(
				context.Background(),
				"custom:sudo",
				executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
				time.Minute,
				auditBrokerDependencies{
					LookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) {
						return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
					},
					DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
						return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
					},
					TLSRootCAs: roots,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			defer gateway.Close()

			request, err := http.NewRequest(http.MethodPost, "http://"+gateway.Address()+"/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer must-not-reach-redirect-target")
			request.Header.Set("Content-Type", "application/json")
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()

			requestMu.Lock()
			method, path := initialMethod, initialPath
			requestMu.Unlock()
			if requests.Load() != 1 || method != http.MethodPost || path != "/v1/responses" || targetAuthorization.Load() {
				t.Fatalf("redirect %d upstream requests=%d initial=%s %s target_authorization=%t", status, requests.Load(), method, path, targetAuthorization.Load())
			}
		})
	}
}

func TestAuditHTTPGatewayCloseReapsBothListenersAndWorkers(t *testing.T) {
	gateway, err := startAuditHTTPGateway(
		context.Background(),
		"custom:sudo",
		executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
		time.Minute,
		fakeAuditBrokerDependencies(),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(gateway.Address())
	if err != nil {
		t.Fatal(err)
	}
	ipv6Address := net.JoinHostPort("::1", port)
	connections := make([]net.Conn, 0, 2)
	for _, address := range []string{gateway.Address(), ipv6Address} {
		connection, dialErr := net.DialTimeout("tcp", address, time.Second)
		if dialErr != nil {
			t.Fatalf("dial gateway listener %q: %v", address, dialErr)
		}
		connections = append(connections, connection)
	}
	deadline := time.Now().Add(time.Second)
	for gateway.activeConnectionCount() != len(connections) {
		if time.Now().After(deadline) {
			t.Fatalf("gateway tracked %d active connections, want %d", gateway.activeConnectionCount(), len(connections))
		}
		time.Sleep(time.Millisecond)
	}
	if err := gateway.Close(); err != nil {
		t.Fatal(err)
	}
	for _, connection := range connections {
		_ = connection.Close()
	}
	if active := gateway.activeConnectionCount(); active != 0 {
		t.Fatalf("gateway retained %d active connections after Close", active)
	}
	select {
	case <-gateway.Done():
	default:
		t.Fatal("gateway workers were not reaped by Close")
	}
	reboundIPv4, err := net.Listen("tcp4", gateway.Address())
	if err != nil {
		t.Fatalf("IPv4 listener remained owned after Close: %v", err)
	}
	defer reboundIPv4.Close()
	reboundIPv6, err := net.Listen("tcp6", ipv6Address)
	if err != nil {
		t.Fatalf("IPv6 listener remained owned after Close: %v", err)
	}
	_ = reboundIPv6.Close()
}

func TestAuditHTTPGatewaySecondListenerErrorRollsBackIPv4(t *testing.T) {
	dependencies := fakeAuditBrokerDependencies()
	listenConfig := &net.ListenConfig{}
	var ipv4 *auditCloseRecordingListener
	dependencies.ListenContext = func(ctx context.Context, network, address string) (net.Listener, error) {
		if network == "tcp6" {
			return nil, errors.New("injected IPv6 bind failure")
		}
		listener, err := listenConfig.Listen(ctx, network, address)
		if err != nil {
			return nil, err
		}
		ipv4 = &auditCloseRecordingListener{Listener: listener}
		return ipv4, nil
	}
	gateway, err := startAuditHTTPGateway(
		context.Background(),
		"custom:sudo",
		executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
		time.Minute,
		dependencies,
	)
	if gateway != nil || !errors.Is(err, ErrAuthentication) {
		t.Fatalf("second-listener failure = gateway %v, error %v; want authentication failure", gateway, err)
	}
	if ipv4 == nil || ipv4.closeCalls.Load() != 1 {
		t.Fatalf("IPv4 rollback close calls = %v, want 1", ipv4)
	}
	rebound, err := net.Listen("tcp4", ipv4.Addr().String())
	if err != nil {
		t.Fatalf("IPv4 listener was not released after IPv6 bind failure: %v", err)
	}
	_ = rebound.Close()
}

type auditCloseRecordingListener struct {
	net.Listener
	closeCalls atomic.Int32
}

func (listener *auditCloseRecordingListener) Close() error {
	listener.closeCalls.Add(1)
	return listener.Listener.Close()
}

func fakeAuditBrokerDependencies() auditBrokerDependencies {
	return auditBrokerDependencies{
		LookupIPAddr: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		},
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("fake audit endpoint did not accept request")
		},
	}
}
