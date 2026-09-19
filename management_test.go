package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func callModelTestEndpoint(t *testing.T, body []byte) pluginapi.ManagementResponse {
	t.Helper()
	request, err := json.Marshal(managementRPCRequest{
		ManagementRequest: pluginapi.ManagementRequest{
			Method: http.MethodPost,
			Path:   "/plugins/free-model-sync/test",
			Body:   body,
		},
		HostCallbackID: "callback-1",
	})
	if err != nil {
		t.Fatal(err)
	}
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
	return response
}

func TestPlanManagementUpdateFetchesAndMerges(t *testing.T) {
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	var gotRequest hostHTTPRequest
	hostCall = func(method string, request any, result any) error {
		if method != pluginabi.MethodHostHTTPDo {
			t.Fatalf("method = %s", method)
		}
		gotRequest = request.(hostHTTPRequest)
		*result.(*pluginapi.HTTPResponse) = pluginapi.HTTPResponse{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"data":[{"id":"mimo-v2.5-free"},{"id":"paid-model"}]}`),
		}
		return nil
	}
	response := planManagementUpdate("callback-1", []byte(`{
		"api_key":"secret-catalog-key",
		"base_url":"https://opencode.ai/zen/v1/",
		"managed":["old-free"],
		"models":[{"name":"manual-model","alias":"manual"},{"name":"old-free","alias":"old"}]
	}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if gotRequest.HostCallbackID != "callback-1" || gotRequest.URL != "https://opencode.ai/zen/v1/models" || gotRequest.Headers.Get("Authorization") != "Bearer secret-catalog-key" {
		t.Fatalf("host request = %#v", gotRequest)
	}
	var plan planResponse
	if err := json.Unmarshal(response.Body, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Models) != 2 || plan.Models[0].Name != "manual-model" || plan.Models[1].Name != "mimo-v2.5-free" {
		t.Fatalf("models = %#v", plan.Models)
	}
}

func TestPlanManagementUpdateForwardsProviderHeaders(t *testing.T) {
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	var gotRequest hostHTTPRequest
	hostCall = func(_ string, request any, result any) error {
		gotRequest = request.(hostHTTPRequest)
		*result.(*pluginapi.HTTPResponse) = pluginapi.HTTPResponse{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"data":[]}`),
		}
		return nil
	}

	response := planManagementUpdate("callback-1", []byte(`{
		"api_key":"bearer-key",
		"base_url":"https://opencode.ai/zen/v1",
		"headers":{"X-Provider":"catalog-token","Authorization":"Token custom","User-Agent":"provider-agent"}
	}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if gotRequest.Headers.Get("X-Provider") != "catalog-token" || gotRequest.Headers.Get("Authorization") != "Token custom" || gotRequest.Headers.Get("User-Agent") != "provider-agent" {
		t.Fatalf("headers = %#v", gotRequest.Headers)
	}
}

func TestPlanManagementUpdateUsesCLIProxyUserAgentByDefault(t *testing.T) {
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	var gotRequest hostHTTPRequest
	hostCall = func(_ string, request any, result any) error {
		gotRequest = request.(hostHTTPRequest)
		*result.(*pluginapi.HTTPResponse) = pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"data":[]}`)}
		return nil
	}

	response := planManagementUpdate("callback-1", []byte(`{"api_key":"bearer-key","base_url":"https://opencode.ai/zen/v1"}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if got := gotRequest.Headers.Get("User-Agent"); got != "cli-proxy-openai-compat" {
		t.Fatalf("User-Agent = %q, want cli-proxy-openai-compat", got)
	}
}

func TestHandleManagementTestsSelectedModel(t *testing.T) {
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	var gotRequest hostHTTPRequest
	hostCall = func(method string, request any, result any) error {
		if method != pluginabi.MethodHostHTTPDo {
			t.Fatalf("method = %s", method)
		}
		gotRequest = request.(hostHTTPRequest)
		*result.(*pluginapi.HTTPResponse) = pluginapi.HTTPResponse{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"choices":[{"message":{"content":"OK"}}]}`),
		}
		return nil
	}

	response := callModelTestEndpoint(t, []byte(`{
				"api_key":"bearer-key",
				"base_url":"https://openrouter.ai/api/v1",
				"headers":{"X-Provider":"chat-token","Authorization":"Token custom","User-Agent":"provider-agent"},
				"model":"example/free"
			}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if gotRequest.Method != http.MethodPost || gotRequest.URL != "https://openrouter.ai/api/v1/chat/completions" || gotRequest.HostCallbackID != "callback-1" {
		t.Fatalf("host request = %#v", gotRequest)
	}
	if gotRequest.Headers.Get("Content-Type") != "application/json" || gotRequest.Headers.Get("X-Provider") != "chat-token" || gotRequest.Headers.Get("Authorization") != "Token custom" || gotRequest.Headers.Get("User-Agent") != "provider-agent" {
		t.Fatalf("headers = %#v", gotRequest.Headers)
	}
	var payload struct {
		Model     string                           `json:"model"`
		Messages  []struct{ Role, Content string } `json:"messages"`
		MaxTokens int                              `json:"max_tokens"`
		Stream    bool                             `json:"stream"`
	}
	if err := json.Unmarshal(gotRequest.Body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != "example/free" || len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "Reply with OK." || payload.MaxTokens != 8 || payload.Stream {
		t.Fatalf("chat payload = %#v", payload)
	}
}

func TestTestModelRejectsMissingModel(t *testing.T) {
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	called := false
	hostCall = func(string, any, any) error {
		called = true
		return nil
	}

	response := callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.ai/zen/v1"}`))
	if response.StatusCode != http.StatusBadRequest || called {
		t.Fatalf("status = %d, host called = %t, body = %s", response.StatusCode, called, response.Body)
	}
}

func TestTestModelDoesNotExposeProviderFailureBody(t *testing.T) {
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	hostCall = func(_ string, request any, result any) error {
		*result.(*pluginapi.HTTPResponse) = pluginapi.HTTPResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       []byte(`{"error":"private provider detail"}`),
		}
		return nil
	}

	response := callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.ai/zen/v1","model":"example/free"}`))
	if response.StatusCode != http.StatusBadGateway || bytes.Contains(response.Body, []byte("private provider detail")) {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
}

func TestTestModelRejectsMalformedChoice(t *testing.T) {
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	hostCall = func(_ string, request any, result any) error {
		*result.(*pluginapi.HTTPResponse) = pluginapi.HTTPResponse{
			StatusCode: http.StatusOK,
			Body:       []byte("{\"choices\":[null]}"),
		}
		return nil
	}

	response := callModelTestEndpoint(t, []byte("{\"base_url\":\"https://opencode.ai/zen/v1\",\"model\":\"example/free\"}"))
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
}

func TestManagementRegisterExposesPagePlanAndModelTest(t *testing.T) {
	raw, err := handleManagementRegister()
	if err != nil {
		t.Fatal(err)
	}
	var envelope rpcEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	var registration managementRegistrationResponse
	if err := json.Unmarshal(envelope.Result, &registration); err != nil {
		t.Fatal(err)
	}
	if len(registration.Routes) != 2 || registration.Routes[0].Path != managementPlanPath || registration.Routes[1].Path != "/plugins/free-model-sync/test" || len(registration.Resources) != 1 || registration.Resources[0].Path != resourcePagePath {
		t.Fatalf("registration = %#v", registration)
	}
}
