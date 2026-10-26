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

func TestMonitorNativeAliasDraftsAndCheckboxActions(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is unavailable for monitor behavior checks")
	}
	const script = `
const assert=require('node:assert/strict'),vm=require('node:vm');
class Element {
 constructor(tag){this.tagName=tag;this.children=[];this.dataset={};this.attributes={};this.className='';this.value='';this.checked=false;this._text='';this.classList={add:(name)=>{this.className+=' '+name}};}
 set textContent(value){this._text=String(value);this.children=[];}
 get textContent(){return this._text+this.children.map(x=>x.textContent).join('');}
 set innerHTML(value){this.textContent=value;}
 append(...nodes){for(const node of nodes){node.parentElement=this;this.children.push(node);}}
 replaceChildren(...nodes){this.children=[];this._text='';this.append(...nodes);}
 add(node){this.append(node);}
 setAttribute(name,value){this.attributes[name]=String(value);}
 getAttribute(name){return this.attributes[name]??null;}
 remove(){if(this.parentElement)this.parentElement.children=this.parentElement.children.filter(x=>x!==this);}
 matches(selector){
  if(selector.startsWith('.'))return this.className.split(/\s+/).includes(selector.slice(1));
  if(selector.startsWith('input')){if(this.tagName!=='input')return false;if(selector.includes('checkbox')&&this.type!=='checkbox')return false;if(selector.includes('text')&&this.type!=='text')return false;return !selector.includes(':checked')||this.checked;}
  return this.tagName===selector;
 }
 querySelectorAll(selector){return this.children.flatMap(node=>[...(node.matches(selector)?[node]:[]),...node.querySelectorAll(selector)]);}
 querySelector(selector){return this.querySelectorAll(selector)[0]||null;}
}
const elements=new Map(),document={getElementById:id=>{if(!elements.has(id))elements.set(id,new Element(id==='picker'?'select':'div'));return elements.get(id)},createElement:tag=>new Element(tag),createTextNode:text=>{const n=new Element('#text');n.textContent=text;return n}};
const canonical='nemotron-3.5-super-vl';
const native={id:'auth:native.json',source_kind:'auth',auth_index:'auth-1',name:'native.json','base-url':'https://opencode.ai/inference/openai/v1',models:[{name:canonical,alias:'opencode-free/'+canonical},{name:'second-free',alias:''}]};
const other={id:'auth:other.json',source_kind:'auth',auth_index:'auth-2',name:'other.json','base-url':native['base-url'],models:[{name:'other-free',alias:'existing-other'}]};
const compat={name:'generic',source_kind:'compat','base-url':'https://example.test/v1',models:[{name:'generic-free',alias:'generic'}]};
const catalog={free:[canonical,'second-free','new-free'],models:[{name:canonical,alias:'catalog-must-not-win'},{name:'second-free',alias:'invented-default'},{name:'new-free',alias:'new'}]},otherCatalog={free:['other-free'],models:other.models},compatCatalog={free:['generic-free'],models:compat.models};
const state={enabled:true,managed:['old-zero']},otherState={enabled:true,managed:[]},calls=[];let failSave=false,deferredSave=null;
const context={document,Option:class extends Element{constructor(text,value){super('option');this.textContent=text;this.value=value}},location:{host:'test'},navigator:{userAgent:'test'},TextEncoder,TextDecoder,atob,setTimeout:()=>0,
 localStorage:{getItem:()=>JSON.stringify({state:{managementKey:'management-test'}})},
 fetch:async(path,options)=>{const body=options.body?JSON.parse(options.body):undefined;calls.push({path,body});if(path.endsWith('/native/save')&&failSave)return{ok:false,status:502,json:async()=>({error:'save failed'})};if(path.endsWith('/native/save')&&deferredSave){const hold=deferredSave;deferredSave=null;hold.started();await hold.promise}let result={};if(path.endsWith('/native/plan'))result=body.auth_index==='auth-1'?catalog:otherCatalog;else if(path.endsWith('/plan'))result=compatCatalog;else if(path.endsWith('/native/test'))result={ok:true};else if(path.endsWith('/native/save')){const provider=body.auth_index==='auth-1'?native:other;result={models:body.selected.map(name=>({name,alias:Object.prototype.hasOwnProperty.call(body.aliases||{},name)?body.aliases[name]:(provider.models.find(m=>m.name===name)?.alias??'new')})),managed:[]};}return{ok:true,status:200,json:async()=>result};}
};
const html=require('node:fs').readFileSync(0,'utf8'),source=html.match(/<script>([\s\S]*)<\/script>/)[1].replace('load();setInterval(()=>void refreshCatalogs(),REFRESH_MS);',"globalThis.monitorTest={render,refreshCatalogs,setup:(ps,ms)=>{providers=ps;monitors=ms;globalProxy='';},catalog:(pid,value)=>catalogs.set(pid,value)};");
vm.runInNewContext(source,context);const m=context.monitorTest,root=elements.get('providers');
const card=provider=>root.querySelectorAll('.provider').find(x=>x.dataset.provider===(provider.id||provider.name+'\n'+provider['base-url']));
const input=(provider,type,name)=>card(provider).querySelectorAll('input').find(x=>x.type===type&&x.dataset.model===name);
const button=(provider,text)=>card(provider).querySelectorAll('button').find(x=>x.textContent===text);
const type=(provider,name,value)=>{const field=input(provider,'text',name);field.value=value;field.oninput();};
const change=provider=>card(provider).querySelector('.checks').onchange();
const nativeSave=()=>calls.filter(x=>x.path.endsWith('/native/save')).at(-1).body;
(async()=>{
 m.setup([native,other,compat],{[native.id]:state,[other.id]:otherState,['generic\n'+compat['base-url']]:{enabled:true,managed:[]}});m.catalog(native.id,catalog);m.catalog(other.id,otherCatalog);m.catalog('generic\n'+compat['base-url'],compatCatalog);m.render();
 assert.equal(card(native).querySelectorAll('input[type="text"]').length,3,'native models need editable alias fields');assert.equal(card(compat).querySelectorAll('input[type="text"]').length,0);
 assert.equal(input(native,'text',canonical).value,'opencode-free/'+canonical);assert.equal(input(native,'text','second-free').value,'','explicit blank must beat catalog default');assert.equal(input(native,'text','new-free').value,'new');
 assert.equal(input(native,'text',canonical).parentElement.tagName,'label');assert.notEqual(input(native,'text',canonical).parentElement,input(native,'checkbox',canonical).parentElement);assert.ok(input(native,'text',canonical).getAttribute('aria-label').includes(canonical));
 type(native,canonical,'nemotron');type(other,'other-free','my-other');assert.equal(calls.length,0,'typing must not save');card(native).querySelector('details').open=true;card(other).querySelector('details').open=true;m.render();assert.equal(input(native,'text',canonical).value,'nemotron');assert.equal(input(other,'text','other-free').value,'my-other');assert.equal(card(native).querySelector('details').open,true);assert.equal(card(other).querySelector('details').open,true);
 await m.refreshCatalogs();assert.equal(input(native,'text',canonical).value,'nemotron');assert.equal(input(other,'text','other-free').value,'my-other');assert.equal(input(native,'text','second-free').value,'');assert.equal(card(native).querySelector('details').open,true);assert.equal(card(other).querySelector('details').open,true);
 button(native,'Select all').onclick();assert.equal(card(native).querySelectorAll('input[type="checkbox"]:checked').length,3);assert.ok(card(native).querySelectorAll('input[type="text"]').every(x=>!x.checked));calls.length=0;await button(native,'Test selected').onclick();assert.deepEqual(calls.filter(x=>x.path.endsWith('/native/test')).map(x=>x.body.model),[canonical,'second-free','new-free']);
 button(native,'Deselect all').onclick();assert.equal(card(native).querySelectorAll('input[type="checkbox"]:checked').length,0);assert.equal(input(native,'text',canonical).value,'nemotron');calls.length=0;await button(native,'Test and select').onclick();assert.equal(calls.filter(x=>x.path.endsWith('/native/test')).length,3);assert.equal(card(native).querySelectorAll('input[type="checkbox"]:checked').length,3);
 input(native,'checkbox','second-free').checked=false;input(native,'checkbox','new-free').checked=false;change(native);calls.length=0;await button(native,'Save selection').onclick();assert.deepEqual(nativeSave().selected,[canonical]);assert.deepEqual(nativeSave().aliases,{[canonical]:'nemotron'});assert.equal(nativeSave().api_key,undefined);assert.equal(calls.some(x=>x.path.endsWith('/openai-compatibility')),false);
 type(native,canonical,'opencode-free/'+canonical);calls.length=0;await button(native,'Save selection').onclick();assert.deepEqual(nativeSave().aliases,{[canonical]:'opencode-free/'+canonical},'editing back to the original alias after save must still send a patch');m.render();assert.equal(input(other,'text','other-free').value,'my-other','saving one source must preserve the other draft');
 type(native,canonical,'');calls.length=0;await button(native,'Save selection').onclick();assert.deepEqual(nativeSave().aliases,{[canonical]:''});m.render();assert.equal(input(native,'text',canonical).value,'','saved canonical-only alias must stay blank after render');
 type(native,'new-free','draft-not-selected');calls.length=0;await button(native,'Save selection').onclick();assert.equal(nativeSave().aliases,undefined,'untouched and deselected aliases must not be sent');m.render();assert.equal(input(native,'text','new-free').value,'draft-not-selected');
 state.managed=['old-zero'];type(native,canonical,'failed-alias');failSave=true;await button(native,'Save selection').onclick();assert.deepEqual(Array.from(state.managed),['old-zero']);m.render();assert.equal(input(native,'text',canonical).value,'failed-alias');assert.equal(input(other,'text','other-free').value,'my-other');failSave=false;calls.length=0;await button(native,'Save selection').onclick();assert.deepEqual(nativeSave().aliases,{[canonical]:'failed-alias'});assert.deepEqual(nativeSave().managed,['old-zero']);
 let revertedStarted,revertedRelease;const revertedSaveStarted=new Promise(resolve=>{revertedStarted=resolve});deferredSave={started:revertedStarted,promise:new Promise(resolve=>{revertedRelease=resolve})};type(native,canonical,'pending-alias');const revertingSave=button(native,'Save selection').onclick();await revertedSaveStarted;type(native,canonical,'failed-alias');revertedRelease();await revertingSave;m.render();assert.equal(input(native,'text',canonical).value,'failed-alias','reverting to the previous alias while a save is pending must remain an explicit edit');calls.length=0;await button(native,'Save selection').onclick();assert.deepEqual(nativeSave().aliases,{[canonical]:'failed-alias'});
 type(native,'new-free','new');let started,release;const saveStarted=new Promise(resolve=>{started=resolve});deferredSave={started,promise:new Promise(resolve=>{release=resolve})};type(native,canonical,'in-flight-alias');const saving=button(native,'Save selection').onclick();await saveStarted;type(native,canonical,'failed-alias');type(native,canonical,'newer-edit');release();await saving;m.render();assert.equal(input(native,'text',canonical).value,'newer-edit','a newer draft must survive an earlier save');calls.length=0;await button(native,'Save selection').onclick();assert.deepEqual(nativeSave().aliases,{[canonical]:'newer-edit'});
 assert.ok(calls.filter(x=>x.path.endsWith('/config')).every(x=>Object.values(x.body.monitors).every(s=>!('aliases' in s))),'alias drafts must not enter monitor config');
})().catch(e=>{console.error(e);process.exitCode=1});`
	cmd := exec.Command(node, "-e", script)
	cmd.Stdin = bytes.NewReader(monitorPage)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("monitor alias behavior: %v\n%s", err, output)
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
