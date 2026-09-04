package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPlanModelsPreservesManualAndReplacesManaged(t *testing.T) {
	raw := []byte(`{"data":[{"id":"z-ai/glm-5.2:free"},{"id":"minimax/minimax-m3:free"},{"id":"paid/model"},{"id":"z-ai/glm-5.2:free"}]}`)
	free, err := freeModelIDs(raw, ":free")
	if err != nil {
		t.Fatal(err)
	}
	got := mergeModels([]modelConfig{
		{Name: "manual/model", Alias: "manual"},
		{Name: "old/free:free", Alias: "old"},
		{Name: "z-ai/glm-5.2:free", Alias: "glm-custom"},
	}, free, ":free")
	want := []modelConfig{
		{Name: "manual/model", Alias: "manual"},
		{Name: "minimax/minimax-m3:free", Alias: "minimax-m3"},
		{Name: "z-ai/glm-5.2:free", Alias: "glm-custom"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeModels() = %#v, want %#v", got, want)
	}
}

func TestFreeModelIDsRejectsInvalidCatalog(t *testing.T) {
	if _, err := freeModelIDs([]byte(`{"data":"broken"}`), "-free"); err == nil {
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
