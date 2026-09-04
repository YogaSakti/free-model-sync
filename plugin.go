package main

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const pluginID = "free-model-sync"

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

var shuttingDown atomic.Bool

type rpcEnvelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

type lifecycleRequest struct {
	ConfigYAML    []byte `json:"config_yaml"`
	SchemaVersion uint32 `json:"schema_version"`
}

type capabilityRegistration struct {
	ManagementAPI bool `json:"management_api"`
}

type registrationResponse struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  capabilityRegistration `json:"capabilities"`
}

func handleMethod(method string, request []byte) ([]byte, error) {
	if shuttingDown.Load() && method == pluginabi.MethodManagementHandle {
		return errorEnvelope(&rpcError{Code: "plugin_shutdown", Message: "plugin is shutting down", HTTPStatus: 503}), nil
	}
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		var lifecycle lifecycleRequest
		if len(request) > 0 {
			if err := json.Unmarshal(request, &lifecycle); err != nil {
				return errorEnvelope(&rpcError{Code: "invalid_request", Message: "invalid lifecycle request", HTTPStatus: 400}), nil
			}
		}
		return okEnvelope(registration())
	case pluginabi.MethodManagementRegister:
		return handleManagementRegister()
	case pluginabi.MethodManagementHandle:
		return handleManagement(request)
	case pluginabi.MethodPluginShutdown:
		shutdownPlugin()
		return okEnvelope(struct{}{})
	default:
		return errorEnvelope(&rpcError{Code: "unknown_method", Message: "unknown method: " + method}), nil
	}
}

func registration() registrationResponse {
	return registrationResponse{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             "Free Model Sync",
			Version:          Version,
			Author:           "Agoy",
			GitHubRepository: "https://github.com/YogaSakti/free-model-sync",
			ConfigFields:     []pluginapi.ConfigField{},
		},
		Capabilities: capabilityRegistration{ManagementAPI: true},
	}
}

func okEnvelope(result any) ([]byte, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal RPC result: %w", err)
	}
	return json.Marshal(rpcEnvelope{OK: true, Result: raw})
}

func errorEnvelope(rpcErr *rpcError) []byte {
	raw, _ := json.Marshal(rpcEnvelope{OK: false, Error: rpcErr})
	return raw
}

func shutdownPlugin() {
	shuttingDown.Store(true)
}
