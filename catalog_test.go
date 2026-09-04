package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPlanModelsInfersMixedFreePatterns(t *testing.T) {
	raw := []byte(`{"data":[{"id":"z-ai/glm-5.3-free"},{"id":"nvidia/reasoning:free"},{"id":"orcarouter/free"},{"id":"freeform-paid"},{"id":"zero-price","pricing":{"prompt":"0","completion":"0"}},{"id":"paid/model","pricing":{"prompt":"0.1","completion":"0"}}]}`)
	free, err := freeModelIDs(raw)
	if err != nil {
		t.Fatal(err)
	}
	wantFree := []string{"nvidia/reasoning:free", "orcarouter/free", "z-ai/glm-5.3-free", "zero-price"}
	if !reflect.DeepEqual(free, wantFree) {
		t.Fatalf("freeModelIDs() = %#v, want %#v", free, wantFree)
	}
	got := mergeModels([]modelConfig{
		{Name: "manual/model", Alias: "manual"},
		{Name: "old/free:free", Alias: "old"},
		{Name: "nvidia/reasoning:free", Alias: "reasoning-custom"},
	}, []string{"old/free:free", "nvidia/reasoning:free"}, free)
	want := []modelConfig{
		{Name: "manual/model", Alias: "manual"},
		{Name: "nvidia/reasoning:free", Alias: "reasoning-custom"},
		{Name: "orcarouter/free", Alias: "free"},
		{Name: "z-ai/glm-5.3-free", Alias: "glm-5.3"},
		{Name: "zero-price", Alias: "zero-price"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeModels() = %#v, want %#v", got, want)
	}
}

func TestFreeModelIDsRejectsInvalidCatalog(t *testing.T) {
	if _, err := freeModelIDs([]byte(`{"data":"broken"}`)); err == nil {
		t.Fatal("freeModelIDs() error = nil, want invalid catalog")
	}
}

func TestPluginRegistersOnlyManagementAPI(t *testing.T) {
	raw, err := handleMethod("plugin.register", nil)
	if err != nil {
		t.Fatal(err)
	}
	var envelope rpcEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	var registration registrationResponse
	if err := json.Unmarshal(envelope.Result, &registration); err != nil {
		t.Fatal(err)
	}
	if !registration.Capabilities.ManagementAPI || registration.Metadata.Name != "Free Model Sync" {
		t.Fatalf("registration = %#v", registration)
	}
}
