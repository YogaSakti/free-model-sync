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
		[]byte("Test selected"),
		[]byte("testButton.onclick=async()=>"),
		[]byte("api_key:apiKey(p),headers:p.headers,model:name"),
		[]byte("Refresh catalog"),
		[]byte("headers:p.headers"),
		[]byte("const catalog=await fetchCatalog(p)"),
		[]byte("if(previous)monitors[providerID]=previous;else delete monitors[providerID]"),
		[]byte("async function refreshCatalogs(notify=false)"),
		[]byte("setInterval(()=>void refreshCatalogs(),REFRESH_MS)"),
		[]byte("Stop monitoring"),
		[]byte("all.onclick=()=>"),
		[]byte("none.onclick=()=>"),
		[]byte("save.onclick=async()=>"),
		[]byte("value:{models:next}"),
		[]byte("selected.size}/${free.length}"),
		[]byte("enabled:s.enabled===true,managed:"),
		[]byte("function needsCleanup(v)"),
		[]byte("PREFIX_V1='enc::v1::'"),
		[]byte("PREFIX_V2='enc::v2::'"),
		[]byte("`${SALT}|v2|${location.host}`"),
		[]byte("`${SALT}|${location.host}|${navigator.userAgent}`"),
		[]byte("const token=key();if(!token)throw authError("),
		[]byte("function failPanel(e)"),
		[]byte("}catch(e){failPanel(e);status.textContent=e.message"),
		[]byte("box.className='empty failed'"),
		[]byte("hint.textContent=AUTH_HINT"),
		[]byte("retry.textContent='Retry'"),
		[]byte("response.status===401||response.status===403?authError("),
		[]byte("if(verdict&&verdict.ok===false)throw new Error(verdict.reason"),
		[]byte("reasons.set(name,e.message)"),
		[]byte("failed.map(n=>reasons.get(n)?n+' ('+reasons.get(n)+')':n)"),
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

func TestMonitorPageFollowsCPAMPTheme(t *testing.T) {
	for _, want := range [][]byte{
		[]byte(`:root[data-theme="dark"],:root.theme-dark`),
		[]byte(`:root[data-theme="white"],:root.theme-light`),
		[]byte(`color-scheme:dark`),
		[]byte(`color-scheme:light`),
		[]byte(`var(--app-bg`),
		[]byte(`var(--app-surface`),
		[]byte(`var(--app-text-primary`),
		[]byte(`var(--primary-solid`),
		[]byte(`var(--primary-contrast`),
		[]byte(`@media(prefers-color-scheme:light)`),
	} {
		if !bytes.Contains(monitorPage, want) {
			t.Fatalf("monitor page is missing theme support %q", want)
		}
	}
	if bytes.Contains(monitorPage, []byte(`:root{color-scheme:light;`)) {
		t.Fatal("monitor page must not force a light-only color scheme")
	}
}

func TestMonitorPageGroupsDraftActionsAndTestsAvailableModels(t *testing.T) {
	for _, want := range [][]byte{
		[]byte(`className='tool-actions'`),
		[]byte(`className='tool-save'`),
		[]byte(`all.textContent='Select all'`),
		[]byte(`none.textContent='Deselect all'`),
		[]byte(`checked.length?checked:free`),
		[]byte(`selectPassing=!checked.length`),
		[]byte(`testButton.textContent=checked.length?'Test selected':'Test and select'`),
		[]byte(`if(selectPassing&&passedNames.has(name))input.checked=true`),
	} {
		if !bytes.Contains(monitorPage, want) {
			t.Fatalf("monitor page is missing model action behavior %q", want)
		}
	}
}
