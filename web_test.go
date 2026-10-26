package main

import (
	"bytes"
	"os/exec"
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
		[]byte("if(verdict&&verdict.ok===false){failed.push(name)"),
		[]byte("if(!verdict.unavailable)unclear.add(name)"),
		[]byte("reasons.set(name,e.message);unclear.add(name)"),
		[]byte("if(unclear.size)counts.push(unclear.size+' inconclusive')"),
		[]byte("inconclusive results are not proof a model is gone"),
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

func TestMonitorNativeRequestsAndProxySelection(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is unavailable for monitor behavior checks")
	}
	const script = `
const assert=require('node:assert/strict'),vm=require('node:vm');
let html=require('node:fs').readFileSync(0,'utf8'),calls=[],fail='',proxy='socks5://global:1080';
const source=html.match(/<script>([\s\S]*)<\/script>/)[1].replace('load();setInterval(()=>void refreshCatalogs(),REFRESH_MS);','globalThis.monitorTest={id,apiKey,fetchCatalog,testProviderModel,saveSelection,loadSources};');
const context={document:{getElementById:()=>({})},location:{host:'test'},navigator:{userAgent:'test'},TextEncoder,TextDecoder,atob,
 localStorage:{getItem:()=>JSON.stringify({state:{managementKey:'management-test'}})},
 fetch:async(path,options)=>{calls.push({path,body:options.body?JSON.parse(options.body):undefined});if(path===fail)return{ok:false,status:502,json:async()=>({error:'proxy lookup failed'})};return{ok:true,status:200,json:async()=>path.endsWith('/proxy-url')?{'proxy-url':proxy}:path.endsWith('/native/list')?{providers:[]}:path.endsWith('/openai-compatibility')?{'openai-compatibility':[]}:path.endsWith('/config')?{}:path.endsWith('/native/save')?{models:[{name:'new-free',alias:'new'}],managed:[]}:{free:['new-free'],models:[]}};}
};vm.runInNewContext(source,context);
(async()=>{const m=context.monitorTest;
 await m.loadSources();calls=[];
 const compat={name:'legacy','base-url':'https://opencode.ai/inference/v1','api-key-entries':[{'proxy-url':'socks5://wrong:1080'},{'api-key':'first-key','proxy-url':'socks5://credential:1080'},{'api-key':'second-key','proxy-url':'socks5://second:1080'}]};
 await m.testProviderModel(compat,'new-free');assert.equal(calls[0].body.api_key,'first-key');assert.equal(calls[0].body.proxy_url,'socks5://credential:1080');
 calls=[];delete compat['api-key-entries'][1]['proxy-url'];await m.testProviderModel(compat,'new-free');assert.equal(calls[0].body.proxy_url,'socks5://global:1080');
 const native={id:'auth:native.json',source_kind:'auth',auth_index:'auth-1',name:'native.json','base-url':'https://opencode.ai/inference/openai/v1',models:[]};
 assert.equal(m.id(native),'auth:native.json');assert.equal(m.id(compat),'legacy\nhttps://opencode.ai/inference/v1');
 calls=[];await m.fetchCatalog(native);assert.equal(calls[0].path,'/v0/management/plugins/free-model-sync/native/plan');assert.equal(calls[0].body.auth_index,'auth-1');assert.equal(calls[0].body.api_key,undefined);
 calls=[];await m.testProviderModel(native,'new-free');assert.equal(calls[0].path,'/v0/management/plugins/free-model-sync/native/test');assert.equal(calls[0].body.proxy_url,'socks5://global:1080');assert.equal(calls[0].body.api_key,undefined);
 calls=[];await m.saveSelection(native,{managed:['old-zero']},new Set(['new-free']),['new-free'],{models:[]});const saved=calls.find(x=>x.path.endsWith('/native/save'));assert.deepEqual(saved.body,{auth_index:'auth-1',selected:['new-free'],managed:['old-zero'],proxy_url:'socks5://global:1080'});assert.equal(native.models[0].alias,'new');assert.equal(calls.some(x=>x.path.endsWith('/openai-compatibility')),false);
 calls=[];compat.models=[{name:'manual-paid',alias:'custom'},{name:'old-free',alias:'old'},{name:'old-zero',alias:'zero'}];await m.saveSelection(compat,{managed:['old-zero']},new Set(['new-free']),['new-free'],{models:[{name:'new-free',alias:'new'}]});const legacySaved=calls.find(x=>x.path.endsWith('/openai-compatibility'));assert.deepEqual(legacySaved.body.value.models,[{name:'manual-paid',alias:'custom'},{name:'new-free',alias:'new'}]);
 const retryState={managed:['old-zero']};fail='/v0/management/plugins/free-model-sync/native/save';await assert.rejects(m.saveSelection(native,retryState,new Set(),['new-free'],{models:[]}));assert.deepEqual(Array.from(retryState.managed),['old-zero']);fail='';calls=[];await m.saveSelection(native,retryState,new Set(),['new-free'],{models:[]});assert.deepEqual(calls.find(x=>x.path.endsWith('/native/save')).body.managed,['old-zero']);
 const compatRetry={managed:['old-zero']};compat.models=[{name:'manual-paid',alias:'custom'},{name:'old-zero',alias:'zero'}];fail='/v0/management/openai-compatibility';await assert.rejects(m.saveSelection(compat,compatRetry,new Set(),['new-free'],{models:[]}));assert.deepEqual(Array.from(compatRetry.managed),['old-zero']);fail='';calls=[];await m.saveSelection(compat,compatRetry,new Set(),['new-free'],{models:[]});assert.deepEqual(calls.find(x=>x.path.endsWith('/openai-compatibility')).body.value.models,[{name:'manual-paid',alias:'custom'}]);
 fail='/v0/management/proxy-url';await assert.rejects(m.loadSources(),/proxy lookup failed/);calls=[];await assert.rejects(m.testProviderModel(native,'new-free'),/proxy configuration/);assert.equal(calls.length,0);
})().catch(e=>{console.error(e);process.exitCode=1});`
	cmd := exec.Command(node, "-e", script)
	cmd.Stdin = bytes.NewReader(monitorPage)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("monitor behavior: %v\n%s", err, output)
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
