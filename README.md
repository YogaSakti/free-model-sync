# Free Model Sync

Free Model Sync is a native [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that monitors selected OpenAI-compatible providers and keeps their currently free models assigned in `config.yaml`.

It handles providers whose free catalog changes over time and whose model IDs may use different conventions, including mixed patterns such as `model-free`, `model:free`, or `provider/free` in the same catalog.

The plugin preserves manually configured models. Fetching a catalog never changes provider configuration; model changes happen only after an explicit selection save.

## How it works

1. The plugin adds a **Free Model Sync** page to CPA Management Center.
2. The page reads providers from CPA's native `/v0/management/openai-compatibility` endpoint.
3. Providers already containing recognizable free models are monitored automatically unless they are disabled. Other providers remain in an **Add provider** picker and are not rendered until selected.
4. The plugin fetches each selected provider's `/models` catalog through CPA's `host.http.do` callback. The configured provider API key is sent as a Bearer token when available.
5. Free models are inferred per catalog from:
   - a standalone `free` token separated by `-`, `_`, `:`, `/`, or `.`;
   - zero-valued pricing metadata when the catalog supplies it.
6. The fetched catalog becomes the checklist. A model is checked when it exists in the provider's current `models` list.
7. Checkbox, **Enable all**, and **Disable all** changes remain local drafts.
8. **Save selection** preserves manual models and applies only the selected free set through CPA's native `PATCH /v0/management/openai-compatibility` endpoint.

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

The output is platform-specific:

```text
dist/free-model-sync.dylib  # macOS
dist/free-model-sync.so     # Linux or FreeBSD
dist/free-model-sync.dll    # Windows
```

Package a platform release asset:

```sh
./scripts/package.sh 0.2.1 ./dist/free-model-sync.dylib ./dist
```

Release archives follow `free-model-sync_<version>_<goos>_<goarch>.zip`. Each archive contains exactly one platform library at its root, and `checksums.txt` contains its SHA-256 digest.

Tagged releases are built and published by GitHub Actions for macOS arm64 and Linux amd64.

## Install

Copy the library to CPA's configured plugin directory. Stop CPA before replacing a loaded plugin library, then restart it.

Example default locations:

```text
~/.cli-proxy-api/plugins/free-model-sync.dylib
~/.cli-proxy-api/plugins/free-model-sync.so
~/.cli-proxy-api/plugins/free-model-sync.dll
```

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
    models:
      - name: paid/model
        alias: paid-model
    priority: 5
```

Then open **Free Model Sync** in CPA Management Center:

1. Existing providers with recognizable free models appear automatically.
2. Choose another provider from **Add a provider...** if needed.
3. Click **Fetch catalog** to load the complete current free set.
4. Expand **Free models** and edit the checkbox draft.
5. Use **Enable all** or **Disable all**, then click **Save selection** to update the provider config.
6. Click **Stop monitoring** to remove a provider from the monitored cards without changing its configured model list. It remains available in **Add a provider...**.

Success and failure are shown in both the page status and a temporary toast notification.

Provider monitoring state is persisted under `plugins.configs.free-model-sync.monitors` in CPA's `config.yaml`. Free catalog results stay in page memory, and active selection is sourced from `openai-compatibility.models`. The optional `managed` list stores only selected zero-priced models whose IDs do not contain a recognizable `free` token; it is required solely for safe ownership cleanup. No exclusion list is stored. A stopped provider is retained with `enabled: false`.

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
- Providers whose `/models` endpoint requires non-Bearer authentication or custom request fields are not currently supported.

## Security

- Model catalog requests use CPA's `host.http.do` callback instead of a custom network client.
- Provider API keys are forwarded only to their configured catalog endpoint and are not stored by the plugin.
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

No license has been granted yet. Add a license file before allowing reuse or redistribution beyond GitHub's default viewing and forking permissions.
