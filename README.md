# Free Model Sync

Free Model Sync is a native [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that monitors selected OpenAI-compatible providers and keeps their currently free models assigned in `config.yaml`. It also manages native `opencode-enhancer` auth files for OpenCode free chat models.

It handles providers whose free catalog changes over time and whose model IDs may use different conventions, including mixed patterns such as `model-free`, `model:free`, or `provider/free` in the same catalog.

The plugin preserves manually configured models. Fetching a catalog never changes provider configuration; model changes happen only after an explicit selection save.

## How it works

1. The plugin adds a **Free Model Sync** page to CPA Management Center.
2. The page reads compatibility providers from CPA's native `/v0/management/openai-compatibility` endpoint and native OpenCode auth descriptors through the plugin's management routes.
3. Providers already containing recognizable free models are monitored automatically unless they are disabled. Other providers remain in an **Add provider** picker and are not rendered until selected.
4. Adding a provider starts monitoring and immediately fetches its `/models` catalog. Generic providers use CPA's `host.http.do` callback; native OpenCode sources use this plugin's Go/uTLS transport. Manual and hourly catalog refreshes use the same path. An `opencode.ai` provider falls back to other catalog paths, described under [OpenCode Zen](#opencode-zen).
5. Configured provider API keys are sent as Bearer tokens by default. Custom provider headers are forwarded on catalog and model-test requests and can override defaults, including Authorization and User-Agent. If no User-Agent is configured, requests use cli-proxy-openai-compat. A custom header whose value is an unresolved CLIProxyAPI `$Name` reference is dropped instead of sent literally.
6. Model tests send a small chat request and report a verdict rather than an error status, so the page can say why a model failed; see [Model test verdicts](#model-test-verdicts). Tests against `opencode.ai` carry the official client fingerprint, since the probe bypasses the pipeline that would otherwise add it. Other providers are probed as they always were.
7. Free models are inferred per catalog from:
   - a standalone `free` token separated by `-`, `_`, `:`, `/`, or `.`;
   - zero-valued pricing metadata when the catalog supplies it.
8. The fetched catalog becomes the checklist. A model is checked when it exists in the provider's current `models` list.
9. Checkbox, **Enable all**, and **Disable all** changes remain local drafts.
10. **Save selection** preserves manual models and applies only the selected free set through CPA's native compatibility endpoint or `host.auth.save` for a native OpenCode auth file.

## Requirements

- CLIProxyAPI v7.2.133 or a compatible version with native plugin support
- Go 1.27 for local builds
- A working C compiler for Go's `-buildmode=c-shared`
- CPA Management Center access
- OpenAI-compatible provider entries configured under `openai-compatibility`, or native `opencode-enhancer` auth files (tested on unchanged CPA 8.0.43)

## Build

```sh
./scripts/build.sh dev ./dist
```

The library carries the version it was built with. A numeric version is written as `v<version>`, and any other label, such as the `dev` above, is used as it stands. The extension is platform-specific:

```text
dist/free-model-sync-dev.dylib  # macOS
dist/free-model-sync-dev.so     # Linux or FreeBSD
dist/free-model-sync-dev.dll    # Windows
```

Package a platform release asset:

```sh
./scripts/package.sh 0.2.1 ./dist/free-model-sync-dev.dylib ./dist
```

Release archives follow `free-model-sync_<version>_<goos>_<goarch>.zip`. Each archive contains exactly one platform library at its root, named `free-model-sync-v<version>` with the platform extension, and `checksums.txt` contains its SHA-256 digest.

Prebuilt archives on the GitHub Release cover `linux_amd64` only. Cross-building
the other platforms needs a matching C toolchain for `-buildmode=c-shared` on each
target, so macOS, Windows, and FreeBSD build from source with `./scripts/build.sh`.

Release Please watches conventional commits merged into `main` and maintains a release PR. The workflow merges that release PR itself, so a single merge into `main` creates the next semantic version, runs tests, builds the Linux amd64 archive, and uploads it to the GitHub Release. Self-merging needs a `RELEASE_PLEASE_TOKEN` secret holding a personal access token with `contents: write` and `pull-requests: write` on this repository; the workflow fails with a reminder if the secret is missing, and the release PR can then be merged by hand. `fix:` commits produce patches, `feat:` commits produce minor releases, and documentation-only commits do not trigger a release.

## Install

Copy the library to CPA's configured plugin directory. Stop CPA before replacing a loaded plugin library, then restart it.

Example default locations:

```text
~/.cli-proxy-api/plugins/free-model-sync-v0.9.2.dylib
~/.cli-proxy-api/plugins/free-model-sync-v0.9.2.so
~/.cli-proxy-api/plugins/free-model-sync-v0.9.2.dll
```

Because the file name carries the version, upgrading adds a file rather than replacing one. CPA loads every library in that directory, so delete the previous version before restarting; leaving both loads the plugin twice.

Enable the plugin in `config.yaml`:

```yaml
plugins:
  enabled: true
  dir: ~/.cli-proxy-api/plugins
  configs:
    free-model-sync:
      enabled: true
```

After restarting CPA, `/v0/management/plugins` should report `free-model-sync` with both `registered: true` and `effective_enabled: true`.

## Usage

Configure providers normally under `openai-compatibility`:

```yaml
openai-compatibility:
  - name: TokenRouter
    base-url: https://api.tokenrouter.com/v1
    api-key-entries:
      - api-key: sk-your-provider-key
    headers:
      User-Agent: provider-client/1.0
      X-Provider-Header: your-header-value
    models:
      - name: paid/model
        alias: paid-model
    priority: 5
```

Then open **Free Model Sync** in CPA Management Center:

1. Existing providers with recognizable free models appear automatically.
2. Choose another provider from **Add a provider...** if needed. Adding it starts monitoring and fetches its catalog immediately.
3. Click **Refresh catalog** to fetch the latest free set again.
4. Expand **Free models** and edit the checkbox draft. Native OpenCode rows also have an **Alias** field; set the name clients should use, or leave it blank to expose only the upstream model ID.
5. Use **Select all** or **Deselect all**, then click **Save selection** to update the provider config.
6. Click **Test selected** to send a small chat request to each checked model and see the results, which are explained under [Model test verdicts](#model-test-verdicts). When none are checked, the button becomes **Test and select**; it tests every free model and checks only those that pass. These are real provider requests and may count toward provider quotas; testing does not save the selection.
7. Click **Stop monitoring** to remove a provider from the monitored cards without changing its configured model list. It remains available in **Add a provider...**.

Catalog refresh, save, and model-test outcomes are shown in a temporary toast; model-test detail is also shown below the checklist.

Provider monitoring state is persisted under `plugins.configs.free-model-sync.monitors` in CPA's `config.yaml`. Free catalog results stay in page memory, and active selection is sourced from `openai-compatibility.models` or the native auth file's `models`. The optional `managed` list stores only selected zero-priced models whose IDs do not contain a recognizable `free` token; it is required solely for safe ownership cleanup. No exclusion list is stored. A stopped provider is retained with `enabled: false`.

## Model test verdicts

A model test asks the provider for a few tokens and reports what came back. The
management call succeeds even when the model does not, so the endpoint answers
`200` and puts the outcome in the body. A `5xx` would be indistinguishable from a
proxy failure, and an edge such as Cloudflare replaces an origin `5xx` body with
its own error page before the page can read the reason.

```json
{"ok": false, "model": "jev-1.13-free", "reason": "provider returned 503", "status": 503, "unavailable": true}
```

A failing test removes a model from the **Test and select** result, so the
verdict separates what the provider settled from what it did not:

| Upstream reply | Verdict |
| --- | --- |
| `2xx` with a usable answer | pass |
| `429`, including `FreeUsageLimitError` | inconclusive — the request may be rejected before the model is tried |
| `401`, `403`, `404` | unavailable |
| a body saying the model or the endpoint is unavailable or unsupported | unavailable |
| transport failure, `500`, `502`, a transient `503`, an unreadable `2xx` | inconclusive |

The page counts passed, unavailable, and inconclusive separately. An
inconclusive result is not evidence that a model is gone, so saving a selection
taken from one can drop a working model.

Upstream response bodies are never forwarded; only the verdict leaves the
plugin.

## OpenCode Zen

OpenCode uses a separate request fingerprint and TLS profile.

**Use the official inference endpoints.** Native credentials use
`https://opencode.ai/inference/openai/v1` for chat and
`https://opencode.ai/inference/v1/models` for the catalog. The legacy
`https://opencode.ai/inference/v1` base is also accepted. Catalog refresh tries
alternate OpenCode paths when the configured base URL has no model list.

**The free tier is gated on the official client fingerprint.** A request missing
any single part is refused with `403 FreeTierError`, so a healthy model would
look dead. Model tests against `opencode.ai` carry the whole fingerprint: the
`opencode/1.18.31` User-Agent, `x-opencode-client`, `x-opencode-project`, a
`ses_` session id in both `x-opencode-session` and `x-opencode-session-id`, a
fresh `msg_` request id per call, `Accept: text/event-stream`, `stream: true`,
and bash, glob, grep and read tool declarations with nonempty parameter schemas.
OpenCode probes also include this environment context as a system message for
Chat Completions or `instructions` for Muse Responses:

```text
<env>
Working directory: not provided
</env>
```

The plugin does not know the caller's working directory. Chat probes use a
128-token budget so reasoning models have room for visible text; Muse Responses
probes retain their 32-token budget. A chat probe passes
only after nonempty text and `[DONE]`; a Muse probe needs nonempty response text
and `response.completed`. Initial reasoning chunks are allowed, but reasoning,
role-only chunks, and unfinished streams do not pass on their own.

**Plugin-owned transport (2026-10-26).** Matched requests completed through Bun
while standard Go TLS returned `429 FreeUsageLimitError`. OpenCode probes now
use Go/uTLS v1.8.2 with Bun 1.4.4's measured ClientHello inside this plugin.
Further matched tests isolated an address-family difference: the same Go/uTLS
request returned `429 FreeUsageLimitError` over IPv6 and completed with visible
text and `[DONE]` over IPv4. Direct connections to the official OpenCode hostname
therefore resolve through IPv4 without pinning an IP address. Explicit HTTP,
HTTPS, and SOCKS proxies retain their own routing and cancellation behavior.
This 429 response alone is not proof that the credential's quota is exhausted.
The native auth monitor's catalog, real model probe, and selection save were
tested on the unchanged CPA 8.0.43 binary. No CPA patch or relay is required.

Client requests routed through CPA use the native executor in
[cpa-opencode-enhancer](https://github.com/YogaSakti/cpa-opencode-enhancer),
while this plugin sends its own catalog and test requests.

### Native OpenCode auth sources

Create a native credential using the enhancer's
[setup instructions](https://github.com/YogaSakti/cpa-opencode-enhancer#4-add-a-native-free-chat-credential).
Keep it as a root-level `.json` file in CPA's `auth-dir`; nested auth files are
excluded because CPA's save API accepts root file names only. It appears in the
provider picker and uses the same catalog, test, and save controls.

Native rows provide an **Alias** text field next to each model. Aliases are
editable drafts until **Save selection**; catalog refreshes keep those drafts.
No `opencode-free/` prefix is required. For example, set the alias of
`nemotron-3.5-super-vl-free` to `nemotron-3.5-super-vl` or another name of your
choice. A blank alias keeps only the canonical upstream ID. Aliases cannot
conflict with another model's name or alias within the same credential.
Shared aliases across providers retain CPA's normal provider selection.

The browser receives the source identity, selected models, and verdicts.
The API key and custom auth headers are read server-side through `host.auth.get`.
For native sources, the stored API key and official probe fingerprint take
precedence over custom Authorization, User-Agent, session, and framing headers.
Unrelated custom headers are retained.
Saving preserves unknown JSON fields, the current key, and metadata on retained
models. The fresh catalog validates the selected free IDs before writing.
Responses-only Muse models are excluded from native Chat Completions sources.

Native management routes under `/v0/management/plugins/free-model-sync/native/`
are `GET list`, `POST plan`, `POST test`, and `PATCH save`. The latter three
identify the credential by `auth_index`; the server rechecks the file identity
on each request. `PATCH save` accepts optional `aliases` patches keyed by
selected upstream model ID, for example
`"aliases":{"nemotron-3.5-super-vl-free":"nemotron-3.5-super-vl"}`. An omitted
alias is preserved; an empty string clears it. Credential `proxy_url` takes priority over CPA's global proxy,
then the process proxy environment. The page stops if it cannot read the global
proxy rather than silently switching paths. Native catalog and probe attempts
have a 45-second timeout and a 4 MiB response limit.

Legacy compatibility entries remain supported:

A provider entry looks like this:

```yaml
openai-compatibility:
  - name: Opencode Zen
    base-url: https://opencode.ai/inference/v1
    api-key-entries:
      - api-key: oc_sk_your-opencode-key
    models:
      - name: mimo-v2.6-flash-free
        alias: mimo-v2.6-flash
```

Keys issued before the move to the new inference host were revoked; current keys
start with `oc_sk_`.

Other models that reject both Chat Completions and Responses stay failed in the
test results. A provider-side model outage can still fail an individual test.

## Troubleshooting

**The page shows an error instead of the provider list.** The page reads the CPA
management key from the browser, from the same storage the Management Center
uses, and both the `enc::v1::` and `enc::v2::` formats are understood. A key
saved on a different host or port cannot be read, because the stored value is
tied to the origin. Open the page from the Management Center on the same origin,
and sign in with *Remember password* enabled — without it the key is never
persisted for the page to find.

**Refreshing a catalog fails for an OpenCode provider.** Its base URL probably
points at an inference prefix that serves no catalog; see
[OpenCode Zen](#opencode-zen) above.

**Every model fails a test.** Check the reason shown next to each model. A row
of `403` results against `opencode.ai` means the fingerprint did not reach
upstream; a row of inconclusive results means the probes never got an answer at
all, and the selection should not be saved in that state.

## Detection examples

The catalog inference supports mixed naming in one provider:

```text
z-ai/glm-5.3-free
nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free
orcarouter/free
```

It does not classify unrelated words such as `freeform-paid`, because `free` must be a complete token at a supported boundary.

When pricing metadata is present, a model is also considered free if every supplied billable field among `prompt`, `completion`, `input`, `output`, `request`, and `image` is numeric zero.

## Merge behavior

Given this configuration:

```yaml
models:
  - name: paid/model
    alias: paid-model
  - name: old/free:free
    alias: old-free
```

If the live catalog currently exposes `z-ai/glm-5.3-free` and `nvidia/reasoning:free`, the resulting model list keeps the manual paid model, removes the obsolete managed entry, and adds both current free models:

```yaml
models:
  - name: paid/model
    alias: paid-model
  - name: nvidia/reasoning:free
    alias: reasoning
  - name: z-ai/glm-5.3-free
    alias: glm-5.3
```

Existing aliases are preserved when a free model remains available. New aliases default to the final model-name segment with a trailing free marker removed.

## Current limitations

- Free-model detection uses ID token boundaries and available zero-pricing metadata; provider-specific flags not represented by either signal require a future detector.
- Lazy refresh runs only when the management page is opened; the plugin does not run a background scheduler.
- The page manages `openai-compatibility` providers only.
- Provider-specific request-body fields are not supported. Providers that need non-Bearer authentication can supply it with custom headers in their `openai-compatibility` entry.

## Security

- Model catalog requests use CPA's `host.http.do` callback instead of a custom network client.
- Provider API keys and custom headers are forwarded only to the configured provider's `/models` and `/chat/completions` endpoints; the plugin does not store them.
- Configuration changes use CPA's authenticated native Management API.
- The plugin does not log or render provider API keys or the Management Center password.
- The browser page is bundled, same-origin, and does not load third-party scripts.

## Verification

```sh
CGO_ENABLED=1 go test ./...
CGO_ENABLED=1 go test -race ./...
./scripts/build.sh dev ./dist
```

## License

Licensed under the [MIT License](LICENSE).
