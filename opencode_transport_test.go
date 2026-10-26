package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestOpenCodeProbeDoesNotUseHostTransport(t *testing.T) {
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	hostCalls := 0
	hostCall = func(_ string, _ any, result any) error {
		hostCalls++
		*result.(*pluginapi.HTTPResponse) = eventStreamResponse()
		return nil
	}
	response := callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.ai/inference/openai/v1","model":"mimo-v2.6-flash-free","proxy_url":"unsupported://proxy.invalid"}`))
	var verdict struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(response.Body, &verdict); err != nil {
		t.Fatal(err)
	}
	if hostCalls != 0 || verdict.OK {
		t.Fatalf("host calls = %d, verdict = %s; OpenCode must use its own TLS transport and fail closed on an invalid proxy", hostCalls, response.Body)
	}
}

func TestOpenCodeTransportVerifiesTLSAndReadsCompleteStream(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Proto != "HTTP/1.1" || r.Header.Get("Authorization") != "Bearer local-key" || string(body) != `{"stream":true}` {
			t.Errorf("request protocol=%q auth=%q body=%q", r.Proto, r.Header.Get("Authorization"), body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	transport, err := newOpenCodeTransport("direct")
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	request, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewBufferString(`{"stream":true}`))
	request.Header.Set("Authorization", "Bearer local-key")
	response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || !eventStreamCarriesCompletion(body) {
		t.Fatalf("status=%d body=%q err=%v", response.StatusCode, body, err)
	}

	untrusted, err := newOpenCodeTransport("direct")
	if err != nil {
		t.Fatal(err)
	}
	defer untrusted.CloseIdleConnections()
	_, err = (&http.Client{Transport: untrusted, Timeout: 3 * time.Second}).Get(server.URL)
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("untrusted certificate error = %v", err)
	}
}

func TestOpenCodeTransportCancelsTLSHandshake(t *testing.T) {
	stall := httptest.NewUnstartedServer(nil)
	defer stall.Close()
	transport, err := newOpenCodeTransport("direct")
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+stall.Listener.Addr().String(), nil)
	started := time.Now()
	_, err = (&http.Client{Transport: transport}).Do(request)
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("cancel error=%v duration=%s", err, time.Since(started))
	}
}

func TestOpenCodeTransportUsesAuthenticatedHTTPProxy(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "proxied")
	}))
	defer upstream.Close()
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Errorf("proxy method=%q authorization=%q", r.Method, r.Header.Get("Proxy-Authorization"))
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		remote, err := net.DialTimeout("tcp", r.Host, time.Second)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		local, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = remote.Close()
			t.Error(err)
			return
		}
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
		_ = buffered.Flush()
		go func() { _, _ = io.Copy(remote, local); _ = remote.Close() }()
		_, _ = io.Copy(local, remote)
		_ = local.Close()
	}))
	defer proxyServer.Close()
	proxyURL := strings.Replace(proxyServer.URL, "http://", "http://user:pass@", 1)
	transport, err := newOpenCodeTransport(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "proxied" {
		t.Fatalf("proxy body=%q err=%v", body, err)
	}
}
