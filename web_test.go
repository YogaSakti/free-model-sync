package main

import (
	"bytes"
	"testing"
)

func TestMonitorPageUsesNativeConfigRouteAndLazyInterval(t *testing.T) {
	for _, want := range [][]byte{
		[]byte("/v0/management/openai-compatibility"),
		[]byte("/v0/management/plugins/free-model-sync/plan"),
		[]byte("INTERVAL_MS=60*60*1000"),
		[]byte("method='GET'"),
		[]byte("ensureInferredSettings()"),
		[]byte("Enable all"),
		[]byte("Disable all"),
		[]byte("excluded=new Set"),
		[]byte("function toast(message,tone)"),
		[]byte("function providerAPIKey(provider)"),
		[]byte("lastAttempt:Date.now()"),
		[]byte("api_key:providerAPIKey(provider)"),
		[]byte("function knownFreeName(name)"),
		[]byte("managed:Array.isArray(previous.available)"),
		[]byte("function visibleProviders(settings)"),
		[]byte("Add a provider…"),
	} {
		if !bytes.Contains(monitorPage, want) {
			t.Fatalf("monitor page missing %q", want)
		}
	}
}
