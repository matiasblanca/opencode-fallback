# opencode-fallback

Proxy local en Go que provee resiliencia automática de proveedores LLM para agentes de coding en OpenCode. Cuando un proveedor falla, el proxy cambia al siguiente de forma transparente — sin intervención del usuario.

## Quick Start

```bash
go install github.com/matiasblanca/opencode-fallback/cmd/opencode-fallback@latest
opencode-fallback setup
opencode-fallback serve
```

## ¿Qué hace?

Si usás OpenCode con Anthropic, OpenAI, o DeepSeek, es probable que en algún momento te hayas encontrado con rate limits, timeouts, o errores 500 que frenan tu flujo de trabajo. La solución manual es cambiar de proveedor, pero eso interrumpe lo que estás haciendo.

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
       ┌─────┼─────┬──────────┐
       ▼     ▼     ▼          ▼
   Anthropic OpenAI DeepSeek Ollama
```

## Instalación

### go install

```bash
go install github.com/matiasblanca/opencode-fallback/cmd/opencode-fallback@latest
```

Requiere Go 1.24+.

### Binarios pre-compilados

Descargá el binario para tu plataforma desde [GitHub Releases](https://github.com/matiasblanca/opencode-fallback/releases). Disponible para Windows, macOS, y Linux.

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

Si no existe archivo de configuración, el proxy:

1. Escanea variables de entorno: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `DEEPSEEK_API_KEY`
2. Detecta Ollama en `localhost:11434`
3. Arma la cadena global automática: Anthropic → OpenAI → DeepSeek → Ollama

No necesitás configurar nada si ya tenés las API keys en tu entorno.

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

**Completo:**

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

### Cadenas de fallback

Las cadenas se resuelven con una cascada de 3 niveles — lo más específico gana:

1. **Agente** (`agents.sdd-apply`) — cadena para un agente específico
2. **Grupo** (`_groups.sdd-*`) — glob pattern que matchea múltiples agentes
3. **Global** (`_global`) — cadena por defecto para todos

## Providers soportados

| Proveedor | Formato | Auth | Estado |
|-----------|---------|------|--------|
| Anthropic | Adapter OpenAI ↔ Anthropic | API key | ✅ v0.1 |
| OpenAI | Nativo OpenAI-compatible | API key | ✅ v0.1 |
| DeepSeek | Nativo OpenAI-compatible | API key | ✅ v0.1 |
| Ollama | Nativo OpenAI-compatible | Sin auth | ✅ v0.1 |
| Google Gemini | — | — | 🔲 v0.2 |
| OpenRouter | — | — | 🔲 v0.2 |

## Cómo funciona

1. **Request llega** al proxy en formato OpenAI-compatible
2. **Chain Selector** elige la cadena de fallback (agente → grupo → global)
3. **Intenta cada proveedor** en orden, consultando el circuit breaker antes de cada intento
4. **Si el proveedor falla** (429, 500, timeout, connection refused), registra el fallo y pasa al siguiente
5. **El primero que responde** envía la respuesta de vuelta a OpenCode — transparente

El **circuit breaker** protege contra proveedores caídos: después de 3 fallos en 1 minuto, el proveedor se skippea por 30 segundos.

**Próximamente (v0.2):** Stream recovery (checkpoint + continuación cuando un stream se corta), TUI dashboard con estado de proveedores.

## Desarrollo

```bash
go test ./...          # Correr todos los tests
go test -cover ./...   # Tests con coverage
go vet ./...           # Verificar código
go build ./...         # Compilar
```

## Licencia

MIT
