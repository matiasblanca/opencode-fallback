# opencode-fallback

Proxy local en Go que provee resiliencia automática de proveedores LLM para agentes de coding en OpenCode. Cuando un proveedor falla, el proxy cambia al siguiente de forma transparente — sin intervención del usuario.

Soporta **cualquier proveedor OpenAI-compatible** (OpenAI, Mistral, DeepSeek, OpenRouter, Ollama, Google Gemini, y más) + Anthropic con adapter de formato automático.

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

Además, cada proveedor tiene su propio **circuit breaker**: si un proveedor falla 3 veces en 1 minuto, el proxy deja de intentarlo por 30 segundos y va directo al siguiente. Esto evita perder tiempo con proveedores que están caídos.

```
┌──────────────────────────┐
│        OpenCode          │
│  (agentes y sub-agentes) │
└────────────┬─────────────┘
             │ baseURL: localhost:8787
  ┌──────────▼──────────┐
  │  opencode-fallback   │
  │  ┌────────────────┐  │
  │  │ Chain Selector │  │  ← cadena: agente → grupo → global
  │  │ Circuit Breaker│  │  ← skip proveedores abiertos
  │  │ Format Adapter │  │  ← OpenAI ↔ Anthropic
  │  └───────┬────────┘  │
  └──────────┼───────────┘
       ┌─────┼─────┬──────────┬─────────┐
       ▼     ▼     ▼          ▼         ▼
   Anthropic OpenAI Mistral DeepSeek  ...N
```

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

**Con proveedores custom (v0.2):**

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
    "opencode-zen": {
      "type": "openai-compatible",
      "display_name": "OpenCode Zen",
      "base_url": "https://zen.opencode.ai/v1",
      "api_key": "$OPENCODE_API_KEY"
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
    "mistral": { "base_url": "https://api.mistral.ai", "api_key": "$MISTRAL_API_KEY", "models": ["mistral-large-latest", "codestral-latest"] },
    "deepseek": { "api_key": "$DEEPSEEK_API_KEY", "models": ["deepseek-chat"] },
    "ollama": { "base_url": "http://localhost:11434", "models": ["qwen2.5-coder:32b"] }
  },
  "fallback_chains": {
    "_global": [
      { "provider": "anthropic", "model": "claude-sonnet-4-20250514" },
      { "provider": "openai", "model": "gpt-4o" },
      { "provider": "mistral", "model": "mistral-large-latest" },
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

### TUI Configurator (v0.3)

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

Navegación: `j/k` para mover, `Enter` para editar, `Tab` para cambiar de tab, `Ctrl+S` para guardar, `?` para help contextual.

Funciona con 60+ agentes gracias a scroll con paginación y layout responsive.

### Cadenas de fallback

Las cadenas se resuelven con una cascada de 3 niveles — lo más específico gana:

1. **Agente** (`agents.sdd-apply`) — cadena para un agente específico
2. **Grupo** (`_groups.sdd-*`) — glob pattern que matchea múltiples agentes
3. **Global** (`_global`) — cadena por defecto para todos

## Proveedores soportados

### Nativos (auto-detección)

| Proveedor | Formato | Auth | Env var |
|-----------|---------|------|---------|
| Anthropic | Adapter OpenAI ↔ Anthropic | API key | `ANTHROPIC_API_KEY` |
| OpenAI | OpenAI-compatible | API key | `OPENAI_API_KEY` |
| Mistral | OpenAI-compatible | API key | `MISTRAL_API_KEY` |
| DeepSeek | OpenAI-compatible | API key | `DEEPSEEK_API_KEY` |
| Google Gemini | OpenAI-compatible (via endpoint compat) | API key | `GEMINI_API_KEY` |
| OpenRouter | OpenAI-compatible | API key | `OPENROUTER_API_KEY` |
| Ollama | OpenAI-compatible | Sin auth | Auto-detect en localhost |

### Custom (via config)

Cualquier API que acepte el formato OpenAI `/v1/chat/completions` funciona — solo agregalo al config. Esto incluye:

- **OpenCode Zen / Go** — servicios oficiales de OpenCode
- **Together AI, Groq, Fireworks AI** — hosting de modelos open-source
- **Azure OpenAI** — con base_url custom
- **LM Studio, llama.cpp** — modelos locales
- **Cualquier otro** que hable OpenAI-compatible

## Cómo funciona

1. **Request llega** al proxy en formato OpenAI-compatible
2. **Chain Selector** elige la cadena de fallback (agente → grupo → global)
3. **Intenta cada proveedor** en orden, consultando el circuit breaker antes de cada intento
4. **Si el proveedor falla** (429, 500, 529, timeout, connection refused), registra el fallo y pasa al siguiente
5. **El primero que responde** envía la respuesta de vuelta a OpenCode — transparente

### Errores que disparan fallback

| HTTP Code | Significado | Acción |
|-----------|-------------|--------|
| 429 | Rate limit / usage limit | Retriable → siguiente proveedor |
| 529 | Overloaded (Anthropic) | Retriable → siguiente proveedor |
| 500, 502, 503 | Server error | Retriable → siguiente proveedor |
| Timeout | Sin respuesta | Retriable → siguiente proveedor |
| Connection refused | Proveedor caído | Retriable → siguiente proveedor |
| 401, 403 | Auth inválido | Fatal → no reintentar este proveedor |
| 404 | Modelo no encontrado | Fatal → no reintentar este proveedor |

### Circuit Breaker

Cada proveedor tiene su propio circuit breaker:
- **3 fallos** en ventana de **1 minuto** → proveedor se marca como "open"
- Proveedor open se **skippea por 30 segundos** — ni siquiera intenta el request
- Después de 30s, hace un intento de prueba (half-open) para ver si se recuperó

## Desarrollo

```bash
go test ./...          # 280 tests
go test -cover ./...   # Tests con coverage
go vet ./...           # Verificar código
go build ./...         # Compilar
```

## Changelog

### v0.3.0

- **TUI Configurator**: interfaz visual con Bubbletea v2 para configurar cadenas de fallback
- **2 tabs**: Global (cadena por defecto) y Agents (lista de agentes desde opencode.json)
- **Chain editor**: 4 slots por agente, model picker con filtro fuzzy agrupado por proveedor
- **Agregar agentes manualmente** con la tecla `n`
- **Colores por proveedor**: identidad visual (Anthropic amber, OpenAI green, DeepSeek blue, etc.)
- **Provider status en slots**: `[available]`/`[offline]`/`[unknown]` al lado de cada modelo
- **Responsive layout**: 3 breakpoints, funciona en terminales de 50 a 200+ columnas
- **Paginación con scroll**: soporta 60+ agentes sin perder performance
- **Help contextual**: muestra solo las teclas relevantes para la pantalla actual
- **Model picker mejorado**: j/k filtran en vez de navegar (patrón fzf), flechas para navegar
- **Persistencia**: `Ctrl+S` guarda, `[unsaved]` indicator, confirmación al salir con cambios
- **Arquitectura limpia**: tui/ no importa proxy/, circuit/, fallback/ — DI estricta via Dependencies
- **+98 tests** (182 → 280)

### v0.2.0

- **GenericOpenAIProvider**: cualquier API OpenAI-compatible funciona con solo config, sin código nuevo
- **Nuevos proveedores**: auto-detección de Mistral, Google Gemini, OpenRouter
- **Clasificador de errores unificado**: `ClassifyGenericOpenAIError` cubre 95%+ de proveedores
- **buildRegistry config-driven**: eliminación del switch/case hardcodeado
- **-442 líneas** de código duplicado, **+29 tests** (153 → 182)

### v0.1.0

- MVP: proxy con fallback automático para Anthropic, OpenAI, DeepSeek, Ollama
- Circuit breaker por proveedor
- Adapter OpenAI ↔ Anthropic
- Auto-detección de proveedores por env vars
- Setup automático de `opencode.json`
- 153 tests

## Licencia

MIT
