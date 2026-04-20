# opencode-fallback

Proxy local en Go que provee resiliencia automática de proveedores LLM para agentes de coding en [OpenCode](https://github.com/opencode-ai/opencode). Cuando un proveedor falla, el proxy cambia al siguiente de forma transparente — sin intervención del usuario.

Soporta **cualquier proveedor OpenAI-compatible** (OpenAI, Mistral, DeepSeek, OpenRouter, Ollama, Google Gemini, y más) + Anthropic con adapter de formato automático. Funciona con suscripciones (Claude Pro/Max, GitHub Copilot) sin necesidad de API keys.

## Quick Start

```bash
go install github.com/matiasblanca/opencode-fallback/cmd/opencode-fallback@latest
opencode-fallback setup
opencode-fallback configure   # TUI visual para configurar cadenas de fallback
opencode-fallback serve
```

## ¿Qué problema resuelve?

OpenCode **no tiene fallback automático** entre proveedores. Si un proveedor falla (rate limit, timeout, overload), el agente se frena y vos tenés que cambiar de modelo manualmente con `/models`. Esto es un problema real cuando estás en medio de una sesión larga con sub-agentes corriendo en paralelo.

**opencode-fallback** se pone entre OpenCode y los proveedores LLM. Intercepta cada request y, si el proveedor primario falla, automáticamente lo envía al siguiente en una cadena configurable. El cambio es transparente — OpenCode nunca se entera.

```
┌──────────────────────────┐
│        OpenCode          │
│  (agentes y sub-agentes) │
└────────────┬────────���────┘
             │ baseURL: localhost:8787
  ┌──────────▼──────────────────────────────┐
  │         opencode-fallback                │
  │  ┌────────────────────────────────────┐  │
  │  │ Chain Selector   (agent→group→global) │
  │  │ Health Scoring   (sort by health) │  │
  │  │ Retry + Backoff  (same provider)   │  │
  │  │ Circuit Breaker  (weighted reasons)│  │
  │  │ Rate-Limit Cooldown (Retry-After)  │  │
  │  │ Quota Detection  (billing vs rate) │  │
  │  │ Abort Safety     (suppress errors) │  │
  │  │ TTFT Timeout     (hung streams)    │  │
  │  │ Overflow Guard   (never fallback)  │  │
  │  │ Format Adapter   (OpenAI↔Anthropic)│  │
  │  │ Subscription Auth (OAuth bridge)   │  │
  │  └───────────┬────────────────────────┘  │
  └──────────────┼───────────────────────────┘
       ┌─────────┼─────┬──────────┬─────────┐
       ▼         ▼     ▼          ▼         ▼
   Anthropic  OpenAI Mistral  DeepSeek   ...N
```

## Features

### Smart Selection (v0.9)

- **Quota exhaustion detection** — Distingue rate limits temporales (429 → retry en segundos) de billing quota agotada (429 → proveedor muerto por horas). 10 patrones regex detectan "exceeded your current quota", "billing hard limit", "insufficient_quota", etc. Los quota exhaustion son fatales — no retry, no circuit breaker recording.
- **Health-scored provider selection** — Antes de caminar la cadena, los proveedores se ordenan por salud: healthy (3) > half-open (2) > cooldown (1) > open/down (0). Sort estable preserva el orden configurado entre providers con igual score.
- **Abort safety** — Cuando el usuario cancela (Ctrl+C), los errores post-abort no se registran en el circuit breaker. La cadena para inmediatamente sin intentar más providers.

### Resiliencia inteligente (v0.7–v0.8)

- **Retry with backoff** — Antes de saltar al siguiente proveedor, reintenta el mismo con backoff exponencial. Muchos rate limits se resuelven en segundos.
- **Reason-aware circuit breaker** — Rate limits (peso 1), server errors (peso 2), y timeouts (peso 3) tienen distintos pesos. Auth errors no cuentan.
- **Rate-limit cooldown** — Respeta el header `Retry-After` del proveedor. El circuit breaker usa ese valor como duración de cooldown.
- **TTFT timeout** — Si un stream se abre pero nunca produce tokens (stream colgado), detecta y salta al siguiente proveedor en 15s.
- **Overflow exclusion** — 14 patrones regex detectan context overflow. Estos errores **nunca** disparan fallback (necesitan compaction, no otro proveedor).
- **Status endpoint** — `GET /v1/status` devuelve estado de providers, circuit breakers, cooldowns, y fallbacks recientes.

### Subscription auth (v0.4–v0.5)

- **Sin API keys** — Funciona con suscripciones de Claude Pro/Max y GitHub Copilot
- **OAuth token bridge** — Plugin de OpenCode captura tokens OAuth y los envía al proxy
- **Token refresh automático** — Renueva tokens de Anthropic antes de que expiren
- **Claude Code impersonation** — Inyecta headers y system prompt requeridos por la API de suscripción

### Configuración visual (v0.3)

- **TUI interactiva** — Configura cadenas de fallback visualmente con Bubbletea
- **Model picker** — Filtro fuzzy de modelos agrupados por proveedor
- **Status screen** — Ve estado de bridge, auth, y providers en tiempo real
- **Responsive** — Funciona en terminales de 50 a 200+ columnas

### Core (v0.1–v0.2)

- **Fallback automático** — Cadena configurable por agente, grupo, o global
- **Universal** — Cualquier API OpenAI-compatible funciona con solo config
- **Circuit breaker** — Skip automático de proveedores caídos
- **Format adapter** — Convierte entre formato OpenAI y Anthropic transparentemente
- **Auto-detección** — Detecta proveedores por env vars sin config

## Instalación

### go install

```bash
go install github.com/matiasblanca/opencode-fallback/cmd/opencode-fallback@latest
```

Requiere Go 1.24+.

### Binarios pre-compilados

Descargá el binario para tu plataforma desde [GitHub Releases](https://github.com/matiasblanca/opencode-fallback/releases). Disponible para Windows, macOS, y Linux (amd64 y arm64).

## Uso

### Modo standalone (serve)

Levanta el proxy en el puerto 8787. Vos lanzás OpenCode por separado.

```bash
opencode-fallback serve
```

### Modo wrapper (run)

Levanta el proxy y lanza OpenCode como subprocess. Cuando OpenCode termina, el proxy se apaga automáticamente.

```bash
opencode-fallback run -- opencode
```

### Setup automático

Configura `opencode.json` para que todos los proveedores apunten al proxy. Crea un backup automático.

```bash
opencode-fallback setup        # Configurar
opencode-fallback setup --undo # Restaurar config original
```

### Status endpoint

```bash
curl http://localhost:8787/v1/status
```

Devuelve JSON con:
- Versión y uptime del proxy
- Estado de cada provider (circuit state, available, cooldown)
- Últimos 50 fallback events con timestamps y razones

## Configuración

### Zero-config (auto-detección)

Si no existe archivo de configuración, el proxy escanea variables de entorno y arma la cadena automáticamente:

| Variable de entorno | Proveedor | Base URL |
|---------------------|-----------|----------|
| `ANTHROPIC_API_KEY` | Anthropic | `https://api.anthropic.com` |
| `OPENAI_API_KEY` | OpenAI | `https://api.openai.com` |
| `MISTRAL_API_KEY` | Mistral | `https://api.mistral.ai` |
| `DEEPSEEK_API_KEY` | DeepSeek | `https://api.deepseek.com` |
| `GEMINI_API_KEY` | Google Gemini | `https://generativelanguage.googleapis.com/v1beta/openai` |
| `OPENROUTER_API_KEY` | OpenRouter | `https://openrouter.ai/api/v1` |
| *(probe localhost:11434)* | Ollama | `http://localhost:11434` |

No necesitás configurar nada si ya tenés las API keys en tu entorno. El proxy detecta todo solo.

### Archivo de configuración

Ubicación: `~/.config/opencode-fallback/config.json` (Linux/macOS) o `%APPDATA%\opencode-fallback\config.json` (Windows).

**Mínimo:**

```json
{
  "version": "1",
  "providers": {
    "anthropic": { "api_key": "$ANTHROPIC_API_KEY" },
    "openai": { "api_key": "$OPENAI_API_KEY" }
  },
  "fallback_chains": {
    "_global": [
      { "provider": "anthropic", "model": "claude-sonnet-4-20250514" },
      { "provider": "openai", "model": "gpt-4o" }
    ]
  }
}
```

**Con proveedores custom:**

```json
{
  "version": "1",
  "providers": {
    "anthropic": { "api_key": "$ANTHROPIC_API_KEY" },
    "openai": { "api_key": "$OPENAI_API_KEY" },
    "mistral": {
      "type": "openai-compatible",
      "base_url": "https://api.mistral.ai",
      "api_key": "$MISTRAL_API_KEY",
      "models": ["mistral-large-latest", "codestral-latest"]
    },
    "openrouter": {
      "type": "openai-compatible",
      "base_url": "https://openrouter.ai/api/v1",
      "api_key": "$OPENROUTER_API_KEY"
    },
    "ollama": { "base_url": "http://localhost:11434" }
  },
  "fallback_chains": {
    "_global": [
      { "provider": "anthropic", "model": "claude-sonnet-4-20250514" },
      { "provider": "openai", "model": "gpt-4o" },
      { "provider": "mistral", "model": "mistral-large-latest" },
      { "provider": "openrouter", "model": "anthropic/claude-sonnet-4" }
    ]
  }
}
```

**Completo con cadenas por agente:**

```json
{
  "version": "1",
  "proxy": { "port": 8787, "host": "127.0.0.1", "log_level": "info" },
  "providers": {
    "anthropic": { "api_key": "$ANTHROPIC_API_KEY", "models": ["claude-sonnet-4-20250514"] },
    "openai": { "api_key": "$OPENAI_API_KEY", "models": ["gpt-4o", "gpt-4o-mini"] },
    "deepseek": { "api_key": "$DEEPSEEK_API_KEY", "models": ["deepseek-chat"] },
    "ollama": { "base_url": "http://localhost:11434", "models": ["qwen2.5-coder:32b"] }
  },
  "fallback_chains": {
    "_global": [
      { "provider": "anthropic", "model": "claude-sonnet-4-20250514" },
      { "provider": "openai", "model": "gpt-4o" },
      { "provider": "deepseek", "model": "deepseek-chat" },
      { "provider": "ollama", "model": "qwen2.5-coder:32b" }
    ],
    "_groups": {
      "sdd-*": [
        { "provider": "anthropic", "model": "claude-sonnet-4-20250514" },
        { "provider": "openai", "model": "gpt-4o" }
      ]
    },
    "agents": {
      "sdd-explore": [
        { "provider": "deepseek", "model": "deepseek-chat" },
        { "provider": "ollama", "model": "qwen2.5-coder:32b" }
      ]
    }
  },
  "circuit_breaker": { "failure_threshold": 3, "failure_window_seconds": 60, "open_duration_seconds": 30 }
}
```

### Agregar un proveedor custom

Cualquier API OpenAI-compatible funciona con solo config — sin código nuevo:

```json
{
  "providers": {
    "mi-proveedor": {
      "type": "openai-compatible",
      "display_name": "Mi Proveedor Custom",
      "base_url": "https://api.mi-proveedor.com/v1",
      "api_key": "$MI_API_KEY",
      "auth_type": "bearer",
      "models": ["modelo-1", "modelo-2"]
    }
  }
}
```

| Campo | Requerido | Default | Descripción |
|-------|-----------|---------|-------------|
| `type` | No | `openai-compatible` (inferido) | `"openai-compatible"` o `"anthropic"` |
| `display_name` | No | nombre del provider | Nombre para logs |
| `base_url` | Sí | — | URL base de la API |
| `api_key` | Depende | — | Soporta `$ENV_VAR` |
| `auth_type` | No | `"bearer"` | `"bearer"` o `"none"` (para Ollama) |
| `models` | No | `[]` | Lista de modelos soportados |

### TUI Configurator

Configurá todo visualmente sin editar JSON:

```bash
opencode-fallback configure
```

La TUI lee tus agentes de `opencode.json` y te permite:

- **Ver todos tus agentes** con su modelo actual y cadena de fallback
- **Editar la cadena global** (4 slots: primario + 3 fallbacks)
- **Crear overrides por agente** — cadenas custom para agentes específicos
- **Agregar agentes manualmente** con la tecla `n`
- **Buscar modelos** con filtro fuzzy, agrupados por proveedor
- **Ver providers detectados** con estado available/offline
- **Status screen** — estado en tiempo real de bridge, auth, y providers

Navegación: `j/k` para mover, `Enter` para editar, `Tab` para cambiar de tab, `Ctrl+S` para guardar, `?` para help contextual.

### Cadenas de fallback

Las cadenas se resuelven con una cascada de 3 niveles — lo más específico gana:

1. **Agente** (`agents.sdd-apply`) — cadena para un agente específico
2. **Grupo** (`_groups.sdd-*`) — glob pattern que matchea múltiples agentes
3. **Global** (`_global`) — cadena por defecto para todos

## Cómo funciona

1. **Request llega** al proxy en formato OpenAI-compatible
2. **Chain Selector** elige la cadena de fallback (agente → grupo → global)
3. **Health scoring** — ordena providers por salud (healthy > half-open > cooldown > down), preservando orden configurado entre iguales
4. **Circuit breaker check** — skip providers con circuito abierto o en cooldown
5. **Intenta el proveedor** — envía el request
6. **Si falla con error retriable** — reintenta el **mismo** proveedor con backoff exponencial (hasta 1 retry)
7. **Si agota retries o error fatal** — registra el fallo y pasa al siguiente proveedor
8. **Si el usuario aborta** — para la cadena inmediatamente sin penalizar al proveedor
9. **El primero que responde** envía la respuesta de vuelta a OpenCode — transparente

### Clasificación de errores

| HTTP Code | Significado | Retry? | Fallback? | CB Weight |
|-----------|-------------|--------|-----------|-----------|
| 429 | Rate limit | Sí (con Retry-After) | Sí | 1 (lento) |
| 429 + quota body | Billing exhausted | No | Sí (skip) | 0 (fatal) |
| 529 | Overloaded (Anthropic) | Sí | Sí | 1 (lento) |
| 500, 502, 503 | Server error | Sí | Sí | 2 (medio) |
| Timeout | Sin respuesta | Sí | Sí | 2 (medio) |
| TTFT timeout | Stream colgado | Sí | Sí | 3 (rápido) |
| Connection refused | Proveedor caído | Sí | Sí | 2 (medio) |
| Context overflow | Prompt muy largo | No | **No** | 0 (ignorado) |
| 401, 403 | Auth inválido | No | Sí (skip) | 0 (ignorado) |
| 404 | Modelo no encontrado | No | Sí (skip) | 0 (ignorado) |
| ctx.Err() | User abort (Ctrl+C) | No | **No** (stops chain) | 0 (ignorado) |

### Circuit Breaker

Cada proveedor tiene su propio circuit breaker con pesos por tipo de error:

- **Threshold = 3 pesos** en ventana de **1 minuto**
  - 3 rate limits (peso 1 cada uno) = trip
  - 2 server errors (peso 2 cada uno) = trip
  - 1 TTFT timeout (peso 3) = trip inmediato
  - Auth errors (peso 0) = nunca trip
- Proveedor open se **skippea por 30 segundos** (o la duración del `Retry-After` si es mayor)
- Después del cooldown, hace un intento de prueba (half-open) para ver si se recuperó

### Retry con backoff

Antes de saltar al siguiente proveedor en la cadena:

1. Reintenta el **mismo** proveedor 1 vez (configurable)
2. Delay exponencial: `1s × 2^attempt` (máximo 10s)
3. Si el proveedor envía `Retry-After`, usa ese valor como delay
4. **Solo para errores retriables** — auth errors y overflow no se reintentan
5. El wait es **cancelable** — si el contexto se cancela, para inmediatamente

## Proveedores soportados

### Nativos (auto-detección)

| Proveedor | Formato | Auth | Env var |
|-----------|---------|------|---------|
| Anthropic | Adapter OpenAI ↔ Anthropic | API key / OAuth | `ANTHROPIC_API_KEY` |
| OpenAI | OpenAI-compatible | API key | `OPENAI_API_KEY` |
| Mistral | OpenAI-compatible | API key | `MISTRAL_API_KEY` |
| DeepSeek | OpenAI-compatible | API key | `DEEPSEEK_API_KEY` |
| Google Gemini | OpenAI-compatible (via endpoint compat) | API key | `GEMINI_API_KEY` |
| OpenRouter | OpenAI-compatible | API key | `OPENROUTER_API_KEY` |
| Ollama | OpenAI-compatible | Sin auth | Auto-detect en localhost |

### Subscription auth (sin API keys)

| Proveedor | Método | Requiere |
|-----------|--------|----------|
| Anthropic (Claude Pro/Max) | OAuth token via bridge plugin | Plugin bridge instalado en OpenCode |
| GitHub Copilot | Device auth | Plugin bridge instalado en OpenCode |

### Custom (via config)

Cualquier API que acepte el formato OpenAI `/v1/chat/completions` funciona — solo agregalo al config. Esto incluye Together AI, Groq, Fireworks AI, Azure OpenAI, LM Studio, llama.cpp, y cualquier otro.

## Desarrollo

```bash
go test ./...                        # Unit + integration tests (647 tests)
go test -tags e2e ./internal/proxy/  # E2E tests (12 tests, full proxy stack)
go test -cover ./...                 # Tests con coverage
go vet ./...                         # Static analysis
go build ./...                       # Compilar todo
```

### E2E Tests

Los E2E tests (`internal/proxy/e2e_full_test.go`) validan el stack completo: un proxy real escuchando en un puerto random, requests HTTP reales, y mock backends con `httptest.Server`. No requieren API keys ni internet.

```bash
go test -tags e2e -v -count=1 ./internal/proxy/ -run TestE2E
```

Cubren: health endpoint, non-streaming/streaming happy path, fallback por server error y connection refused, circuit breaker trips, rate limit cooldown, overflow blocks fallback, streaming fallback, health scoring reorder, abort safety, y quota exhaustion.

~7400 líneas de código de producción, ~9700 líneas de tests.

## Changelog

### v0.9.1

- **E2E test suite** — 12 tests de integración completa que validan el stack entero: proxy real en puerto random + mock backends + requests HTTP reales. Build tag `e2e` (no corren en `go test ./...`).
- **Bugfix: overflow ahora aborta la cadena** — Context overflow (`prompt is too long`) abortaba el retry-same-provider pero seguía fallando a otros providers inútilmente (el mismo prompt oversize falla en todos). Ahora aborta toda la cadena de fallback inmediatamente.
- **CI workflow** — GitHub Actions corre E2E tests con `-tags e2e` en cada push.

### v0.9.0

- **Quota exhaustion detection** — Distingue 429 temporales de billing quota exhausted (10 patrones). Quota exhaustion es fatal — no retry, no CB recording, skip inmediato.
- **Health-scored provider selection** — Providers se ordenan por salud (closed=3, half-open=2, cooldown=1, open=0) antes de caminar la cadena. Sort estable preserva el orden del usuario entre providers con igual score.
- **Abort safety** — Errores post-abort (Ctrl+C) no se registran en el circuit breaker. La cadena para inmediatamente. Reason `"aborted"` no dispara retry ni fallback.
- **+28 tests** (393 → 421 top-level, 647 including subtests)

### v0.8.0

- **Retry with backoff** — Reintenta el mismo proveedor con backoff exponencial antes de saltar al siguiente. Respeta `Retry-After`. Errors fatales no se reintentan.
- **Reason-aware circuit breaker** — Pesos diferenciados por tipo de error: rate_limit=1, server_error=2, ttft_timeout=3, fatal=0. Distintos tipos de fallo abren el circuito a velocidades distintas.
- **Rate-limit cooldown** — Cuando un proveedor envía 429 con `Retry-After`, el circuit breaker entra en cooldown por esa duración. Visible en `/v1/status` como `cooldown_until`.
- **+18 tests** (375 → 393)

### v0.7.0

- **Overflow pattern exclusion** — 14 regex patterns detectan context overflow. Estos errores nunca disparan fallback.
- **TTFT timeout** — Detecta streams colgados (stream abierto pero sin tokens). Fallback a siguiente proveedor después de 15s.
- **Status endpoint** — `GET /v1/status` con estado de providers, circuit breakers, y fallback events recientes.
- **+15 tests** (360 → 375)

### v0.6.0

- **TUI status integration** — Status bar y pantalla de detalle mostrando estado de bridge plugin, auth OAuth, y providers en tiempo real.
- **+8 tests** (352 → 360)

### v0.5.0

- **Plugin bridge** — Plugin TypeScript para OpenCode que captura tokens OAuth y los envía al proxy Go via HTTP bridge.
- **Bridge client en Go** — Recibe tokens del plugin y los usa para autenticación con proveedores.
- **+25 tests** (327 → 352)

### v0.4.0

- **Subscription auth** — Soporte OAuth para Claude Pro/Max y GitHub Copilot sin API keys.
- **Token refresh automático** — Renueva tokens de Anthropic antes de expiración.
- **Claude Code impersonation** — Headers y system prompt requeridos por la API de suscripción.
- **AnthropicOAuthProvider + CopilotProvider** — Providers dedicados para suscripciones.
- **+47 tests** (280 → 327)

### v0.3.0

- **TUI Configurator** — Interfaz visual con Bubbletea v2 para configurar cadenas de fallback.
- **2 tabs**: Global (cadena por defecto) y Agents (lista de agentes desde opencode.json).
- **Chain editor**: 4 slots por agente, model picker con filtro fuzzy agrupado por proveedor.
- **Agregar agentes manualmente**, colores por proveedor, responsive layout, paginación, help contextual.
- **+98 tests** (182 → 280)

### v0.2.0

- **GenericOpenAIProvider** — Cualquier API OpenAI-compatible funciona con solo config, sin código nuevo.
- **Nuevos proveedores**: auto-detección de Mistral, Google Gemini, OpenRouter.
- **Clasificador de errores unificado**: `ClassifyGenericOpenAIError` cubre 95%+ de proveedores.
- **-442 líneas** de código duplicado, **+29 tests** (153 → 182)

### v0.1.0

- MVP: proxy con fallback automático para Anthropic, OpenAI, DeepSeek, Ollama.
- Circuit breaker por proveedor.
- Adapter OpenAI ↔ Anthropic.
- Auto-detección de proveedores por env vars.
- Setup automático de `opencode.json`.
- 153 tests.

## Licencia

MIT
