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
	resourcePagePath       = "/monitor"
	resourcePageFullPath   = "/v0/resource/plugins/free-model-sync" + resourcePagePath
	maxManagementBodyBytes = 512 * 1024
	pageCSP                = "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:"
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
	BaseURL string        `json:"base_url"`
	Suffix  string        `json:"suffix"`
	Models  []modelConfig `json:"models"`
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
	catalogURL, err := modelCatalogURL(request.BaseURL)
	if err != nil {
		return managementJSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if strings.TrimSpace(request.Suffix) == "" {
		return managementJSON(http.StatusBadRequest, map[string]string{"error": "free suffix is required"})
	}
	var upstream pluginapi.HTTPResponse
	if err := hostCall(pluginabi.MethodHostHTTPDo, hostHTTPRequest{
		HostCallbackID: hostCallbackID,
		Method:         http.MethodGet,
		URL:            catalogURL,
		Headers:        http.Header{"Accept": []string{"application/json"}},
	}, &upstream); err != nil || upstream.StatusCode < http.StatusOK || upstream.StatusCode >= http.StatusMultipleChoices {
		return managementJSON(http.StatusBadGateway, map[string]string{"error": "unable to fetch model catalog"})
	}
	free, err := freeModelIDs(upstream.Body, request.Suffix)
	if err != nil {
		return managementJSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return managementJSON(http.StatusOK, planResponse{
		CatalogURL: catalogURL,
		Free:       free,
		Models:     mergeModels(request.Models, free, request.Suffix),
	})
}

func modelCatalogURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("valid http or https base URL is required")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/models"
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
