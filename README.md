# Free Model Sync

Free Model Sync is a native [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that monitors selected OpenAI-compatible providers and keeps their currently free models assigned in `config.yaml`.

It is useful for providers whose free model catalog changes over time, such as:

- OpenRouter model IDs ending in `:free`
- opencode Zen model IDs ending in `-free`

The plugin preserves manually configured models. It only replaces entries matching the selected free-model suffix.

## How it works

1. The plugin adds a **Free Model Sync** page to CPA Management Center.
2. The page reads providers from CPA's native `/v0/management/openai-compatibility` endpoint.
3. You choose which providers to monitor and select their free-model suffix rule.
4. The plugin fetches each provider's `/models` catalog through CPA's `host.http.do` callback.
5. Existing manual models are preserved, obsolete matching free models are removed, and current free models are added.
6. The page applies the merged model list through CPA's native `PATCH /v0/management/openai-compatibility` endpoint.

The page performs a lazy refresh at most once per hour while it is opened. You can also sync one provider or all monitored providers manually.

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
./scripts/package.sh 0.1.0 ./dist/free-model-sync.dylib ./dist
```

Release archives follow `free-model-sync_<version>_<goos>_<goarch>.zip`. Each archive contains exactly one platform library at its root, and `checksums.txt` contains its SHA-256 digest.

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
  - name: OpenRouter
    base-url: https://openrouter.ai/api/v1/
    api-key-entries:
      - api-key: sk-your-openrouter-key
    models:
      - name: paid/model
        alias: paid-model
    priority: 5
```

Then open **Free Model Sync** in CPA Management Center:

1. Enable **Monitor** for the provider.
2. Select the suffix rule:
   - **OpenRouter (`:free`)**
   - **Zen / suffix (`-free`)**
3. Click **Sync now**, or use **Sync monitored** for all selected providers.

Provider selections and the last lazy-refresh timestamp are stored in the current browser's `localStorage`.

## Merge behavior

Given this configuration:

```yaml
models:
  - name: paid/model
    alias: paid-model
  - name: old/free:free
    alias: old-free
```

If the live catalog currently exposes `z-ai/glm-5.2:free`, the resulting model list keeps the manual paid model, removes the obsolete `:free` entry, and adds the live free model:

```yaml
models:
  - name: paid/model
    alias: paid-model
  - name: z-ai/glm-5.2:free
    alias: glm-5.2
```

Existing aliases are preserved when a free model remains available. New aliases default to the final model-name segment with the configured suffix removed.

## Current limitations

- Free-model detection is suffix-based and intentionally simple. Provider-specific pricing or metadata rules can be added as provider catalogs evolve.
- Lazy refresh runs only when the management page is opened; the plugin does not run a background scheduler.
- Monitor selections are browser-local and are not shared between devices or browser profiles.
- The page manages `openai-compatibility` providers only.

## Security

- Model catalog requests use CPA's `host.http.do` callback instead of a custom network client.
- Configuration changes use CPA's authenticated native Management API.
- The plugin does not receive, store, log, or render provider API keys or the Management Center password.
- The browser page is bundled, same-origin, and does not load third-party scripts.

## Verification

```sh
CGO_ENABLED=1 go test ./...
CGO_ENABLED=1 go test -race ./...
./scripts/build.sh dev ./dist
```

## License

No license has been granted yet. Add a license file before allowing reuse or redistribution beyond GitHub's default viewing and forking permissions.
