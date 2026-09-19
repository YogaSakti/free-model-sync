# Free Model Sync

Free Model Sync is a native [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that monitors selected OpenAI-compatible providers and keeps their currently free models assigned in `config.yaml`.

It handles providers whose free catalog changes over time and whose model IDs may use different conventions, including mixed patterns such as `model-free`, `model:free`, or `provider/free` in the same catalog.

The plugin preserves manually configured models. Fetching a catalog never changes provider configuration; model changes happen only after an explicit selection save.

## How it works

1. The plugin adds a **Free Model Sync** page to CPA Management Center.
2. The page reads providers from CPA's native `/v0/management/openai-compatibility` endpoint.
3. Providers already containing recognizable free models are monitored automatically unless they are disabled. Other providers remain in an **Add provider** picker and are not rendered until selected.
4. Adding a provider starts monitoring and immediately fetches its `/models` catalog through CPA's `host.http.do` callback. Manual and hourly catalog refreshes use the same path. An `opencode.ai` provider falls back to other catalog paths, described under [OpenCode Zen](#opencode-zen).
5. Configured provider API keys are sent as Bearer tokens by default. Custom provider headers are forwarded on catalog and model-test requests and can override defaults, including Authorization and User-Agent. If no User-Agent is configured, requests use cli-proxy-openai-compat. A custom header whose value is an unresolved CLIProxyAPI `$Name` reference is dropped instead of sent literally.
6. Model tests send a small chat request and report a verdict rather than an error status, so the page can say why a model failed; see [Model test verdicts](#model-test-verdicts). Tests against `opencode.ai` carry the official client fingerprint, since the probe bypasses the pipeline that would otherwise add it. Other providers are probed as they always were.
7. Free models are inferred per catalog from:
   - a standalone `free` token separated by `-`, `_`, `:`, `/`, or `.`;
   - zero-valued pricing metadata when the catalog supplies it.
8. The fetched catalog becomes the checklist. A model is checked when it exists in the provider's current `models` list.
9. Checkbox, **Enable all**, and **Disable all** changes remain local drafts.
10. **Save selection** preserves manual models and applies only the selected free set through CPA's native `PATCH /v0/management/openai-compatibility` endpoint.

## Requirements

- CLIProxyAPI v7.2.133 or a compatible version with native plugin support
- Go 1.27 for local builds
- A working C compiler for Go's `-buildmode=c-shared`
- CPA Management Center access
- OpenAI-compatible provider entries configured under `openai-compatibility`

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
4. Expand **Free models** and edit the checkbox draft.
5. Use **Select all** or **Deselect all**, then click **Save selection** to update the provider config.
6. Click **Test selected** to send a small chat request to each checked model and see the results, which are explained under [Model test verdicts](#model-test-verdicts). When none are checked, the button becomes **Test and select**; it tests every free model and checks only those that pass. These are real provider requests and may count toward provider quotas; testing does not save the selection.
7. Click **Stop monitoring** to remove a provider from the monitored cards without changing its configured model list. It remains available in **Add a provider...**.

Catalog refresh, save, and model-test outcomes are shown in a temporary toast; model-test detail is also shown below the checklist.

Provider monitoring state is persisted under `plugins.configs.free-model-sync.monitors` in CPA's `config.yaml`. Free catalog results stay in page memory, and active selection is sourced from `openai-compatibility.models`. The optional `managed` list stores only selected zero-priced models whose IDs do not contain a recognizable `free` token; it is required solely for safe ownership cleanup. No exclusion list is stored. A stopped provider is retained with `enabled: false`.

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
| `429`, including `FreeUsageLimitError` | pass — upstream had to accept the request to charge it against the quota |
| `401`, `403`, `404` | unavailable |
| a body saying the model or the endpoint is unavailable or unsupported | unavailable |
| transport failure, `500`, `502`, a transient `503`, an unreadable `2xx` | inconclusive |

The page counts passed, unavailable, and inconclusive separately. An
inconclusive result is not evidence that a model is gone, so saving a selection
taken from one can drop a working model.

Upstream response bodies are never forwarded; only the verdict leaves the
plugin.

## OpenCode Zen

OpenCode needs two things no other provider does.

**The catalog and the inference endpoints sit under different path prefixes.**
`https://opencode.ai/inference/openai/v1/chat/completions` answers, but
`https://opencode.ai/inference/openai/v1/models` is a 404 and the catalog lives
at `https://opencode.ai/inference/v1/models`. A provider on `opencode.ai`
therefore tries the URL derived from its base URL, then `/inference/v1/models`,
then `/zen/v1/models`, and keeps the first that answers.

**The free tier is gated on the official client fingerprint.** A request missing
any single part is refused with `403 FreeTierError`, so a healthy model would
look dead. Model tests against `opencode.ai` carry the whole fingerprint: the
`opencode/1.18.31` User-Agent, `x-opencode-client`, `x-opencode-project`, a
`ses_` session id, a fresh `msg_` request id per call,
`Accept: text/event-stream`, `stream: true`, and the bash, glob, grep and read
tool declarations. The reply is then an event stream, which the plugin reads for
completion chunks. A first chunk carrying only reasoning still counts, since
reasoning models open with empty content.

The proxy path is a separate matter: requests routed through CPA are shaped by
[cpa-opencode-enhancer](https://github.com/YogaSakti/cpa-opencode-enhancer),
which this plugin's probe cannot use because `host.http.do` bypasses the
executor pipeline.

A provider entry looks like this:

```yaml
openai-compatibility:
  - name: Opencode Zen
    base-url: https://opencode.ai/inference/openai/v1
    api-key-entries:
      - api-key: oc_sk_your-opencode-key
    models:
      - name: mimo-v2.5-free
        alias: mimo-v2.5
```

Keys issued before the move to the new inference host were revoked; current keys
start with `oc_sk_`.

Models served only by the Responses API answer `503 Endpoint is unavailable` on
`/chat/completions`. They are reported as unavailable and are never selected on
a chat provider.

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
