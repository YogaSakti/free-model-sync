package main

import (
	"bytes"
	"testing"
)

func TestMonitorPageUsesExplicitModelSelection(t *testing.T) {
	for _, want := range [][]byte{
		[]byte("/v0/management/openai-compatibility"),
		[]byte("/v0/management/plugins/free-model-sync/config"),
		[]byte("/v0/management/plugins/free-model-sync/plan"),
		[]byte("Save selection"),
		[]byte("Fetch catalog"),
		[]byte("Stop monitoring"),
		[]byte("all.onclick=()=>"),
		[]byte("none.onclick=()=>"),
		[]byte("save.onclick=async()=>"),
		[]byte("value:{models:next}"),
		[]byte("out[k]={enabled:s.enabled===true}"),
	} {
		if !bytes.Contains(monitorPage, want) {
			t.Fatalf("monitor page missing %q", want)
		}
	}
	for _, forbidden := range [][]byte{[]byte("excluded"), []byte("managed:compactManaged")} {
		if bytes.Contains(monitorPage, forbidden) {
			t.Fatalf("monitor page still contains %q", forbidden)
		}
	}
}
