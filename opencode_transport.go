package main

import (
	"bytes"
	"context"
	stdtls "crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	tls "github.com/refraction-networking/utls"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
	"golang.org/x/net/proxy"
)

var (
	openCodeContext, cancelOpenCodeRequests = context.WithCancel(context.Background())
	openCodeHTTPDo                          = doOpenCodeHTTPRequest
)

func doOpenCodeHTTPRequest(ctx context.Context, request hostHTTPRequest, proxyURL string) (pluginapi.HTTPResponse, error) {
	var result pluginapi.HTTPResponse
	endpoint, err := url.Parse(request.URL)
	if err != nil || !strings.EqualFold(endpoint.Scheme, "https") || !strings.EqualFold(endpoint.Hostname(), openCodeHost) || endpoint.User != nil {
		return result, errors.New("invalid OpenCode endpoint")
	}
	if proxyURL == "" {
		selected, err := http.ProxyFromEnvironment(&http.Request{URL: endpoint})
		if err != nil {
			return result, err
		}
		if selected != nil {
			proxyURL = selected.String()
		}
	}
	transport, err := newOpenCodeTransport(proxyURL)
	if err != nil {
		return result, err
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	upstream, err := http.NewRequestWithContext(ctx, request.Method, request.URL, bytes.NewReader(request.Body))
	if err != nil {
		return result, err
	}
	upstream.Header = request.Headers.Clone()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(upstream)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	const maxBody = 4 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return result, err
	}
	if len(body) > maxBody {
		return result, errors.New("OpenCode probe response too large")
	}
	return pluginapi.HTTPResponse{StatusCode: response.StatusCode, Headers: response.Header.Clone(), Body: body}, nil
}

func newOpenCodeTransport(proxyURL string) (*http.Transport, error) {
	dialer, mode, err := proxyutil.BuildDialer(proxyURL)
	if err != nil {
		return nil, err
	}
	var forward proxy.ContextDialer = &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	if dialer != nil {
		var ok bool
		forward, ok = dialer.(proxy.ContextDialer)
		if !ok {
			return nil, errors.New("OpenCode proxy does not support cancellation")
		}
	}
	transport := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 30 * time.Second}
	transport.DialContext = forward.DialContext
	// Bun 1.4.4 advertises only HTTP/1.1; keep net/http from adding HTTP/2.
	transport.TLSNextProto = make(map[string]func(string, *stdtls.Conn) http.RoundTripper)
	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		// The verified OpenCode IPv6 route rejects requests; proxies keep their own routing.
		if mode != proxyutil.ModeProxy && network == "tcp" && strings.EqualFold(host, openCodeHost) {
			network = "tcp4"
		}
		conn, err := transport.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		config := &tls.Config{ServerName: host}
		if transport.TLSClientConfig != nil {
			config.RootCAs = transport.TLSClientConfig.RootCAs
		}
		tlsConn := tls.UClient(conn, config, tls.HelloCustom)
		if err := tlsConn.ApplyPreset(openCodeClientHelloSpec()); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("OpenCode TLS profile: %w", err)
		}
		handshakeCtx, cancel := context.WithTimeout(ctx, transport.TLSHandshakeTimeout)
		defer cancel()
		if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("OpenCode TLS handshake: %w", err)
		}
		return tlsConn, nil
	}
	return transport, nil
}

// Each connection needs fresh key shares; the profile matches the verified native client.
func openCodeClientHelloSpec() *tls.ClientHelloSpec {
	return &tls.ClientHelloSpec{
		CipherSuites:       []uint16{0x1301, 0x1302, 0x1303, 0xc02b, 0xc02f, 0xc02c, 0xc030, 0xcca9, 0xcca8, 0xc009, 0xc013, 0xc00a, 0xc014, 0x009c, 0x009d, 0x002f, 0x0035},
		CompressionMethods: []uint8{0},
		Extensions: []tls.TLSExtension{
			&tls.SNIExtension{},
			&tls.ExtendedMasterSecretExtension{},
			&tls.RenegotiationInfoExtension{Renegotiation: tls.RenegotiateOnceAsClient},
			&tls.SupportedCurvesExtension{Curves: []tls.CurveID{tls.X25519MLKEM768, tls.X25519, tls.CurveP256, tls.CurveP384}},
			&tls.SupportedPointsExtension{SupportedPoints: []uint8{0}},
			&tls.SessionTicketExtension{},
			&tls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}},
			&tls.StatusRequestExtension{},
			&tls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []tls.SignatureScheme{
				tls.ECDSAWithP256AndSHA256, tls.PSSWithSHA256, tls.PKCS1WithSHA256,
				tls.ECDSAWithP384AndSHA384, tls.PSSWithSHA384, tls.PKCS1WithSHA384,
				tls.PSSWithSHA512, tls.PKCS1WithSHA512, tls.Ed25519, tls.ECDSAWithP521AndSHA512, tls.PKCS1WithSHA1,
			}},
			&tls.SCTExtension{},
			&tls.KeyShareExtension{KeyShares: []tls.KeyShare{{Group: tls.X25519MLKEM768}, {Group: tls.X25519}}},
			&tls.PSKKeyExchangeModesExtension{Modes: []uint8{tls.PskModeDHE}},
			&tls.SupportedVersionsExtension{Versions: []uint16{tls.VersionTLS13, tls.VersionTLS12}},
		},
	}
}
