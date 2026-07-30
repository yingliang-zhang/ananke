package trustedsupervisor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxAuditBrokerConnections       = 8
	maxAuditBrokerHeaderBytes       = 16 * 1024
	maxAuditBrokerHeaders           = 64
	maxAuditBrokerBodyBytes         = 32 * 1024 * 1024
	maxAuditBrokerResponseBytes     = 32 * 1024 * 1024
	auditBrokerConnectTimeout       = 5 * time.Second
	auditBrokerHeaderTimeout        = 5 * time.Second
	auditBrokerBodyTimeout          = 30 * time.Second
	auditBrokerTrailingProbeTimeout = 5 * time.Millisecond
	auditBrokerIdleTimeout          = 30 * time.Second
	auditBrokerCloseTimeout         = 2 * time.Second
	maxAuditBrokerLifetime          = 24*time.Hour + 5*time.Minute
	maxAuditBrokerResolvedIPs       = 16
)

type auditBrokerDependencies struct {
	LookupIPAddr  func(context.Context, string) ([]net.IPAddr, error)
	DialContext   func(context.Context, string, string) (net.Conn, error)
	ListenContext func(context.Context, string, string) (net.Listener, error)
	TLSRootCAs    *x509.CertPool
}

type auditProviderResolutionClass string

const (
	auditProviderResolutionInvalid           auditProviderResolutionClass = ""
	auditProviderResolutionPublic            auditProviderResolutionClass = "ordinary_public_upstream"
	auditProviderResolutionTransparentFakeIP auditProviderResolutionClass = "transparent_fake_ip_transport_alias"
)

type auditProviderResolution struct {
	class     auditProviderResolutionClass
	pinnedIPs []netip.Addr
}

type auditHTTPGateway struct {
	provider          string
	endpoint          executionPolicyEndpoint
	upstreamAuthority string
	path              string
	address           string
	ipv4Listener      net.Listener
	ipv6Listener      net.Listener
	context           context.Context
	cancel            context.CancelFunc
	deadline          time.Time
	dialContext       func(context.Context, string, string) (net.Conn, error)
	pinnedIPs         []netip.Addr
	resolutionClass   auditProviderResolutionClass
	nextIP            atomic.Uint64
	client            *http.Client
	transport         *http.Transport
	semaphore         chan struct{}
	acceptDone        chan struct{}
	done              chan struct{}
	acceptLoops       sync.WaitGroup
	workers           sync.WaitGroup
	activeMu          sync.Mutex
	active            map[net.Conn]struct{}
	shutdownOnce      sync.Once
	closeOnce         sync.Once
	closeErr          error
	rejectionDiagMu   sync.Mutex
	rejectionDiag     string
}

type auditGatewayRequest struct {
	headers http.Header
	body    []byte
}

func startAuditHTTPGateway(parent context.Context, provider string, endpoint executionPolicyEndpoint, total time.Duration, dependencies auditBrokerDependencies) (*auditHTTPGateway, error) {
	path, err := auditProviderGatewayPath(provider, endpoint)
	if parent == nil || parent.Err() != nil || err != nil || total <= 0 || total > maxAuditBrokerLifetime {
		return nil, authenticationError("audit HTTP gateway configuration")
	}
	lookup := dependencies.LookupIPAddr
	dial := dependencies.DialContext
	if lookup == nil && dial == nil && dependencies.TLSRootCAs == nil {
		lookup = net.DefaultResolver.LookupIPAddr
		dialer := &net.Dialer{Timeout: auditBrokerConnectTimeout, KeepAlive: -1}
		dial = dialer.DialContext
	} else if lookup == nil || dial == nil {
		return nil, authenticationError("partial audit HTTP gateway dependency injection")
	}
	resolution, err := resolveAndPinAuditProviderAddresses(parent, provider, endpoint, lookup)
	if err != nil {
		return nil, err
	}
	listen := dependencies.ListenContext
	if listen == nil {
		listenConfig := &net.ListenConfig{}
		listen = listenConfig.Listen
	}
	ipv4Listener, err := listen(parent, "tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, authenticationError("listen for audit HTTP gateway IPv4")
	}
	port, err := validateAuditGatewayListener(ipv4Listener, net.ParseIP("127.0.0.1"))
	if err != nil {
		_ = ipv4Listener.Close()
		return nil, err
	}
	ipv6Listener, err := listen(parent, "tcp6", net.JoinHostPort("::1", port))
	if err != nil {
		_ = ipv4Listener.Close()
		return nil, authenticationError("listen for audit HTTP gateway IPv6")
	}
	ipv6Port, err := validateAuditGatewayListener(ipv6Listener, net.ParseIP("::1"))
	if err != nil || ipv6Port != port {
		_ = ipv6Listener.Close()
		_ = ipv4Listener.Close()
		return nil, authenticationError("bind exact audit HTTP gateway IPv6 port")
	}
	gatewayContext, cancel := context.WithTimeout(parent, total)
	gateway := &auditHTTPGateway{
		provider:          provider,
		endpoint:          endpoint,
		upstreamAuthority: net.JoinHostPort(endpoint.Hostname, strconv.Itoa(endpoint.Port)),
		path:              path,
		address:           net.JoinHostPort("127.0.0.1", port),
		ipv4Listener:      ipv4Listener,
		ipv6Listener:      ipv6Listener,
		context:           gatewayContext,
		cancel:            cancel,
		deadline:          time.Now().Add(total),
		dialContext:       dial,
		pinnedIPs:         resolution.pinnedIPs,
		resolutionClass:   resolution.class,
		semaphore:         make(chan struct{}, maxAuditBrokerConnections),
		acceptDone:        make(chan struct{}),
		done:              make(chan struct{}),
		active:            make(map[net.Conn]struct{}),
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: endpoint.Hostname,
		RootCAs:    dependencies.TLSRootCAs,
	}
	gateway.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           gateway.dialPinnedEndpoint,
		TLSClientConfig:       tlsConfig,
		ForceAttemptHTTP2:     false,
		DisableCompression:    true,
		MaxIdleConns:          maxAuditBrokerConnections,
		MaxIdleConnsPerHost:   maxAuditBrokerConnections,
		IdleConnTimeout:       auditBrokerIdleTimeout,
		TLSHandshakeTimeout:   auditBrokerConnectTimeout,
		ResponseHeaderTimeout: total,
	}
	gateway.client = &http.Client{
		Transport: gateway.transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return authenticationError("audit HTTP gateway redirect")
		},
	}
	gateway.acceptLoops.Add(2)
	go gateway.acceptLoop(gateway.ipv4Listener)
	go gateway.acceptLoop(gateway.ipv6Listener)
	go func() {
		gateway.acceptLoops.Wait()
		close(gateway.acceptDone)
	}()
	go func() {
		<-gatewayContext.Done()
		gateway.initiateShutdown()
	}()
	return gateway, nil
}

func validateAuditGatewayListener(listener net.Listener, expectedIP net.IP) (string, error) {
	if listener == nil || expectedIP == nil {
		return "", authenticationError("audit HTTP gateway listener")
	}
	host, port, err := net.SplitHostPort(listener.Addr().String())
	parsedPort, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || parsedPort < 1 || parsedPort > 65535 {
		return "", authenticationError("audit HTTP gateway listener address")
	}
	parsedHost := net.ParseIP(host)
	if parsedHost == nil || !parsedHost.Equal(expectedIP) {
		return "", authenticationError("exact audit HTTP gateway listener address")
	}
	return strconv.Itoa(parsedPort), nil
}

func auditProviderGatewayPath(provider string, endpoint executionPolicyEndpoint) (string, error) {
	if !validAuditProviderEndpoint(provider, endpoint) {
		return "", authenticationError("audit provider gateway route")
	}
	switch provider {
	case "custom:sudo":
		return "/v1/responses", nil
	case "anthropic":
		return "/v1/messages", nil
	default:
		return "", authenticationError("audit provider gateway route")
	}
}

func validAuditProviderEndpointForAnyRoute(endpoint executionPolicyEndpoint) bool {
	if endpoint.Port != 443 {
		return false
	}
	for _, allowed := range auditProviderEndpointAllowlist {
		if endpoint == allowed {
			return true
		}
	}
	return false
}

func resolveAndPinAuditProviderAddresses(parent context.Context, provider string, endpoint executionPolicyEndpoint, lookup func(context.Context, string) ([]net.IPAddr, error)) (auditProviderResolution, error) {
	if parent == nil || parent.Err() != nil || lookup == nil || !validAuditProviderEndpoint(provider, endpoint) {
		return auditProviderResolution{}, authenticationError("audit provider resolution route")
	}
	resolveContext, cancelResolve := context.WithTimeout(parent, auditBrokerConnectTimeout)
	defer cancelResolve()
	addresses, err := lookup(resolveContext, endpoint.Hostname)
	if err != nil {
		return auditProviderResolution{}, authenticationError("resolve audit provider endpoint")
	}
	return validateAndPinAuditBrokerAddresses(provider, endpoint, addresses)
}

func preflightAuditProviderResolution(parent context.Context, provider string, endpoint executionPolicyEndpoint, lookup func(context.Context, string) ([]net.IPAddr, error)) (auditProviderResolutionClass, error) {
	resolution, err := resolveAndPinAuditProviderAddresses(parent, provider, endpoint, lookup)
	if err != nil {
		return auditProviderResolutionInvalid, err
	}
	return resolution.class, nil
}

func validateAndPinAuditBrokerAddresses(provider string, endpoint executionPolicyEndpoint, addresses []net.IPAddr) (auditProviderResolution, error) {
	if !validAuditProviderEndpoint(provider, endpoint) {
		return auditProviderResolution{}, authenticationError("audit provider resolution route")
	}
	if len(addresses) == 0 || len(addresses) > maxAuditBrokerResolvedIPs {
		return auditProviderResolution{}, authenticationError("bounded audit provider resolution")
	}
	unique := make(map[netip.Addr]struct{}, len(addresses))
	resolutionClass := auditProviderResolutionInvalid
	for _, resolved := range addresses {
		address, ok := netip.AddrFromSlice(resolved.IP)
		if !ok || resolved.Zone != "" {
			return auditProviderResolution{}, authenticationError("invalid audit provider resolution")
		}
		address = address.Unmap()
		addressClass := classifyAuditProviderAddress(provider, endpoint, address)
		if addressClass == auditProviderResolutionInvalid {
			return auditProviderResolution{}, authenticationError("disallowed audit provider resolution")
		}
		if resolutionClass == auditProviderResolutionInvalid {
			resolutionClass = addressClass
		} else if resolutionClass != addressClass {
			return auditProviderResolution{}, authenticationError("mixed audit provider resolution classes")
		}
		if _, duplicate := unique[address]; duplicate {
			return auditProviderResolution{}, authenticationError("duplicate audit provider resolution")
		}
		unique[address] = struct{}{}
	}
	pinned := make([]netip.Addr, 0, len(unique))
	for address := range unique {
		pinned = append(pinned, address)
	}
	sort.Slice(pinned, func(left, right int) bool { return pinned[left].Compare(pinned[right]) < 0 })
	if len(pinned) == 0 || resolutionClass == auditProviderResolutionInvalid {
		return auditProviderResolution{}, authenticationError("empty audit provider resolution")
	}
	return auditProviderResolution{class: resolutionClass, pinnedIPs: pinned}, nil
}

func classifyAuditProviderAddress(provider string, endpoint executionPolicyEndpoint, address netip.Addr) auditProviderResolutionClass {
	if publicAuditBrokerAddress(address) {
		return auditProviderResolutionPublic
	}
	if transparentFakeIPAuditBrokerAddress(provider, endpoint, address) {
		return auditProviderResolutionTransparentFakeIP
	}
	return auditProviderResolutionInvalid
}

func transparentFakeIPAuditBrokerAddress(provider string, endpoint executionPolicyEndpoint, address netip.Addr) bool {
	return auditPlatformSupported(runtime.GOOS) && validAuditProviderEndpoint(provider, endpoint) && address.Is4() && auditBrokerTransparentFakeIPPrefix.Contains(address)
}

func publicAuditBrokerAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range auditBrokerReservedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

var (
	auditBrokerTransparentFakeIPPrefix = netip.MustParsePrefix("198.18.0.0/15")
	auditBrokerReservedPrefixes        = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		auditBrokerTransparentFakeIPPrefix,
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
)

func (gateway *auditHTTPGateway) Address() string {
	if gateway == nil {
		return ""
	}
	return gateway.address
}

func (gateway *auditHTTPGateway) Done() <-chan struct{} {
	if gateway == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return gateway.done
}

func (gateway *auditHTTPGateway) acceptLoop(listener net.Listener) {
	defer gateway.acceptLoops.Done()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if gateway.context.Err() == nil {
				gateway.initiateShutdown()
			}
			return
		}
		select {
		case gateway.semaphore <- struct{}{}:
			gateway.workers.Add(1)
			go gateway.handle(connection)
		default:
			_ = connection.Close()
		}
	}
}

func (gateway *auditHTTPGateway) handle(client net.Conn) {
	defer gateway.workers.Done()
	defer func() { <-gateway.semaphore }()
	if !gateway.register(client) {
		_ = client.Close()
		return
	}
	defer gateway.closeAndUnregister(client)
	request, err := readAuditGatewayRequest(client, gateway.deadline, gateway.path, gateway.Address())
	if err != nil {
		gateway.captureRejection(err)
		writeAuditGatewayRejection(client)
		return
	}
	defer zeroBytes(request.body)
	upstreamURL := "https://" + gateway.endpoint.Hostname + gateway.path
	upstreamRequest, err := http.NewRequestWithContext(gateway.context, http.MethodPost, upstreamURL, bytes.NewReader(request.body))
	if err != nil {
		writeAuditGatewayRejection(client)
		return
	}
	upstreamRequest.Host = gateway.endpoint.Hostname
	copyAuditEndToEndHeaders(upstreamRequest.Header, request.headers)
	response, err := gateway.client.Do(upstreamRequest)
	if err != nil {
		writeAuditGatewayUpstreamFailure(client)
		return
	}
	defer response.Body.Close()
	gateway.writeResponse(client, response)
}

func readAuditGatewayRequest(connection net.Conn, totalDeadline time.Time, expectedPath, listenerAuthority string) (auditGatewayRequest, error) {
	if err := setAuditBrokerDeadline(connection, totalDeadline, auditBrokerHeaderTimeout); err != nil {
		return auditGatewayRequest{}, err
	}
	reader := bufio.NewReaderSize(connection, maxAuditBrokerHeaderBytes+1)
	header, err := readAuditGatewayHeader(reader)
	if err != nil {
		return auditGatewayRequest{}, err
	}
	headers, contentLength, err := validateAuditGatewayHeader(header, expectedPath, listenerAuthority)
	if err != nil {
		return auditGatewayRequest{}, err
	}
	if err := setAuditBrokerDeadline(connection, totalDeadline, auditBrokerBodyTimeout); err != nil {
		return auditGatewayRequest{}, err
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		zeroBytes(body)
		return auditGatewayRequest{}, err
	}
	if !jsonObjectOrArray(body) {
		zeroBytes(body)
		return auditGatewayRequest{}, authenticationError("audit gateway JSON request body")
	}
	if reader.Buffered() != 0 {
		zeroBytes(body)
		return auditGatewayRequest{}, authenticationError("audit gateway pipelined request")
	}
	if err := connection.SetReadDeadline(time.Now().Add(auditBrokerTrailingProbeTimeout)); err != nil {
		zeroBytes(body)
		return auditGatewayRequest{}, err
	}
	var trailing [1]byte
	read, trailingErr := connection.Read(trailing[:])
	if read != 0 || trailingErr == nil {
		zeroBytes(body)
		return auditGatewayRequest{}, authenticationError("audit gateway trailing request bytes")
	}
	if networkError, ok := trailingErr.(net.Error); !ok || !networkError.Timeout() {
		if !errors.Is(trailingErr, io.EOF) {
			zeroBytes(body)
			return auditGatewayRequest{}, authenticationError("audit gateway trailing request state")
		}
	}
	return auditGatewayRequest{headers: headers, body: body}, nil
}

func readAuditGatewayHeader(reader *bufio.Reader) ([]byte, error) {
	if reader == nil {
		return nil, ErrProtocol
	}
	header := make([]byte, 0, 1024)
	for len(header) <= maxAuditBrokerHeaderBytes {
		value, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		header = append(header, value)
		if len(header) >= 4 && bytes.Equal(header[len(header)-4:], []byte("\r\n\r\n")) {
			return header, nil
		}
	}
	return nil, ErrLimit
}

func validateAuditGatewayHeader(raw []byte, expectedPath, listenerAuthority string) (http.Header, int, error) {
	if len(raw) < 4 || len(raw) > maxAuditBrokerHeaderBytes || !bytes.HasSuffix(raw, []byte("\r\n\r\n")) || bytes.IndexByte(raw, 0) >= 0 {
		return nil, 0, authenticationError("bounded audit gateway header")
	}
	for _, value := range raw {
		if value < 0x20 && value != '\r' && value != '\n' || value == 0x7f {
			return nil, 0, authenticationError("audit gateway header grammar")
		}
	}
	lines := strings.Split(string(raw[:len(raw)-4]), "\r\n")
	if len(lines) < 2 || len(lines)-1 > maxAuditBrokerHeaders || lines[0] != "POST "+expectedPath+" HTTP/1.1" {
		return nil, 0, authenticationError("exact audit gateway request target")
	}
	headers := make(http.Header, len(lines)-1)
	seen := make(map[string]struct{}, len(lines)-1)
	for _, line := range lines[1:] {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			return nil, 0, authenticationError("audit gateway header folding")
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 || colon+2 > len(line) || line[colon+1] != ' ' || !validAuditHTTPHeaderName(line[:colon]) {
			return nil, 0, authenticationError("audit gateway header grammar")
		}
		name := strings.ToLower(line[:colon])
		if _, duplicate := seen[name]; duplicate {
			return nil, 0, authenticationError("duplicate audit gateway header")
		}
		seen[name] = struct{}{}
		value := line[colon+2:]
		if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
			return nil, 0, authenticationError("audit gateway header value")
		}
		if auditHopByHopHeader(name) {
			continue
		}
		headers.Set(http.CanonicalHeaderKey(name), value)
	}
	if headers.Get("Host") != listenerAuthority {
		return nil, 0, authenticationError("audit gateway listener authority")
	}
	lengthValue := headers.Get("Content-Length")
	length, err := strconv.ParseInt(lengthValue, 10, 64)
	if err != nil || strconv.FormatInt(length, 10) != lengthValue || length < 1 || length > maxAuditBrokerBodyBytes {
		return nil, 0, authenticationError("bounded audit gateway content length")
	}
	contentType := headers.Get("Content-Type")
	if contentType != "application/json" {
		return nil, 0, authenticationError("audit gateway content type")
	}
	return headers, int(length), nil
}

func validAuditHTTPHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, value := range []byte(name) {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(value)) {
			continue
		}
		return false
	}
	return true
}

func auditHopByHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "proxy-connection", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func jsonObjectOrArray(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) >= 2 && (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid(trimmed)
}

func copyAuditEndToEndHeaders(destination, source http.Header) {
	for name, values := range source {
		lower := strings.ToLower(name)
		if lower == "host" || lower == "content-length" || auditHopByHopHeader(lower) {
			continue
		}
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func writeAuditGatewayRejection(connection net.Conn) {
	_ = connection.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = io.WriteString(connection, "HTTP/1.1 400 Bad Request\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
}

func (gateway *auditHTTPGateway) captureRejection(err error) {
	gateway.rejectionDiagMu.Lock()
	if gateway.rejectionDiag == "" {
		gateway.rejectionDiag = err.Error() + " [path=" + gateway.path + " authority=" + gateway.address + "]"
	}
	gateway.rejectionDiagMu.Unlock()
}

func (gateway *auditHTTPGateway) RejectionDiagnostic() string {
	gateway.rejectionDiagMu.Lock()
	defer gateway.rejectionDiagMu.Unlock()
	return gateway.rejectionDiag
}

func writeAuditGatewayUpstreamFailure(connection net.Conn) {
	_ = connection.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = io.WriteString(connection, "HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
}

func (gateway *auditHTTPGateway) writeResponse(client net.Conn, response *http.Response) {
	if response == nil || response.StatusCode < 100 || response.StatusCode > 599 {
		writeAuditGatewayUpstreamFailure(client)
		return
	}
	if err := setAuditBrokerDeadline(client, gateway.deadline, auditBrokerIdleTimeout); err != nil {
		return
	}
	var header strings.Builder
	header.WriteString("HTTP/1.1 ")
	header.WriteString(strconv.Itoa(response.StatusCode))
	header.WriteByte(' ')
	header.WriteString(http.StatusText(response.StatusCode))
	header.WriteString("\r\n")
	for name, values := range response.Header {
		lower := strings.ToLower(name)
		if lower == "content-length" || auditHopByHopHeader(lower) || !validAuditHTTPHeaderName(name) {
			continue
		}
		for _, value := range values {
			if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
				continue
			}
			header.WriteString(http.CanonicalHeaderKey(name))
			header.WriteString(": ")
			header.WriteString(value)
			header.WriteString("\r\n")
			if header.Len() > maxAuditBrokerHeaderBytes {
				writeAuditGatewayUpstreamFailure(client)
				return
			}
		}
	}
	header.WriteString("Transfer-Encoding: chunked\r\nConnection: close\r\n\r\n")
	if _, err := io.WriteString(client, header.String()); err != nil {
		return
	}
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			total += int64(read)
			if total > maxAuditBrokerResponseBytes {
				return
			}
			if err := setAuditBrokerDeadline(client, gateway.deadline, auditBrokerIdleTimeout); err != nil {
				return
			}
			length := strconv.AppendInt(nil, int64(read), 16)
			if _, err := client.Write(append(length, '\r', '\n')); err != nil {
				return
			}
			if _, err := client.Write(buffer[:read]); err != nil {
				return
			}
			if _, err := io.WriteString(client, "\r\n"); err != nil {
				return
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				_, _ = io.WriteString(client, "0\r\n\r\n")
			}
			return
		}
	}
}

func (gateway *auditHTTPGateway) dialPinnedEndpoint(ctx context.Context, network, authority string) (net.Conn, error) {
	if gateway == nil || ctx == nil || network != "tcp" && network != "tcp4" && network != "tcp6" || authority != gateway.upstreamAuthority || len(gateway.pinnedIPs) == 0 {
		return nil, authenticationError("pinned audit gateway dial target")
	}
	connectContext, cancel := context.WithTimeout(ctx, auditBrokerConnectTimeout)
	defer cancel()
	start := int(gateway.nextIP.Add(1)-1) % len(gateway.pinnedIPs)
	var last error
	for offset := range len(gateway.pinnedIPs) {
		address := gateway.pinnedIPs[(start+offset)%len(gateway.pinnedIPs)]
		connection, err := gateway.dialContext(connectContext, "tcp", net.JoinHostPort(address.String(), strconv.Itoa(gateway.endpoint.Port)))
		if err == nil {
			return connection, nil
		}
		last = err
		if connectContext.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("dial pinned audit provider: %w", last)
}

func setAuditBrokerDeadline(connection net.Conn, totalDeadline time.Time, interval time.Duration) error {
	deadline := time.Now().Add(interval)
	if totalDeadline.Before(deadline) {
		deadline = totalDeadline
	}
	return connection.SetDeadline(deadline)
}

func (gateway *auditHTTPGateway) register(connection net.Conn) bool {
	gateway.activeMu.Lock()
	defer gateway.activeMu.Unlock()
	if gateway.context.Err() != nil {
		return false
	}
	gateway.active[connection] = struct{}{}
	return true
}

func (gateway *auditHTTPGateway) closeAndUnregister(connection net.Conn) {
	_ = connection.Close()
	gateway.activeMu.Lock()
	delete(gateway.active, connection)
	gateway.activeMu.Unlock()
}

func (gateway *auditHTTPGateway) activeConnectionCount() int {
	if gateway == nil {
		return 0
	}
	gateway.activeMu.Lock()
	defer gateway.activeMu.Unlock()
	return len(gateway.active)
}

func (gateway *auditHTTPGateway) initiateShutdown() {
	if gateway == nil {
		return
	}
	gateway.shutdownOnce.Do(func() {
		gateway.cancel()
		_ = gateway.ipv4Listener.Close()
		_ = gateway.ipv6Listener.Close()
		if gateway.transport != nil {
			gateway.transport.CloseIdleConnections()
		}
		gateway.activeMu.Lock()
		connections := make([]net.Conn, 0, len(gateway.active))
		for connection := range gateway.active {
			connections = append(connections, connection)
		}
		gateway.activeMu.Unlock()
		for _, connection := range connections {
			_ = connection.Close()
		}
	})
}

func (gateway *auditHTTPGateway) Close() error {
	if gateway == nil {
		return nil
	}
	gateway.closeOnce.Do(func() {
		gateway.initiateShutdown()
		acceptTimer := time.NewTimer(auditBrokerCloseTimeout)
		select {
		case <-gateway.acceptDone:
			if !acceptTimer.Stop() {
				<-acceptTimer.C
			}
		case <-acceptTimer.C:
			gateway.closeErr = authenticationError("reap audit HTTP gateway accept loop")
		}
		workersDone := make(chan struct{})
		go func() {
			gateway.workers.Wait()
			close(workersDone)
		}()
		workerTimer := time.NewTimer(auditBrokerCloseTimeout)
		select {
		case <-workersDone:
			if !workerTimer.Stop() {
				<-workerTimer.C
			}
		case <-workerTimer.C:
			if gateway.closeErr == nil {
				gateway.closeErr = authenticationError("reap audit HTTP gateway workers")
			}
		}
		close(gateway.done)
	})
	select {
	case <-gateway.done:
		return gateway.closeErr
	case <-time.After(2 * auditBrokerCloseTimeout):
		return errors.New("audit HTTP gateway close did not complete")
	}
}
