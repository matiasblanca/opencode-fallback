# opencode-fallback-bridge

OpenCode plugin that bridges authentication between OpenCode and the [opencode-fallback](../../) proxy.

## What it does

When loaded by OpenCode, this plugin starts a local HTTP server that the opencode-fallback proxy can call to:

1. **Get fresh auth tokens** — The proxy doesn't need to read `auth.json` directly or handle token refresh. The plugin uses OpenCode's SDK which handles refresh automatically.

2. **Transform Anthropic requests** — Instead of the proxy reimplementing Claude Code impersonation in Go, it delegates to this plugin. The plugin applies all transformations (system prompt rewrite, tool name prefixing, CCH billing header) and returns the ready-to-send body + headers.

## Architecture

```
[OpenCode Agent] → [Proxy :8787] → [Bridge Plugin :18787] → [Anthropic API]
                                    ↑
                                    localhost-only
                                    bearer token auth
```

The bridge is an **optimization, not a requirement**. If the bridge is unavailable, the proxy falls back to its own Go-based transformation code.

## Installation

```bash
# From the opencode-fallback repo root:
opencode plugin add ./plugins/opencode-fallback-bridge
```

Or add to your `opencode.json` plugins array:

```json
{
  "plugins": ["./plugins/opencode-fallback-bridge"]
}
```

## Configuration

| Env Variable | Default | Description |
|---|---|---|
| `FALLBACK_BRIDGE_PORT` | `18787` | Port for the bridge HTTP server |
| `OPENCODE_DATA_DIR` | platform XDG | Override OpenCode data directory |

## Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/health` | No | Health check |
| `GET` | `/auth/:provider` | Bearer | Get fresh auth tokens |
| `POST` | `/transform/anthropic` | Bearer | Transform Anthropic request body |
| `POST` | `/transform/strip-response` | Bearer | Strip tool prefixes from response |

## Security

- Server binds to `127.0.0.1` only — never network-accessible
- Bearer token generated per-session, stored at `<XDG_DATA_HOME>/opencode/fallback-bridge-token`
- Token file has `0o600` permissions
- No token values are ever logged
