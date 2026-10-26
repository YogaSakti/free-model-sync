package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const nativeProvider = "opencode-enhancer"
const nativePath = "/plugins/free-model-sync/native/"
const nativeBaseURL = "https://opencode.ai/inference/openai/v1"

var nativeSaveMu sync.Mutex

type nativeAuthList struct {
	Files []pluginapi.HostAuthFileEntry `json:"files"`
}
type nativeAuthFile struct {
	AuthIndex string          `json:"auth_index"`
	Name      string          `json:"name"`
	Path      string          `json:"path"`
	JSON      json.RawMessage `json:"json"`
}
type nativeCredential struct {
	Type     string            `json:"type"`
	APIKey   string            `json:"api_key"`
	BaseURL  string            `json:"base_url"`
	ProxyURL string            `json:"proxy_url"`
	Headers  map[string]string `json:"headers"`
	Models   []modelConfig     `json:"models"`
}
type nativeProviderRow struct {
	ID         string        `json:"id"`
	SourceKind string        `json:"source_kind"`
	AuthIndex  string        `json:"auth_index"`
	Name       string        `json:"name"`
	BaseURL    string        `json:"base-url"`
	Disabled   bool          `json:"disabled"`
	Models     []modelConfig `json:"models"`
}
type nativeRequest struct {
	AuthIndex string   `json:"auth_index"`
	Managed   []string `json:"managed"`
	Selected  []string `json:"selected"`
	Model     string   `json:"model"`
	ProxyURL  string   `json:"proxy_url"`
}

func nativeEntries() ([]pluginapi.HostAuthFileEntry, error) {
	var response nativeAuthList
	if err := hostCall(pluginabi.MethodHostAuthList, struct{}{}, &response); err != nil {
		return nil, errors.New("native auth list unavailable")
	}
	entries := make([]pluginapi.HostAuthFileEntry, 0)
	seen := make(map[string]bool)
	for _, entry := range response.Files {
		if entry.Provider != nativeProvider && entry.Type != nativeProvider {
			continue
		}
		if entry.RuntimeOnly || entry.Source != "file" || strings.TrimSpace(entry.Path) == "" || strings.TrimSpace(entry.AuthIndex) == "" {
			continue
		}
		name := entry.Name
		// host.auth.save accepts a root file name, never a relative path.
		if name != strings.TrimSpace(name) || strings.ContainsAny(name, `/\`) || !strings.HasSuffix(strings.ToLower(name), ".json") || seen[name] {
			continue
		}
		entry.Name = name
		seen[name] = true
		entries = append(entries, entry)
	}
	return entries, nil
}

func readNativeCredential(entry pluginapi.HostAuthFileEntry) (nativeAuthFile, nativeCredential, error) {
	var file nativeAuthFile
	var credential nativeCredential
	if err := hostCall(pluginabi.MethodHostAuthGet, pluginapi.HostAuthGetRequest{AuthIndex: entry.AuthIndex}, &file); err != nil {
		return file, credential, errors.New("native auth file unavailable")
	}
	if file.AuthIndex != entry.AuthIndex || file.Name != entry.Name || file.Path != entry.Path {
		return file, credential, errors.New("native auth file changed; reload providers")
	}
	if err := json.Unmarshal(file.JSON, &credential); err != nil || credential.Type != nativeProvider || strings.TrimSpace(credential.APIKey) == "" {
		return file, credential, errors.New("invalid native OpenCode auth file")
	}
	credential.BaseURL = strings.TrimRight(strings.TrimSpace(credential.BaseURL), "/")
	if credential.BaseURL == "" {
		credential.BaseURL = nativeBaseURL
	}
	parsed, err := url.Parse(credential.BaseURL)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), openCodeHost) || (parsed.Port() != "" && parsed.Port() != "443") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "/inference/openai/v1" && parsed.Path != "/inference/v1") {
		return file, credential, errors.New("native auth requires an official OpenCode HTTPS base URL")
	}
	file.Name = entry.Name
	return file, credential, nil
}

func requestedNativeAuth(body []byte) (nativeRequest, nativeAuthFile, nativeCredential, int, error) {
	var request nativeRequest
	if json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.AuthIndex) == "" {
		return request, nativeAuthFile{}, nativeCredential{}, 400, errors.New("auth_index is required")
	}
	entries, err := nativeEntries()
	if err != nil {
		return request, nativeAuthFile{}, nativeCredential{}, 502, err
	}
	for _, entry := range entries {
		if entry.AuthIndex == request.AuthIndex {
			file, credential, err := readNativeCredential(entry)
			return request, file, credential, 502, err
		}
	}
	return request, nativeAuthFile{}, nativeCredential{}, 404, errors.New("native OpenCode auth file not found")
}

func listNativeAuth() pluginapi.ManagementResponse {
	entries, err := nativeEntries()
	if err != nil {
		return nativeError(502, err)
	}
	rows := make([]nativeProviderRow, 0, len(entries))
	for _, entry := range entries {
		_, credential, err := readNativeCredential(entry)
		if err != nil {
			return nativeError(502, err)
		}
		rows = append(rows, nativeProviderRow{ID: "auth:" + entry.Name, SourceKind: "auth", AuthIndex: entry.AuthIndex, Name: entry.Name, BaseURL: credential.BaseURL, Disabled: entry.Disabled, Models: credential.Models})
	}
	return managementJSON(200, map[string]any{"providers": rows})
}

func nativePlan(hostCallbackID string, request nativeRequest, credential nativeCredential) pluginapi.ManagementResponse {
	candidates, err := catalogCandidates(credential.BaseURL)
	if err != nil {
		return nativeError(400, err)
	}
	for _, candidate := range candidates {
		upstream, err := openCodeHTTPDo(openCodeContext, hostHTTPRequest{HostCallbackID: hostCallbackID, Method: http.MethodGet, URL: candidate, Headers: providerHeaders(credential.APIKey, nativeCustomHeaders(credential.Headers), "", nil)}, nativeProxy(request, credential))
		if err != nil || upstream.StatusCode < 200 || upstream.StatusCode >= 300 {
			continue
		}
		free, err := freeModelIDs(upstream.Body)
		if err != nil {
			return nativeError(502, err)
		}
		// The native executor supports Chat Completions; Muse requires Responses.
		supported := free[:0]
		for _, name := range free {
			if !strings.HasPrefix(strings.ToLower(name[strings.LastIndexByte(name, '/')+1:]), "muse-") {
				supported = append(supported, name)
			}
		}
		free = supported
		return managementJSON(200, planResponse{CatalogURL: candidate, Free: free, Models: mergeModels(credential.Models, request.Managed, free)})
	}
	return nativeError(502, errors.New("unable to fetch model catalog"))
}

func nativeProxy(request nativeRequest, credential nativeCredential) string {
	if proxyURL := strings.TrimSpace(credential.ProxyURL); proxyURL != "" {
		return proxyURL
	}
	return request.ProxyURL
}

func nativeCustomHeaders(custom map[string]string) map[string]string {
	headers := make(map[string]string, len(custom))
	for name, value := range custom {
		// The native executor owns credentials, its fingerprint, and HTTP framing.
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "authorization", "user-agent", "accept", "content-type", "x-opencode-client", "x-opencode-project", "x-opencode-session", "x-opencode-session-id", "x-opencode-request", "x-cpa-opencode-execution-id", "host", "content-length", "transfer-encoding", "proxy-authorization", "proxy-connection", "connection", "keep-alive", "trailer", "upgrade":
			continue
		}
		headers[name] = value
	}
	return headers
}

func planNativeAuth(hostCallbackID string, body []byte) pluginapi.ManagementResponse {
	request, _, credential, status, err := requestedNativeAuth(body)
	if err != nil {
		return nativeError(status, err)
	}
	return nativePlan(hostCallbackID, request, credential)
}

func testNativeAuth(hostCallbackID string, body []byte) pluginapi.ManagementResponse {
	request, _, credential, status, err := requestedNativeAuth(body)
	if err != nil {
		return nativeError(status, err)
	}
	plan := nativePlan(hostCallbackID, request, credential)
	if plan.StatusCode != 200 {
		return plan
	}
	var catalog planResponse
	if json.Unmarshal(plan.Body, &catalog) != nil {
		return nativeError(502, errors.New("invalid model catalog"))
	}
	found := false
	for _, name := range catalog.Free {
		if name == request.Model {
			found = true
			break
		}
	}
	if !found {
		return nativeError(400, errors.New("model must be in the current free catalog"))
	}
	probe, _ := json.Marshal(modelTestRequest{APIKey: credential.APIKey, BaseURL: credential.BaseURL, Headers: nativeCustomHeaders(credential.Headers), Model: request.Model, ProxyURL: nativeProxy(request, credential)})
	return testManagementModel(hostCallbackID, probe)
}

func saveNativeAuth(hostCallbackID string, body []byte) pluginapi.ManagementResponse {
	nativeSaveMu.Lock()
	defer nativeSaveMu.Unlock()
	request, file, credential, status, err := requestedNativeAuth(body)
	if err != nil {
		return nativeError(status, err)
	}
	if request.Selected == nil {
		return nativeError(400, errors.New("selected models are required"))
	}
	plan := nativePlan(hostCallbackID, request, credential)
	if plan.StatusCode != 200 {
		return plan
	}
	var catalog planResponse
	if json.Unmarshal(plan.Body, &catalog) != nil {
		return nativeError(502, errors.New("invalid model catalog"))
	}
	free := make(map[string]bool, len(catalog.Free))
	for _, name := range catalog.Free {
		free[name] = true
	}
	selected := make(map[string]bool, len(request.Selected))
	for _, name := range request.Selected {
		if !free[name] {
			return nativeError(400, errors.New("selected models must be in the current free catalog"))
		}
		selected[name] = true
	}
	// A catalog request can outlive a key rotation; mutate the latest file only.
	request, file, _, status, err = requestedNativeAuth(body)
	if err != nil {
		return nativeError(status, err)
	}
	var metadata map[string]json.RawMessage
	if json.Unmarshal(file.JSON, &metadata) != nil {
		return nativeError(502, errors.New("invalid native auth JSON"))
	}
	var existing []json.RawMessage
	if raw := metadata["models"]; len(raw) > 0 && json.Unmarshal(raw, &existing) != nil {
		return nativeError(502, errors.New("invalid native model configuration"))
	}
	owned := make(map[string]bool, len(request.Managed))
	for _, name := range request.Managed {
		owned[name] = true
	}
	models := make([]json.RawMessage, 0, len(existing)+len(selected))
	byName := make(map[string]json.RawMessage, len(existing))
	for _, raw := range existing {
		var model modelConfig
		if json.Unmarshal(raw, &model) != nil || strings.TrimSpace(model.Name) == "" {
			return nativeError(502, errors.New("invalid native model configuration"))
		}
		byName[model.Name] = raw
		if !hasFreeToken(model.Name) && !owned[model.Name] && !free[model.Name] {
			models = append(models, raw)
		}
	}
	managed := make([]string, 0)
	for _, name := range catalog.Free {
		if !selected[name] {
			continue
		}
		raw := byName[name]
		if raw == nil {
			raw, _ = json.Marshal(modelConfig{Name: name, Alias: defaultAlias(name)})
		}
		models = append(models, raw)
		if !hasFreeToken(name) {
			managed = append(managed, name)
		}
	}
	metadata["models"], _ = json.Marshal(models)
	updated, err := json.Marshal(metadata)
	if err != nil {
		return nativeError(500, errors.New("unable to encode native auth JSON"))
	}
	var saved pluginapi.HostAuthSaveResponse
	if err := hostCall(pluginabi.MethodHostAuthSave, pluginapi.HostAuthSaveRequest{Name: file.Name, JSON: updated}, &saved); err != nil {
		return nativeError(502, errors.New("unable to save native auth file"))
	}
	var safeModels []modelConfig
	_ = json.Unmarshal(metadata["models"], &safeModels)
	return managementJSON(200, map[string]any{"models": safeModels, "managed": managed})
}

func nativeError(status int, err error) pluginapi.ManagementResponse {
	return managementJSON(status, map[string]string{"error": err.Error()})
}
