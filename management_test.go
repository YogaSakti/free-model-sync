package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

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

func TestManagementRegisterExposesPageAndPlan(t *testing.T) {
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
	if len(registration.Routes) != 1 || registration.Routes[0].Path != managementPlanPath || len(registration.Resources) != 1 || registration.Resources[0].Path != resourcePagePath {
		t.Fatalf("registration = %#v", registration)
	}
}
