package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const (
	managementPlanPath     = "/plugins/free-model-sync/plan"
	managementPlanFullPath = "/v0/management" + managementPlanPath
	managementTestPath     = "/plugins/free-model-sync/test"
	managementTestFullPath = "/v0/management" + managementTestPath
	resourcePagePath       = "/monitor"
	resourcePageFullPath   = "/v0/resource/plugins/free-model-sync" + resourcePagePath
	maxManagementBodyBytes = 512 * 1024
	pageCSP                = "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:"
	openAICompatUserAgent  = "cli-proxy-openai-compat"
)

//go:embed web/monitor.html
var monitorPage []byte

type managementRPCRequest struct {
	pluginapi.ManagementRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type managementRegistrationResponse struct {
	Routes    []pluginapi.ManagementRoute `json:"routes,omitempty"`
	Resources []pluginapi.ResourceRoute   `json:"resources,omitempty"`
}

type planRequest struct {
	APIKey  string            `json:"api_key"`
	BaseURL string            `json:"base_url"`
	Headers map[string]string `json:"headers"`
	Managed []string          `json:"managed"`
	Models  []modelConfig     `json:"models"`
}

type modelTestRequest struct {
	APIKey  string            `json:"api_key"`
	BaseURL string            `json:"base_url"`
	Headers map[string]string `json:"headers"`
	Model   string            `json:"model"`
}

// chatProbe is the Chat Completions request the model test sends. Tools are
// declared only where a provider gates on them; the field is omitted
// otherwise, leaving the probe as it always was.
type chatProbe struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
	Stream    bool          `json:"stream"`
	Tools     []chatTool    `json:"tools,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Parameters  chatToolParameters `json:"parameters"`
}

type chatToolParameters struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
}

type planResponse struct {
	CatalogURL string        `json:"catalog_url"`
	Free       []string      `json:"free"`
	Models     []modelConfig `json:"models"`
}

type hostHTTPRequest struct {
	HostCallbackID string      `json:"host_callback_id,omitempty"`
	Method         string      `json:"method,omitempty"`
	URL            string      `json:"url,omitempty"`
	Headers        http.Header `json:"headers,omitempty"`
	Body           []byte      `json:"body,omitempty"`
}

var hostCall = callHost

func handleManagementRegister() ([]byte, error) {
	return okEnvelope(managementRegistrationResponse{
		Routes: []pluginapi.ManagementRoute{{
			Method:      http.MethodPost,
			Path:        managementPlanPath,
			Description: "Fetch a provider catalog and plan its free model set",
		}, {
			Method:      http.MethodPost,
			Path:        managementTestPath,
			Description: "Test a model from a provider",
		}},
		Resources: []pluginapi.ResourceRoute{{
			Path:        resourcePagePath,
			Menu:        "Free Model Sync",
			Description: "Monitor selected OpenAI-compatible providers and sync free models",
		}},
	})
}

func handleManagement(raw []byte) ([]byte, error) {
	var request managementRPCRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return errorEnvelope(&rpcError{Code: "invalid_request", Message: "invalid management request", HTTPStatus: 400}), nil
	}
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	path := strings.TrimRight(strings.TrimSpace(request.Path), "/")
	if len(request.Body) > maxManagementBodyBytes {
		return okEnvelope(managementJSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"}))
	}
	var response pluginapi.ManagementResponse
	switch {
	case method == http.MethodGet && (path == resourcePagePath || path == resourcePageFullPath):
		response = pageResponse()
	case method == http.MethodPost && (path == managementPlanPath || path == managementPlanFullPath):
		response = planManagementUpdate(request.HostCallbackID, request.Body)
	case method == http.MethodPost && (path == managementTestPath || path == managementTestFullPath):
		response = testManagementModel(request.HostCallbackID, request.Body)
	default:
		response = managementJSON(http.StatusNotFound, map[string]string{"error": "route not found"})
	}
	return okEnvelope(response)
}

func pageResponse() pluginapi.ManagementResponse {
	return pluginapi.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type":            []string{"text/html; charset=utf-8"},
			"Content-Security-Policy": []string{pageCSP},
			"Referrer-Policy":         []string{"no-referrer"},
		},
		Body: append([]byte(nil), monitorPage...),
	}
}

func planManagementUpdate(hostCallbackID string, body []byte) pluginapi.ManagementResponse {
	var request planRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return managementJSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	candidates, err := catalogCandidates(request.BaseURL)
	if err != nil {
		return managementJSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	catalogURL := ""
	var upstream pluginapi.HTTPResponse
	for _, candidate := range candidates {
		var attempt pluginapi.HTTPResponse
		if err := hostCall(pluginabi.MethodHostHTTPDo, hostHTTPRequest{
			HostCallbackID: hostCallbackID,
			Method:         http.MethodGet,
			URL:            candidate,
			Headers:        providerHeaders(request.APIKey, request.Headers, "", nil),
		}, &attempt); err != nil {
			continue
		}
		if attempt.StatusCode >= http.StatusOK && attempt.StatusCode < http.StatusMultipleChoices {
			catalogURL, upstream = candidate, attempt
			break
		}
	}
	if catalogURL == "" {
		return managementJSON(http.StatusBadGateway, map[string]string{"error": "unable to fetch model catalog"})
	}
	free, err := freeModelIDs(upstream.Body)
	if err != nil {
		return managementJSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return managementJSON(http.StatusOK, planResponse{
		CatalogURL: catalogURL,
		Free:       free,
		Models:     mergeModels(request.Models, request.Managed, free),
	})
}

func testManagementModel(hostCallbackID string, body []byte) pluginapi.ManagementResponse {
	var request modelTestRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return managementJSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	request.Model = strings.TrimSpace(request.Model)
	if request.Model == "" {
		return managementJSON(http.StatusBadRequest, map[string]string{"error": "model is required"})
	}
	endpoint, err := providerEndpoint(request.BaseURL, "chat/completions")
	if err != nil {
		return managementJSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	probe := chatProbe{
		Model:     request.Model,
		Messages:  []chatMessage{{Role: "user", Content: "Reply with OK."}},
		MaxTokens: 8,
	}
	var fingerprint http.Header
	if isOpenCodeURL(endpoint) {
		// Zen answers 403 FreeTierError unless the client fingerprint, the
		// stream flag and the tool quartet all arrive together.
		fingerprint = openCodeHeaders()
		probe.Stream = true
		probe.Tools = openCodeToolSet()
	}
	payload, err := json.Marshal(probe)
	if err != nil {
		return managementJSON(http.StatusInternalServerError, map[string]string{"error": "unable to create model test request"})
	}
	var upstream pluginapi.HTTPResponse
	if err := hostCall(pluginabi.MethodHostHTTPDo, hostHTTPRequest{
		HostCallbackID: hostCallbackID,
		Method:         http.MethodPost,
		URL:            endpoint,
		Headers:        providerHeaders(request.APIKey, request.Headers, "application/json", fingerprint),
		Body:           payload,
	}, &upstream); err != nil {
		// The upstream error text can carry provider detail, so only the fact
		// of the failure is reported. A request that never arrived says
		// nothing about the model, so it is not marked unavailable.
		return modelVerdict(request.Model, probeVerdict{Reason: "provider request failed"})
	}
	return modelVerdict(request.Model, classifyProbe(&upstream))
}

// modelVerdict reports a model that did not answer. The management call itself
// succeeded, so it answers 200: a 5xx here is indistinguishable from a proxy
// failure, and an edge that rewrites origin 5xx bodies would replace the
// reason with its own error page before the page could read it.
func modelVerdict(model string, verdict probeVerdict) pluginapi.ManagementResponse {
	if verdict.OK {
		return managementJSON(http.StatusOK, map[string]any{"ok": true, "model": model})
	}
	body := map[string]any{
		"ok":          false,
		"model":       model,
		"reason":      verdict.Reason,
		"unavailable": verdict.Unavailable,
	}
	if verdict.Status > 0 {
		body["status"] = verdict.Status
	}
	return managementJSON(http.StatusOK, body)
}


func providerHeaders(apiKey string, custom map[string]string, contentType string, defaults http.Header) http.Header {
	headers := make(http.Header)
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", openAICompatUserAgent)
	if contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	if apiKey := strings.TrimSpace(apiKey); apiKey != "" {
		headers.Set("Authorization", "Bearer "+apiKey)
	}
	for name, values := range defaults {
		if len(values) > 0 {
			headers.Set(name, values[0])
		}
	}
	for name, value := range custom {
		name = strings.TrimSpace(name)
		// A value like "$X-Opencode-Session" is a CLIProxyAPI reference this
		// plugin cannot resolve. Sending the literal is worse than sending
		// nothing, so drop it the way util.extractCustomHeaders does.
		if name == "" || value == "" || strings.HasPrefix(strings.TrimSpace(value), "$") {
			continue
		}
		headers.Set(name, value)
	}
	return headers
}

func providerEndpoint(baseURL, endpoint string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("valid http or https base URL is required")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + endpoint
	return parsed.String(), nil
}

func managementJSON(status int, payload any) pluginapi.ManagementResponse {
	raw, err := json.Marshal(payload)
	if err != nil {
		status = http.StatusInternalServerError
		raw = []byte(`{"error":"response encoding failed"}`)
	}
	return pluginapi.ManagementResponse{
		StatusCode: status,
		Headers:    http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:       raw,
	}
}
