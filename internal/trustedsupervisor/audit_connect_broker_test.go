package trustedsupervisor

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditHTTPGatewayAcceptsExactTransparentFakeIPRouteAndPinsDial(t *testing.T) {
	if !auditPlatformSupported(runtime.GOOS) {
		t.Skip("transparent fake-IP transport is restricted to the Darwin audit boundary")
	}
	var lookupCalls, dialCalls atomic.Int32
	var dialedAuthority string
	gateway, err := startAuditHTTPGateway(
		context.Background(),
		"custom:sudo",
		executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
		time.Minute,
		auditBrokerDependencies{
			LookupIPAddr: func(_ context.Context, hostname string) ([]net.IPAddr, error) {
				lookupCalls.Add(1)
				if hostname != "coding.sudoai.cc" {
					t.Fatalf("resolution hostname = %q, want coding.sudoai.cc", hostname)
				}
				return []net.IPAddr{{IP: net.ParseIP("198.18.0.34")}}, nil
			},
			DialContext: func(_ context.Context, network, authority string) (net.Conn, error) {
				dialCalls.Add(1)
				if network != "tcp" {
					t.Fatalf("pinned dial network = %q, want tcp", network)
				}
				dialedAuthority = authority
				return nil, errors.New("injected pinned dial stop")
			},
		},
	)
	if err != nil {
		t.Fatalf("exact transparent fake-IP route rejected: %v", err)
	}
	defer gateway.Close()
	if gateway.resolutionClass != auditProviderResolutionTransparentFakeIP || gateway.transport.Proxy != nil {
		t.Fatalf("gateway resolution class = %q, proxy configured = %t", gateway.resolutionClass, gateway.transport.Proxy != nil)
	}

	if _, err := gateway.dialPinnedEndpoint(context.Background(), "tcp", "coding.sudoai.cc:443"); err == nil {
		t.Fatal("injected pinned dial unexpectedly succeeded")
	}
	if dialedAuthority != "198.18.0.34:443" || lookupCalls.Load() != 1 || dialCalls.Load() != 1 {
		t.Fatalf("pinned dial authority = %q, lookups = %d, dials = %d", dialedAuthority, lookupCalls.Load(), dialCalls.Load())
	}
	for _, escape := range []struct {
		network   string
		authority string
	}{
		{network: "udp", authority: "coding.sudoai.cc:443"},
		{network: "tcp", authority: "coding.sudoai.cc:444"},
		{network: "tcp", authority: "attacker.example:443"},
		{network: "tcp", authority: "198.18.0.34:443"},
	} {
		if _, err := gateway.dialPinnedEndpoint(context.Background(), escape.network, escape.authority); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("escape dial %s %s error = %v, want %v", escape.network, escape.authority, err, ErrAuthentication)
		}
	}
	if lookupCalls.Load() != 1 || dialCalls.Load() != 1 {
		t.Fatalf("escape attempts caused lookups = %d, dials = %d", lookupCalls.Load(), dialCalls.Load())
	}
}

func TestAuditProviderResolutionClassifiesOnlyExactTransparentFakeIPRange(t *testing.T) {
	if !auditPlatformSupported(runtime.GOOS) {
		t.Skip("transparent fake-IP transport is restricted to the Darwin audit boundary")
	}
	provider := "custom:sudo"
	endpoint := executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443}
	for _, testCase := range []struct {
		address string
		want    auditProviderResolutionClass
	}{
		{address: "198.18.0.0", want: auditProviderResolutionTransparentFakeIP},
		{address: "198.19.255.255", want: auditProviderResolutionTransparentFakeIP},
		{address: "198.17.255.255", want: auditProviderResolutionPublic},
		{address: "198.20.0.0", want: auditProviderResolutionPublic},
	} {
		t.Run(testCase.address, func(t *testing.T) {
			address := netip.MustParseAddr(testCase.address)
			if publicAuditBrokerAddress(address) == (testCase.want == auditProviderResolutionTransparentFakeIP) {
				t.Fatalf("public classification for %s did not stay disjoint from fake-IP classification", address)
			}
			class, err := preflightAuditProviderResolution(context.Background(), provider, endpoint, func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP(testCase.address)}}, nil
			})
			if err != nil || class != testCase.want {
				t.Fatalf("resolution %s class = %q, error = %v; want %q", testCase.address, class, err, testCase.want)
			}
		})
	}
}

func TestAuditProviderResolutionRejectsFakeIPWithoutExactRoute(t *testing.T) {
	if !auditPlatformSupported(runtime.GOOS) {
		t.Skip("transparent fake-IP transport is restricted to the Darwin audit boundary")
	}
	for _, testCase := range []struct {
		name     string
		provider string
		endpoint executionPolicyEndpoint
	}{
		{name: "wrong provider", provider: "anthropic", endpoint: executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443}},
		{name: "wrong hostname", provider: "custom:sudo", endpoint: executionPolicyEndpoint{Hostname: "attacker.example", Port: 443}},
		{name: "wrong port", provider: "custom:sudo", endpoint: executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 444}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var lookups atomic.Int32
			class, err := preflightAuditProviderResolution(context.Background(), testCase.provider, testCase.endpoint, func(context.Context, string) ([]net.IPAddr, error) {
				lookups.Add(1)
				return []net.IPAddr{{IP: net.ParseIP("198.18.0.34")}}, nil
			})
			if !errors.Is(err, ErrAuthentication) || class != auditProviderResolutionInvalid || lookups.Load() != 0 {
				t.Fatalf("unbound fake-IP route class = %q, error = %v, lookups = %d", class, err, lookups.Load())
			}
		})
	}
}

func TestAuditProviderResolutionRejectsMalformedMixedDuplicateAndUnboundedAnswers(t *testing.T) {
	if !auditPlatformSupported(runtime.GOOS) {
		t.Skip("transparent fake-IP transport is restricted to the Darwin audit boundary")
	}
	overBound := make([]net.IPAddr, maxAuditBrokerResolvedIPs+1)
	for index := range overBound {
		overBound[index] = net.IPAddr{IP: net.ParseIP("198.18.0.34")}
	}
	for _, testCase := range []struct {
		name      string
		addresses []net.IPAddr
	}{
		{name: "empty"},
		{name: "malformed", addresses: []net.IPAddr{{IP: net.IP{0xde, 0xad}}}},
		{name: "zone", addresses: []net.IPAddr{{IP: net.ParseIP("198.18.0.34"), Zone: "credential-fixture-zone"}}},
		{name: "mixed public and fake", addresses: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("198.18.0.34")}}},
		{name: "duplicate fake", addresses: []net.IPAddr{{IP: net.ParseIP("198.18.0.34")}, {IP: net.ParseIP("198.18.0.34")}}},
		{name: "over bound", addresses: overBound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			class, err := preflightAuditProviderResolutionForTest(testCase.addresses)
			if !errors.Is(err, ErrAuthentication) || class != auditProviderResolutionInvalid {
				t.Fatalf("unsafe answer class = %q, error = %v", class, err)
			}
			if err != nil && (strings.Contains(err.Error(), "198.18.0.34") || strings.Contains(err.Error(), "credential-fixture-zone")) {
				t.Fatalf("preflight error exposed raw resolution material: %v", err)
			}
		})
	}
}

func TestAuditProviderResolutionKeepsReservedRangesClosed(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		address string
	}{
		{name: "unspecified IPv4", address: "0.0.0.0"},
		{name: "private 10", address: "10.0.0.1"},
		{name: "private 172", address: "172.16.0.1"},
		{name: "private 192", address: "192.168.0.1"},
		{name: "loopback IPv4", address: "127.0.0.1"},
		{name: "link-local IPv4", address: "169.254.1.1"},
		{name: "multicast IPv4", address: "224.0.0.1"},
		{name: "CGNAT", address: "100.64.0.1"},
		{name: "IETF protocol assignments", address: "192.0.0.1"},
		{name: "documentation 1", address: "192.0.2.1"},
		{name: "documentation 2", address: "198.51.100.1"},
		{name: "documentation 3", address: "203.0.113.1"},
		{name: "reserved high IPv4", address: "240.0.0.1"},
		{name: "unspecified IPv6", address: "::"},
		{name: "loopback IPv6", address: "::1"},
		{name: "link-local IPv6", address: "fe80::1"},
		{name: "multicast IPv6", address: "ff02::1"},
		{name: "documentation IPv6", address: "2001:db8::1"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			class, err := preflightAuditProviderResolutionForTest([]net.IPAddr{{IP: net.ParseIP(testCase.address)}})
			if !errors.Is(err, ErrAuthentication) || class != auditProviderResolutionInvalid {
				t.Fatalf("reserved address %s class = %q, error = %v", testCase.address, class, err)
			}
		})
	}
}

func TestAuditProviderResolutionPreflightReturnsOnlySafeTransportClass(t *testing.T) {
	if !auditPlatformSupported(runtime.GOOS) {
		t.Skip("transparent fake-IP transport is restricted to the Darwin audit boundary")
	}
	class, err := preflightAuditProviderResolutionForTest([]net.IPAddr{{IP: net.ParseIP("198.18.0.34")}})
	if err != nil || class != auditProviderResolutionTransparentFakeIP {
		t.Fatalf("fake-IP preflight class = %q, error = %v", class, err)
	}
	if strings.Contains(string(class), "198.18.0.34") || strings.Contains(string(class), "coding.sudoai.cc") {
		t.Fatalf("preflight class exposed route material: %q", class)
	}
}

func TestAuditProviderResolutionPreflightSeparatesAcceptedFakeIPFromListenFailure(t *testing.T) {
	if !auditPlatformSupported(runtime.GOOS) {
		t.Skip("transparent fake-IP transport is restricted to the Darwin audit boundary")
	}
	lookup := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("198.18.0.34")}}, nil
	}
	class, err := preflightAuditProviderResolution(
		context.Background(),
		"custom:sudo",
		executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
		lookup,
	)
	if err != nil || class != auditProviderResolutionTransparentFakeIP {
		t.Fatalf("resolution preflight class = %q, error = %v", class, err)
	}
	var listenCalls atomic.Int32
	gateway, err := startAuditHTTPGateway(
		context.Background(),
		"custom:sudo",
		executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
		time.Minute,
		auditBrokerDependencies{
			LookupIPAddr: lookup,
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				return nil, errors.New("provider dial must not run during preflight")
			},
			ListenContext: func(context.Context, string, string) (net.Listener, error) {
				listenCalls.Add(1)
				return nil, errors.New("injected listener failure")
			},
		},
	)
	if gateway != nil || !errors.Is(err, ErrAuthentication) || listenCalls.Load() != 1 {
		t.Fatalf("listen failure gateway = %v, error = %v, listen calls = %d", gateway, err, listenCalls.Load())
	}
	if strings.Contains(err.Error(), "198.18.0.34") {
		t.Fatalf("listen failure exposed resolved IP: %v", err)
	}
}

func preflightAuditProviderResolutionForTest(addresses []net.IPAddr) (auditProviderResolutionClass, error) {
	return preflightAuditProviderResolution(
		context.Background(),
		"custom:sudo",
		executionPolicyEndpoint{Hostname: "coding.sudoai.cc", Port: 443},
		func(context.Context, string) ([]net.IPAddr, error) { return addresses, nil },
	)
}

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
	if !auditPlatformSupported(runtime.GOOS) {
		t.Skip("transparent fake-IP transport is restricted to the Darwin audit boundary")
	}
	for _, status := range []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests atomic.Int32
			var targetAuthorization, wrongDial atomic.Bool
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
						return []net.IPAddr{{IP: net.ParseIP("198.18.0.34")}}, nil
					},
					DialContext: func(ctx context.Context, network, authority string) (net.Conn, error) {
						wrongDial.Store(authority != "198.18.0.34:443")
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
			if response.StatusCode != http.StatusBadGateway || response.Header.Get("Location") != "" || requests.Load() != 1 ||
				method != http.MethodPost || path != "/v1/responses" || targetAuthorization.Load() || wrongDial.Load() {
				t.Fatalf("redirect %d result=%d location=%q upstream_requests=%d initial=%s %s target_authorization=%t wrong_dial=%t",
					status, response.StatusCode, response.Header.Get("Location"), requests.Load(), method, path, targetAuthorization.Load(), wrongDial.Load())
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
