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
	} {
		if !bytes.Contains(monitorPage, want) {
			t.Fatalf("monitor page missing %q", want)
		}
	}
}
