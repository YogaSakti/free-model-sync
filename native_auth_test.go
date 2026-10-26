package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const nativeFixture = `{"type":"opencode-enhancer","api_key":"private-test-key","base_url":"https://opencode.ai/inference/openai/v1","proxy_url":"socks5://credential:1080","headers":{"X-Private":"private-header"},"weight":7,"metadata":{"keep":true},"models":[{"name":"manual-paid","alias":"manual","custom":"keep"},{"name":"old-free","alias":"old"},{"name":"current-free","alias":"my-alias","custom":"keep-too"},{"name":"zero-price","alias":"zero"}]}`

func stubNativeAuth(t *testing.T, raw string) *json.RawMessage {
	t.Helper()
	previous := hostCall
	previousHTTP := openCodeHTTPDo
	t.Cleanup(func() { hostCall = previous; openCodeHTTPDo = previousHTTP })
	openCodeHTTPDo = func(_ context.Context, _ hostHTTPRequest, _ string) (pluginapi.HTTPResponse, error) {
		return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"current-free"},{"id":"new-free"}]}`)}, nil
	}
	var saved json.RawMessage
	hostCall = func(method string, request any, result any) error {
		var payload any
		switch method {
		case pluginabi.MethodHostAuthList:
			payload = map[string]any{"files": []any{
				map[string]any{"auth_index": "native-1", "name": "native.json", "type": "opencode-enhancer", "provider": "opencode-enhancer", "source": "file", "path": "/auth/native.json"},
				map[string]any{"auth_index": "other-1", "name": "other.json", "type": "openai", "source": "file", "path": "/auth/other.json"},
				map[string]any{"auth_index": "memory-1", "name": "runtime", "type": "opencode-enhancer", "runtime_only": true},
			}}
		case pluginabi.MethodHostAuthGet:
			data, _ := json.Marshal(request)
			if !bytes.Contains(data, []byte(`"auth_index":"native-1"`)) {
				return fmt.Errorf("unexpected auth index")
			}
			payload = map[string]any{"auth_index": "native-1", "name": "native.json", "path": "/auth/native.json", "json": json.RawMessage(raw)}
		case pluginabi.MethodHostHTTPDo:
			payload = pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"current-free"},{"id":"new-free"}]}`)}
		case pluginabi.MethodHostAuthSave:
			data, _ := json.Marshal(request)
			var req pluginapi.HostAuthSaveRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return err
			}
			if req.Name != "native.json" {
				t.Fatalf("save name = %q", req.Name)
			}
			saved = req.JSON
			payload = pluginapi.HostAuthSaveResponse{Name: req.Name, Path: "/auth/native.json"}
		default:
			return fmt.Errorf("unexpected callback %s", method)
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, result)
	}
	return &saved
}

func TestNativeListKeepsCredentialsServerSide(t *testing.T) {
	stubNativeAuth(t, nativeFixture)
	response := listNativeAuth()
	if response.StatusCode != 200 {
		t.Fatalf("status %d: %s", response.StatusCode, response.Body)
	}
	var body struct {
		Providers []struct {
			ID, AuthIndex, Name string
			Models              []modelConfig
		} `json:"providers"`
	}
	if err := json.Unmarshal(response.Body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Providers) != 1 || body.Providers[0].ID != "auth:native.json" || body.Providers[0].Name != "native.json" || len(body.Providers[0].Models) != 4 {
		t.Fatalf("body = %s", response.Body)
	}
	for _, forbidden := range []string{"private-test-key", "private-header", "credential:1080", "/auth/", "metadata"} {
		if bytes.Contains(response.Body, []byte(forbidden)) {
			t.Fatalf("response leaked %s", forbidden)
		}
	}
}

func TestNativeSavePreservesCredentialsManualModelsAndUnknownFields(t *testing.T) {
	saved := stubNativeAuth(t, nativeFixture)
	response := saveNativeAuth("callback", []byte(`{"auth_index":"native-1","managed":["zero-price"],"selected":["current-free","new-free"]}`))
	if response.StatusCode != 200 {
		t.Fatalf("status %d: %s", response.StatusCode, response.Body)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(*saved, &body); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"api_key":"private-test-key"`, `"weight":7`, `"metadata":{"keep":true}`, `"proxy_url":"socks5://credential:1080"`, `"custom":"keep-too"`, `"alias":"my-alias"`, `"custom":"keep"`} {
		if !bytes.Contains(*saved, []byte(expected)) {
			t.Fatalf("saved JSON missing %s: %s", expected, *saved)
		}
	}
	var models []modelConfig
	if err := json.Unmarshal(body["models"], &models); err != nil {
		t.Fatal(err)
	}
	if len(models) != 3 || models[0].Name != "manual-paid" || models[1].Name != "current-free" || models[2].Name != "new-free" {
		t.Fatalf("models = %#v", models)
	}
	if bytes.Contains(response.Body, []byte("private-test-key")) {
		t.Fatal("save response leaked key")
	}
}

func TestNativeSavePreservesKeyRotatedDuringCatalogFetch(t *testing.T) {
	saved := stubNativeAuth(t, nativeFixture)
	previous := hostCall
	reads := 0
	hostCall = func(method string, request any, result any) error {
		if err := previous(method, request, result); err != nil {
			return err
		}
		if method == pluginabi.MethodHostAuthGet {
			reads++
			if reads > 1 {
				file := result.(*nativeAuthFile)
				file.JSON = json.RawMessage(strings.ReplaceAll(string(file.JSON), "private-test-key", "rotated-key"))
			}
		}
		return nil
	}
	response := saveNativeAuth("callback", []byte(`{"auth_index":"native-1","selected":["new-free"]}`))
	if response.StatusCode != 200 || !bytes.Contains(*saved, []byte(`"api_key":"rotated-key"`)) {
		t.Fatalf("save replaced newly rotated key: status %d", response.StatusCode)
	}
}

func TestNativeSaveRejectsModelsOutsideFreeCatalogWithoutWriting(t *testing.T) {
	saved := stubNativeAuth(t, nativeFixture)
	response := saveNativeAuth("callback", []byte(`{"auth_index":"native-1","selected":["paid-injected"]}`))
	if response.StatusCode != 400 || len(*saved) != 0 {
		t.Fatalf("status %d, saved=%s", response.StatusCode, *saved)
	}
}

func TestNativeEndpointsRejectOtherProviderBeforeReadingItsKey(t *testing.T) {
	stubNativeAuth(t, nativeFixture)
	response := planNativeAuth("callback", []byte(`{"auth_index":"other-1"}`))
	if response.StatusCode != 404 {
		t.Fatalf("status %d: %s", response.StatusCode, response.Body)
	}
}

func TestNativeAuthRejectsNestedSaveTargets(t *testing.T) {
	for _, name := range []string{"team/native.json", `team\native.json`, " native.json"} {
		t.Run(name, func(t *testing.T) {
			saved := stubNativeAuth(t, nativeFixture)
			previous := hostCall
			hostCall = func(method string, request any, result any) error {
				if err := previous(method, request, result); err != nil {
					return err
				}
				if method == pluginabi.MethodHostAuthList {
					result.(*nativeAuthList).Files[0].Name = name
				}
				return nil
			}
			response := saveNativeAuth("callback", []byte(`{"auth_index":"native-1","selected":["new-free"]}`))
			if response.StatusCode != 404 || len(*saved) != 0 {
				t.Fatalf("unsafe target accepted: status %d, saved=%v", response.StatusCode, len(*saved) != 0)
			}
		})
	}
}

func TestNativeAuthUsesExecutorBaseURLRules(t *testing.T) {
	for _, test := range []struct {
		name, base string
		ok         bool
	}{
		{"default", "", true}, {"canonical", "https://opencode.ai/inference/openai/v1", true},
		{"legacy", "https://opencode.ai/inference/v1", true}, {"tls-port", "https://opencode.ai:443/inference/openai/v1", true},
		{"wrong-port", "https://opencode.ai:8443/inference/openai/v1", false}, {"obsolete", "https://opencode.ai/zen/v1", false},
		{"http", "http://opencode.ai/inference/openai/v1", false}, {"other-host", "https://other.opencode.ai/inference/openai/v1", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			stubNativeAuth(t, strings.Replace(nativeFixture, "https://opencode.ai/inference/openai/v1", test.base, 1))
			response := listNativeAuth()
			if (response.StatusCode == 200) != test.ok {
				t.Fatalf("status %d for %q: %s", response.StatusCode, test.base, response.Body)
			}
			if test.name == "default" && !bytes.Contains(response.Body, []byte(`"base-url":"https://opencode.ai/inference/openai/v1"`)) {
				t.Fatalf("default not normalized: %s", response.Body)
			}
		})
	}
}

func TestNativeProbeUsesServerCredentialAndCredentialProxy(t *testing.T) {
	stubNativeAuth(t, nativeFixture)
	previous := openCodeHTTPDo
	t.Cleanup(func() { openCodeHTTPDo = previous })
	openCodeHTTPDo = func(_ context.Context, request hostHTTPRequest, proxyURL string) (pluginapi.HTTPResponse, error) {
		if request.Headers.Get("Authorization") != "Bearer private-test-key" || proxyURL != "socks5://credential:1080" {
			t.Fatalf("credential or proxy selection incorrect")
		}
		if request.Method == http.MethodGet {
			return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"current-free"}]}`)}, nil
		}
		return pluginapi.HTTPResponse{StatusCode: 200, Headers: http.Header{"Content-Type": {"text/event-stream"}}, Body: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n")}, nil
	}
	response := testNativeAuth("callback", []byte(`{"auth_index":"native-1","model":"current-free","proxy_url":"socks5://global:1080","api_key":"browser-must-not-win"}`))
	if response.StatusCode != 200 || !bytes.Contains(response.Body, []byte(`"ok":true`)) {
		t.Fatalf("response %d: %s", response.StatusCode, response.Body)
	}
}

func TestNativeHeadersKeepCredentialAndFingerprintAuthoritative(t *testing.T) {
	raw := strings.Replace(nativeFixture, `"headers":{"X-Private":"private-header"}`, `"headers":{"X-Private":"private-header","authorization":"Bearer wrong-key","User-Agent":"wrong-agent","Accept":"text/plain","Content-Type":"text/plain","X-Opencode-Client":"wrong-client","X-Opencode-Project":"wrong-project","X-Opencode-Session":"wrong-session","X-Opencode-Session-Id":"wrong-session","X-Opencode-Request":"wrong-request","X-CPA-OpenCode-Execution-ID":"internal-id","Connection":"close"}`, 1)
	stubNativeAuth(t, raw)
	seenCatalog, seenProbe := false, false
	openCodeHTTPDo = func(_ context.Context, request hostHTTPRequest, _ string) (pluginapi.HTTPResponse, error) {
		if request.Headers.Get("Authorization") != "Bearer private-test-key" || request.Headers.Get("X-Private") != "private-header" {
			t.Fatalf("native credential/custom header precedence incorrect: %s", request.Method)
		}
		if request.Headers.Get("X-CPA-OpenCode-Execution-ID") != "" || request.Headers.Get("Connection") != "" {
			t.Fatal("native internal or hop header leaked")
		}
		if request.Method == http.MethodGet {
			seenCatalog = true
			return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"current-free"}]}`)}, nil
		}
		seenProbe = true
		for name, want := range map[string]string{"User-Agent": openCodeUserAgent, "Accept": openCodeAccept, "Content-Type": "application/json", "X-Opencode-Client": openCodeClient, "X-Opencode-Project": openCodeProject} {
			if request.Headers.Get(name) != want {
				t.Fatalf("native fingerprint header %s = %q", name, request.Headers.Get(name))
			}
		}
		session := request.Headers.Get("X-Opencode-Session")
		if !strings.HasPrefix(session, "ses_") || request.Headers.Get("X-Opencode-Session-Id") != session || !strings.HasPrefix(request.Headers.Get("X-Opencode-Request"), "msg_") {
			t.Fatal("native generated fingerprint was overridden")
		}
		return pluginapi.HTTPResponse{StatusCode: 200, Headers: http.Header{"Content-Type": {"text/event-stream"}}, Body: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n")}, nil
	}
	response := testNativeAuth("callback", []byte(`{"auth_index":"native-1","model":"current-free"}`))
	if response.StatusCode != 200 || !bytes.Contains(response.Body, []byte(`"ok":true`)) || !seenCatalog || !seenProbe {
		t.Fatalf("native test result: %d %s", response.StatusCode, response.Body)
	}
}

func TestNativePlanUsesCredentialProxy(t *testing.T) {
	stubNativeAuth(t, nativeFixture)
	previous := openCodeHTTPDo
	t.Cleanup(func() { openCodeHTTPDo = previous })
	called := false
	openCodeHTTPDo = func(_ context.Context, request hostHTTPRequest, proxyURL string) (pluginapi.HTTPResponse, error) {
		called = true
		if request.Method != http.MethodGet || proxyURL != "socks5://credential:1080" {
			t.Fatalf("catalog method/proxy = %s/%s", request.Method, proxyURL)
		}
		return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"new-free"}]}`)}, nil
	}
	response := planNativeAuth("callback", []byte(`{"auth_index":"native-1","proxy_url":"socks5://global:1080"}`))
	if response.StatusCode != 200 || !called {
		t.Fatalf("status %d, native transport called=%v", response.StatusCode, called)
	}
}

func TestNativePlanUsesGlobalProxyWhenCredentialHasNone(t *testing.T) {
	stubNativeAuth(t, strings.Replace(nativeFixture, `"proxy_url":"socks5://credential:1080",`, "", 1))
	openCodeHTTPDo = func(_ context.Context, _ hostHTTPRequest, proxyURL string) (pluginapi.HTTPResponse, error) {
		if proxyURL != "socks5://global:1080" {
			t.Fatalf("proxy = %q", proxyURL)
		}
		return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"new-free"}]}`)}, nil
	}
	response := planNativeAuth("callback", []byte(`{"auth_index":"native-1","proxy_url":"socks5://global:1080"}`))
	if response.StatusCode != 200 {
		t.Fatalf("status %d: %s", response.StatusCode, response.Body)
	}
}

func TestNativeCatalogAndWritesExcludeResponsesOnlyMuse(t *testing.T) {
	saved := stubNativeAuth(t, nativeFixture)
	openCodeHTTPDo = func(_ context.Context, request hostHTTPRequest, _ string) (pluginapi.HTTPResponse, error) {
		if request.Method != http.MethodGet {
			t.Fatal("unsupported Muse model reached inference")
		}
		return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"new-free"},{"id":"muse-spark-free"},{"id":"provider/MUSE-spark-free"}]}`)}, nil
	}
	plan := planNativeAuth("callback", []byte(`{"auth_index":"native-1"}`))
	if plan.StatusCode != 200 || bytes.Contains(plan.Body, []byte("muse")) || bytes.Contains(plan.Body, []byte("MUSE")) {
		t.Fatalf("unsupported native catalog: %d %s", plan.StatusCode, plan.Body)
	}
	response := saveNativeAuth("callback", []byte(`{"auth_index":"native-1","selected":["muse-spark-free"]}`))
	if response.StatusCode != 400 || len(*saved) != 0 {
		t.Fatalf("unsupported selection saved: %d", response.StatusCode)
	}
	probe := testNativeAuth("callback", []byte(`{"auth_index":"native-1","model":"muse-spark-free"}`))
	if probe.StatusCode != 400 {
		t.Fatalf("unsupported model tested: %d", probe.StatusCode)
	}
}

func TestNativeManagementRoutesDispatchWithBothPrefixes(t *testing.T) {
	for _, prefix := range []string{"", "/v0/management"} {
		for _, route := range []struct{ method, path, body string }{
			{http.MethodGet, "list", ""},
			{http.MethodPost, "plan", `{"auth_index":"native-1"}`},
			{http.MethodPost, "test", `{"auth_index":"native-1","model":"new-free"}`},
			{http.MethodPatch, "save", `{"auth_index":"native-1","selected":["new-free"]}`},
		} {
			t.Run(prefix+route.path, func(t *testing.T) {
				stubNativeAuth(t, nativeFixture)
				request, _ := json.Marshal(managementRPCRequest{ManagementRequest: pluginapi.ManagementRequest{Method: route.method, Path: prefix + nativePath + route.path, Body: []byte(route.body)}})
				raw, err := handleManagement(request)
				if err != nil {
					t.Fatal(err)
				}
				var envelope rpcEnvelope
				if err := json.Unmarshal(raw, &envelope); err != nil {
					t.Fatal(err)
				}
				var response pluginapi.ManagementResponse
				if err := json.Unmarshal(envelope.Result, &response); err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != 200 {
					t.Fatalf("status %d: %s", response.StatusCode, response.Body)
				}
			})
		}
	}
}
