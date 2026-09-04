package main

import (
	"bytes"
	"testing"
)

func TestMonitorPageUsesNativeConfigRouteAndLazyInterval(t *testing.T) {
	for _, want := range [][]byte{
		[]byte("/v0/management/openai-compatibility"),
		[]byte("/v0/management/plugins/free-model-sync/plan"),
		[]byte("/v0/management/plugins/free-model-sync/config"),
		[]byte("INTERVAL_MS=60*60*1000"),
		[]byte("api_key:providerAPIKey(provider)"),
		[]byte("managed:state.managed"),
		[]byte("plugins.configs.free-model-sync.monitors"),
		[]byte("Stop monitoring"),
		[]byte("async function migrateLegacy()"),
		[]byte("async function saveMonitors()"),
		[]byte("Enable all"),
		[]byte("Disable all"),
		[]byte("function toast(message,tone)"),
		[]byte("Add a provider…"),
	} {
		if !bytes.Contains(monitorPage, want) {
			t.Fatalf("monitor page missing %q", want)
		}
	}
}
