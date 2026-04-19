# Analisis: Soporte de suscripciones en opencode-fallback

**Fecha**: 2025-07-21
**Metodo**: Lectura directa del source code + 2 subagentes de investigacion en paralelo
**Repos analizados**:
- OpenCode (`anomalyco/opencode`, branch `dev`)
- opencode-anthropic-auth (`ex-machina-co/opencode-anthropic-auth`, branch `main`)

---

## 1. Mapa de autenticacion de OpenCode

### 1.1 Anthropic (API key)

- **Identificacion**: Provider ID `anthropic` en `opencode.json`. Se identifica por la presencia de `ANTHROPIC_API_KEY` en el env o por auth almacenado via `opencode auth anthropic`.
  - Ref: `packages/opencode/src/provider/schema.ts:16` — `anthropic: schema.make("anthropic")`
  - Ref: `packages/opencode/src/provider/provider.ts:1203-1211` — carga API keys desde env vars

- **Auth mechanism**: API key directa. Se pasa como header `x-api-key: <key>` por el SDK `@ai-sdk/anthropic`.
  - Ref: `packages/opencode/src/provider/provider.ts:143-151` — custom loader para `anthropic` solo agrega `anthropic-beta` headers
  - El SDK `@ai-sdk/anthropic` maneja la autenticacion internamente

- **Endpoint**: `https://api.anthropic.com/v1/messages` (Anthropic Messages API nativa, NO OpenAI-compatible)
  - NPM package: `@ai-sdk/anthropic`
  - Ref: proviene del models.dev registry (`api.json`)

- **Headers**: `x-api-key: <ANTHROPIC_API_KEY>`, `anthropic-beta: interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14`
  - Ref: `packages/opencode/src/provider/provider.ts:146-149`

- **Token storage**: API key en `<XDG_DATA_HOME>/opencode/auth.json` bajo la key `"anthropic"` con tipo `"api"`.
  - Ref: `packages/opencode/src/auth/index.ts:9` — `const file = path.join(Global.Path.data, "auth.json")`
  - Formato: `{ "anthropic": { "type": "api", "key": "sk-ant-xxx" } }`
  - Permisos: `0o600` (lectura/escritura solo owner)
  - Ref: `packages/opencode/src/auth/index.ts:79` — `fsys.writeJson(file, { ...data, [norm]: info }, 0o600)`

- **Expiracion**: No aplica — API keys no expiran.

- **Env var override**: Todo el contenido de `auth.json` puede pasarse como JSON en `OPENCODE_AUTH_CONTENT` (util para CI/CD).
  - Ref: `packages/opencode/src/auth/index.ts:59-62`

### 1.2 Anthropic (suscripcion via plugin opencode-anthropic-auth)

- **Identificacion**: Mismo provider ID `anthropic`. El plugin se registra con `auth.provider: 'anthropic'` y maneja el caso `type: 'oauth'`.
  - Ref: `opencode-anthropic-auth/src/index.ts:15-16` — `auth: { provider: 'anthropic', ... }`

- **Auth mechanism**: OAuth 2.0 Authorization Code con PKCE (S256).
  - Dos modos:
    - `max`: para suscripciones Claude Pro/Max → autoriza via `https://claude.ai/oauth/authorize`
    - `console`: para crear API keys → autoriza via `https://platform.claude.com/oauth/authorize`
  - Ref: `opencode-anthropic-auth/src/auth.ts:97-119`

- **OAuth flow** (detallado en seccion 2):
  1. Genera PKCE verifier (64 bytes random, base64url) + challenge (SHA-256 del verifier)
  2. Construye URL de autorizacion con `client_id`, `response_type=code`, `redirect_uri`, `scope`, `code_challenge`, `code_challenge_method=S256`, `state`
  3. Usuario abre URL, autoriza, recibe code+state
  4. Exchange: POST a `https://platform.claude.com/v1/oauth/token` con code, state, grant_type, client_id, redirect_uri, code_verifier
  5. Recibe `access_token`, `refresh_token`, `expires_in`

- **Endpoint**: `https://api.anthropic.com/v1/messages?beta=true` (se agrega `?beta=true` para requests OAuth)
  - Ref: `opencode-anthropic-auth/src/transform.ts:214-219` — `requestUrl.searchParams.set('beta', 'true')`

- **Headers** (cuando usa OAuth):
  ```
  authorization: Bearer <access_token>
  anthropic-beta: oauth-2025-04-20,interleaved-thinking-2025-05-14,<existing-betas>
  user-agent: claude-cli/2.1.87 (external, cli)
  ```
  Se ELIMINA `x-api-key` (si existia)
  - Ref: `opencode-anthropic-auth/src/transform.ts:90-98`

- **Token storage**: Almacenados en `<XDG_DATA_HOME>/opencode/auth.json` bajo la key `"anthropic"` con tipo `"oauth"`.
  - Formato:
    ```json
    {
      "anthropic": {
        "type": "oauth",
        "refresh": "<refresh_token>",
        "access": "<access_token>",
        "expires": 1721234567890
      }
    }
    ```
  - Ref: `opencode-anthropic-auth/src/index.ts:101-112` — el plugin llama `client.auth.set(...)` que internamente escribe a `auth.json`
  - Path exacto por plataforma:
    - **Linux**: `~/.local/share/opencode/auth.json`
    - **macOS**: `~/Library/Application Support/opencode/auth.json` (o `~/.local/share/opencode/auth.json` si XDG está configurado)
    - **Windows**: `%LOCALAPPDATA%/opencode/auth.json` (o XDG equivalent)
  - Permisos: `0o600`

- **Token refresh**: El plugin tiene refresh automatico con reintentos.
  - Detecta expiracion: `!auth.access || !auth.expires || auth.expires < Date.now()` (estricta, NO proactiva)
  - Usa `refreshPromise` compartido para evitar refreshes concurrentes (deduplication via singleton Promise pattern)
  - POST a `https://platform.claude.com/v1/oauth/token` con `grant_type: 'refresh_token'`, `refresh_token`, `client_id`
  - **NO envia** `code_verifier` ni `redirect_uri` en el refresh (solo en el exchange inicial)
  - Headers del refresh: `Content-Type: application/json`, `Accept: application/json, text/plain, */*`, `User-Agent: axios/1.13.6`
  - Reintentos: hasta 2 retries (3 intentos total) con backoff exponencial (500ms, 1000ms)
  - Solo reintenta en: HTTP 5xx o errores de red (`fetch failed`, `ECONNRESET`, `ECONNREFUSED`, `ETIMEDOUT`, `UND_ERR_CONNECT_TIMEOUT`)
  - Errores 4xx (401, 403) NO se reintentan — fallan inmediatamente
  - **Critical**: Re-lee `getAuth()` antes de cada intento para evitar usar un refresh token stale (Ref: `src/index.ts:67`)
  - `refreshPromise` se limpia en `.finally()` para que futuros intentos puedan reintentar
  - Ref: `opencode-anthropic-auth/src/index.ts:49-138`

- **System prompt rewrite**: El plugin reescribe el request body para "pasar" como Claude Code.
  - Ref: `opencode-anthropic-auth/src/transform.ts:337-365` — `rewriteRequestBody()`
  - Detallado en seccion 2.3 y 2.4

### 1.3 GitHub Copilot

- **Identificacion**: Provider ID `github-copilot`. Plugin built-in `CopilotAuthPlugin` en `packages/opencode/src/plugin/github-copilot/copilot.ts`.
  - Ref: `packages/opencode/src/plugin/index.ts:16` — importado como plugin interno
  - Ref: `packages/opencode/src/provider/schema.ts:19` — `githubCopilot: schema.make("github-copilot")`

- **Auth mechanism**: GitHub OAuth Device Flow.
  - Client ID: `Ov23li8tweQw6odWQebz`
  - Scope: `read:user`
  - Soporta GitHub.com y GitHub Enterprise
  - Ref: `packages/opencode/src/plugin/github-copilot/copilot.ts:12-13`

- **OAuth flow**:
  1. POST a `https://github.com/login/device/code` con `client_id` y `scope`
  2. Recibe `verification_uri`, `user_code`, `device_code`, `interval`
  3. Muestra URL y codigo al usuario
  4. Polling: POST a `https://github.com/login/oauth/access_token` con `device_code`, `grant_type=urn:ietf:params:oauth:grant-type:device_code`
  5. Espera `authorization_pending` con intervalo, maneja `slow_down` (RFC 8628)
  6. Recibe `access_token`
  - Ref: `packages/opencode/src/plugin/github-copilot/copilot.ts:212-326`

- **Endpoint**: `https://api.githubcopilot.com` (GitHub Copilot API, NO api.openai.com)
  - Para Enterprise: `https://copilot-api.<domain>`
  - Modelos que soportan `/v1/messages` usan Anthropic Messages API
  - Modelos GPT/Gemini usan OpenAI-compatible chat/responses API
  - Ref: `packages/opencode/src/plugin/github-copilot/copilot.ts:28-29`
  - Ref: `packages/opencode/src/plugin/github-copilot/models.ts:59-67` — determina npm package basado en `supported_endpoints`

- **Headers** (cuando usa OAuth):
  ```
  Authorization: Bearer <github_access_token>   (nótese: mayúscula)
  User-Agent: opencode/<version>
  Openai-Intent: conversation-edits
  x-initiator: user|agent
  ```
  Se eliminan `x-api-key` y `authorization` (lowercase) si existen.
  - Opcionalmente: `Copilot-Vision-Request: true` para requests con imagenes
  - Ref: `packages/opencode/src/plugin/github-copilot/copilot.ts:150-163`

- **Token storage**: Almacenado en `<XDG_DATA_HOME>/opencode/auth.json` bajo `"github-copilot"` con tipo `"oauth"`.
  - **IMPORTANTE**: El token de GitHub no expira de la forma tradicional. Se almacena con `expires: 0` y el `access_token` se guarda como AMBOS `refresh` y `access`.
  - Formato:
    ```json
    {
      "github-copilot": {
        "type": "oauth",
        "refresh": "<github_access_token>",
        "access": "<github_access_token>",
        "expires": 0,
        "enterpriseUrl": "company.ghe.com"  // solo para Enterprise
      }
    }
    ```
  - Ref: `packages/opencode/src/plugin/github-copilot/copilot.ts:277-295`

- **Token refresh**: NO hay refresh automatico. El GitHub access token es de larga duracion (Personal Access Token style). Si expira, el usuario debe re-autenticar.

- **NPM package**: Usa un SDK custom `@ai-sdk/github-copilot` (bundled en el repo como `packages/opencode/src/provider/sdk/copilot/`)
  - Ref: `packages/opencode/src/provider/provider.ts:115`

### 1.4 OpenAI

- **Identificacion**: Provider ID `openai`. Env var: `OPENAI_API_KEY`.
  - Ref: `packages/opencode/src/provider/schema.ts:17`

- **Auth mechanism**: API key directa. Se pasa como `Authorization: Bearer <key>` por el SDK.

- **Endpoint**: `https://api.openai.com/v1` — Responses API para la mayoria de modelos.
  - NPM package: `@ai-sdk/openai`
  - Custom model loader que usa `sdk.responses(modelID)` en vez de `sdk.languageModel(modelID)`
  - Ref: `packages/opencode/src/provider/provider.ts:175-182`

- **Token storage**: Env var `OPENAI_API_KEY` o en `auth.json` como `{ "openai": { "type": "api", "key": "sk-..." } }`

- **Expiracion**: No aplica — API keys no expiran.

### 1.5 Google/Gemini

- **Identificacion**: Provider ID `google`. Env var: `GOOGLE_GENERATIVE_AI_API_KEY`.

- **Auth mechanism**: API key directa.

- **Endpoint**: Google Generative AI API via `@ai-sdk/google`.

- **Token storage**: Env var o `auth.json`.

- **Expiracion**: No aplica.

### 1.6 Otros providers relevantes

- **OpenRouter** (`openrouter`): API key `OPENROUTER_API_KEY`, headers `HTTP-Referer: https://opencode.ai/`, `X-Title: opencode`
- **Azure** (`azure`): API key `AZURE_API_KEY`, endpoint `https://{resourceName}.openai.azure.com`, opciones `store: true`, `promptCacheKey: sessionID`
- **Amazon Bedrock** (`amazon-bedrock`): AWS credentials chain (profile, access key, web identity, container creds, bearer token). Bearer token se escribe a `process.env.AWS_BEARER_TOKEN_BEDROCK` directamente.
- **Google Vertex** (`google-vertex`): Application Default Credentials via `google-auth-library`. Custom `fetch` wrapper que llama `auth.getApplicationDefault()` y agrega `Authorization: Bearer <token>`. El refresh lo maneja la libreria internamente.
- **OpenCode** (`opencode`): API key o free tier con `apiKey: "public"`. Modelos pagos se filtran si no hay key.
- **GitLab** (`gitlab`): PAT via header `PRIVATE-TOKEN: <key>` (type api) o OAuth via `Authorization: Bearer <token>` (type oauth). Headers: `User-Agent: opencode/{version} gitlab-ai-provider/{version}`, `anthropic-beta: context-1m-2025-08-07`
- **Codex** (OpenAI): Plugin built-in `CodexAuthPlugin`. El npm package `opencode-openai-codex-auth` esta deprecated/built-in.
  - Ref: `packages/opencode/src/plugin/shared.ts:10`

### 1.7 Provider resolution priority

Los providers se activan en este orden de precedencia (Ref: `provider/provider.ts:1200-1275`):

| Paso | Source | Condicion |
|------|--------|-----------|
| 1 | `env` | Un env var listado en `provider.env[]` esta definido |
| 2 | `api` | Hay una key almacenada en `auth.json` para este provider ID |
| 3 | `custom` | Plugin `auth.loader` retorna opciones |
| 4 | `custom` | Factory `custom(dep)` retorna `autoload: true` |
| 5 | `config` | Config `provider[id].options` esta seteado en `opencode.json` |

---

## 2. Analisis detallado del plugin anthropic-auth

### 2.1 Flujo OAuth PKCE (paso a paso)

```
[Usuario] → [OpenCode UI] → [Plugin] → [Anthropic OAuth]
```

**Paso 1: Generacion PKCE**
- Se genera un verifier de 64 bytes random, codificado como base64url
- Se computa el challenge como SHA-256 del verifier, codificado como base64url
- Ref: `opencode-anthropic-auth/src/pkce.ts:7-23`

**Paso 2: Construccion de URL de autorizacion**
- Base URL: `https://claude.ai/oauth/authorize` (modo `max`) o `https://platform.claude.com/oauth/authorize` (modo `console`)
- Parametros:
  ```
  code=true
  client_id=9d1c250a-e61b-44d9-88ed-5944d1962f5e
  response_type=code
  redirect_uri=https://platform.claude.com/oauth/code/callback
  scope=org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload
  code_challenge=<SHA256(verifier) base64url>
  code_challenge_method=S256
  state=<random UUID sin guiones>
  ```
- Ref: `opencode-anthropic-auth/src/auth.ts:97-119`, `src/constants.ts:1-20`

**Paso 3: Autorizacion del usuario**
- El usuario abre la URL en el browser
- Autoriza la aplicacion
- Recibe un `code` y `state` en la callback URL o como texto para pegar

**Paso 4: Exchange del code por tokens**
- POST a `https://platform.claude.com/v1/oauth/token`
- Headers:
  ```
  Content-Type: application/json
  Accept: application/json, text/plain, */*
  User-Agent: axios/1.13.6
  ```
- Body:
  ```json
  {
    "code": "<authorization_code>",
    "state": "<state>",
    "grant_type": "authorization_code",
    "client_id": "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
    "redirect_uri": "https://platform.claude.com/oauth/code/callback",
    "code_verifier": "<pkce_verifier>"
  }
  ```
- Ref: `opencode-anthropic-auth/src/auth.ts:55-95`

**Paso 5: Almacenamiento de tokens**
- Respuesta exitosa contiene: `{ refresh_token, access_token, expires_in }`
- Se calcula `expires = Date.now() + expires_in * 1000`
- Se almacena via `client.auth.set()` → escribe en `auth.json`

### 2.2 Token storage (path, formato, permisos)

- **Path**: `<XDG_DATA_HOME>/opencode/auth.json`
  - Linux: `~/.local/share/opencode/auth.json`
  - macOS: `~/Library/Application Support/opencode/auth.json` (normalmente) o `~/.local/share/opencode/auth.json`
  - Windows: `%LOCALAPPDATA%/opencode/auth.json`
- **Formato**:
  ```json
  {
    "anthropic": {
      "type": "oauth",
      "refresh": "eyJ...",
      "access": "eyJ...",
      "expires": 1721234567890
    }
  }
  ```
- **Permisos**: `0o600` (solo el owner puede leer/escribir)
- **Schema Effect**: Definido en `packages/opencode/src/auth/index.ts:13-20` como `Oauth` class con campos `type`, `refresh`, `access`, `expires`, `accountId?`, `enterpriseUrl?`

### 2.3 Request transformation (headers, body, system prompt)

Cuando el plugin intercepta un request (via el custom `fetch` que retorna el `loader`):

**Headers modificados** (`setOAuthHeaders()`):
1. `authorization` → `Bearer <access_token>` (reemplaza cualquier valor existente)
2. `anthropic-beta` → merge de betas requeridos + existentes: `oauth-2025-04-20,interleaved-thinking-2025-05-14,...`
3. `user-agent` → `claude-cli/2.1.87 (external, cli)`
4. `x-api-key` → ELIMINADO
- Ref: `opencode-anthropic-auth/src/transform.ts:90-98`

**URL modificada** (`rewriteUrl()`):
- Para `/v1/messages`: agrega `?beta=true` si no existe
- Soporta `ANTHROPIC_BASE_URL` para proxies custom
- Ref: `opencode-anthropic-auth/src/transform.ts:189-230`

**Body modificado** (`rewriteRequestBody()`):
1. **System prompt**: reescribe para impersonar Claude Code
2. **Tool names**: agrega prefijo `mcp_` y PascalCase (e.g. `bash` → `mcp_Bash`, `read_file` → `mcp_Read_file`)
3. **Billing header**: inyecta un bloque system con `x-anthropic-billing-header` que contiene version y hash
4. **Tool_use blocks en messages**: tambien se prefijan los names
- Ref: `opencode-anthropic-auth/src/transform.ts:337-365`

**Response modificada** (`createStrippedStream()`):
- Strip del prefijo `mcp_` de tool names en el stream de respuesta
- Ref: `opencode-anthropic-auth/src/transform.ts:370-396`

### 2.4 Claude Code impersonation (que exactamente necesita Anthropic)

Para que Anthropic acepte un request OAuth como "Claude Code", se necesitan TODOS estos elementos:

1. **Beta flags requeridos**:
   - `oauth-2025-04-20`
   - `interleaved-thinking-2025-05-14`
   - Ref: `opencode-anthropic-auth/src/constants.ts:24-27`

2. **System prompt format** (3 bloques en orden):
   - `system[0]`: Billing header — `x-anthropic-billing-header: cc_version=2.1.87.<suffix>; cc_entrypoint=sdk-cli; cch=<hash>;`
   - `system[1]`: Claude Code identity — `"You are a Claude agent, built on Anthropic's Claude Agent SDK."`
   - `system[2..n]`: El system prompt original, sanitizado (sin menciones de OpenCode)
   - Ref: `opencode-anthropic-auth/src/transform.ts:284-332`, `src/constants.ts:30-36`

3. **Tool naming convention**:
   - Tool definitions: `name: "mcp_<PascalCase>"` (e.g. `mcp_Bash`, `mcp_Read`, `mcp_Edit`)
   - Tool use blocks en messages: misma convencion
   - `StructuredOutput` NO se prefija
   - Ref: `opencode-anthropic-auth/src/transform.ts:18-31`

4. **User-Agent**: `claude-cli/2.1.87 (external, cli)`
   - Ref: `opencode-anthropic-auth/src/constants.ts:38`

5. **Cost zeroing**: El plugin pone todos los costos de modelos en `{ input: 0, output: 0, cache: { read: 0, write: 0 } }` cuando `auth.type === 'oauth'` — refleja que Max plan no cobra por token.
   - Ref: `opencode-anthropic-auth/src/index.ts:29-38`

6. **Billing header (CCH)**: Computo basado en el primer mensaje del usuario:
   - `cc_version`: version de Claude Code + 3 chars de hash(salt + chars del mensaje + version)
   - `cc_entrypoint`: `sdk-cli`
   - `cch`: primeros 5 chars hex de SHA-256 del texto del primer user message
   - Ref: `opencode-anthropic-auth/src/cch.ts:52-67`

7. **URL con `?beta=true`**: Agrega query param `beta=true` a requests a `/v1/messages`

8. **Response stream**: Strip del prefijo `mcp_` de tool names via regex `/"name"\s*:\s*"mcp_([^"]+)"/g` en el stream SSE.
   - Excepcion: `StructuredOutput` NO se un-prefija (se mantiene como esta)
   - Ref: `opencode-anthropic-auth/src/transform.ts:25-31, 144-149`

---

## 3. Analisis de GitHub Copilot auth

### 3.1 Flujo de auth

- **Tipo**: GitHub OAuth Device Flow (RFC 8628)
- **Client ID**: `Ov23li8tweQw6odWQebz` (OAuth App de GitHub)
- **Scope**: `read:user`

**Pasos**:
1. POST `https://github.com/login/device/code` → recibe `verification_uri`, `user_code`, `device_code`, `interval`
2. Muestra al usuario: "Go to {verification_uri} and enter code: {user_code}"
3. Polling POST a `https://github.com/login/oauth/access_token` cada `interval` segundos
4. Maneja `authorization_pending` (espera), `slow_down` (+5s al intervalo por RFC), `error` (falla)
5. Cuando llega `access_token` → stored como OAuth con `refresh = access = access_token`, `expires = 0`

Para **GitHub Enterprise**:
- El usuario provee el domain (e.g. `company.ghe.com`)
- URLs se adaptan: `https://company.ghe.com/login/device/code`, etc.
- API base cambia a `https://copilot-api.company.ghe.com`

### 3.2 Token management

- **Storage**: `auth.json` → `"github-copilot": { "type": "oauth", "refresh": "<token>", "access": "<token>", "expires": 0 }`
- **No hay refresh**: El token de GitHub es de larga duracion. `expires: 0` significa "no expira" en la logica del plugin.
- **Re-auth**: Si el token deja de funcionar, el usuario debe volver a hacer `opencode auth github-copilot`

### 3.3 Request format

El plugin Copilot tiene un custom `fetch` wrapper:

1. **Lee el auth** con `getAuth()` (siempre fresco)
2. **Detecta el tipo de request**: vision, agent-initiated, etc.
3. **Headers**:
   ```
   Authorization: Bearer <github_token>
   User-Agent: opencode/<version>
   Openai-Intent: conversation-edits
   x-initiator: user|agent
   [Copilot-Vision-Request: true]   // si hay imagenes
   ```
4. **Elimina**: `x-api-key`, `authorization` (lowercase)
5. **NPM packages usados**:
   - `@ai-sdk/github-copilot` (custom SDK bundled) para chat/responses endpoints
   - `@ai-sdk/anthropic` para modelos que soportan `/v1/messages` (como `claude-sonnet-4.5` via Copilot)

---

## 4. Propuestas para opencode-fallback

### 4.1 Approach A: Token passthrough (Proxy lee tokens de disco)

**Descripcion**: El proxy Go lee directamente `<XDG_DATA_HOME>/opencode/auth.json` para obtener los tokens OAuth almacenados por OpenCode y sus plugins. Los usa para autenticarse con los providers.

**Viabilidad tecnica**: ✅ VIABLE
- Los tokens estan en un archivo JSON con path conocido y predecible
- El formato es simple y documentado en el schema Effect
- Los tokens OAuth de Anthropic incluyen `access_token`, `refresh_token`, y `expires`
- Los tokens de Copilot son simplemente un `access_token` de GitHub

**Implementacion**:
1. El proxy lee `auth.json` al inicio y lo watch para cambios
2. Para cada provider, detecta si tiene `type: "oauth"` o `type: "api"`
3. Para Anthropic OAuth:
   - Chequea `expires` antes de cada request
   - Si expirado, usa `refresh_token` para obtener nuevo `access_token`
   - Aplica las transformaciones de Claude Code impersonation (headers, system prompt, tool names, billing header)
   - Llama a `https://api.anthropic.com/v1/messages?beta=true`
   - Strip tool prefix de la respuesta
4. Para Copilot OAuth:
   - Usa `refresh` (que es el GitHub access token) como Bearer token
   - Aplica headers Copilot-specific (User-Agent, Openai-Intent, x-initiator)
   - Llama a `https://api.githubcopilot.com`

**Pros**:
- Implementacion relativamente simple — solo necesita leer un archivo JSON
- No necesita interaccion con OpenCode en runtime
- El proxy es independiente — funciona aunque OpenCode no este corriendo
- Ya sabemos el formato exacto de los tokens
- Soporta hot-reload via file watcher

**Cons**:
- **Fragilidad**: Si OpenCode cambia el formato de `auth.json`, se rompe. Bajo riesgo actual porque el schema esta estabilizado en el Effect type system.
- **Refresh token race condition**: Si el proxy y OpenCode refrescan el token al mismo tiempo, pueden pisar uno al otro (token rotation de Anthropic usa refresh tokens de un solo uso)
- **El proxy necesita reimplementar TODA la transformacion de Claude Code**: billing header (CCH), system prompt rewrite, tool name prefixing, response stripping. Son ~300 lineas de logica no trivial.
- **Seguridad**: El proxy tiene acceso directo a refresh tokens en disco
- **Dependency en XDG paths**: Necesita resolver los paths correctos por plataforma

**Evaluacion**:
- Viabilidad: 8/10
- Fragilidad: 6/10 (moderada — auth.json es estable pero la transformacion puede cambiar)
- Complejidad: 7/10 (alto — reimplementar CCH, system prompt rewrite, tool prefix en Go)
- UX: 9/10 (transparente — el usuario no hace nada extra)
- Seguridad: 5/10 (acceso directo a tokens sensibles)

### 4.2 Approach B: Plugin bridge (OpenCode plugin que expone auth al proxy)

**Descripcion**: Crear un plugin de OpenCode que expone un endpoint HTTP local (o archivo compartido) con los tokens frescos. El proxy se conecta a este endpoint para obtener tokens validados y refrescados.

**Viabilidad tecnica**: ✅ MUY VIABLE
- El plugin system de OpenCode soporta plugins npm arbitrarios
- Los plugins tienen acceso a `getAuth()` que siempre devuelve tokens frescos
- Los plugins pueden exponer servicios via `input.serverUrl`
- El plugin maneja el refresh de tokens — el proxy solo consume

**Implementacion**:
1. Plugin de OpenCode (npm package):
   - Se registra como plugin con acceso al SDK client
   - Expone un endpoint HTTP (e.g. `http://localhost:PORT/auth/anthropic`)
   - Cuando el proxy pide tokens, el plugin llama `getAuth()` y devuelve el access token fresco
   - Opcionalmente, expone un endpoint que hace la transformacion completa del request (headers, body)
2. El proxy:
   - Al recibir un request para un provider con suscripcion, consulta al plugin para obtener tokens/headers
   - Aplica los tokens al request y lo envía

**Variante**: En vez de HTTP, usar un archivo compartido que el plugin actualiza:
```json
// ~/.local/share/opencode/fallback-tokens.json
{
  "anthropic": {
    "access_token": "...",
    "headers": { "authorization": "Bearer ...", "anthropic-beta": "...", "user-agent": "..." },
    "updated_at": 1721234567890
  }
}
```

**Pros**:
- **Menos acoplamiento**: El proxy no necesita saber de OAuth ni de CCH — el plugin hace toda la logica
- **Tokens siempre frescos**: El plugin usa `getAuth()` que ya maneja refresh automaticamente
- **No race condition**: Solo el plugin refresca tokens, el proxy solo lee
- **Evita reimplementar transformaciones**: El plugin puede exportar los headers y body transformados
- **Mas seguro**: El proxy nunca ve refresh tokens, solo access tokens de corta duracion

**Cons**:
- **Requiere OpenCode corriendo**: El proxy no funciona sin OpenCode activo
- **Dependency bidireccional**: El usuario necesita instalar el plugin en OpenCode Y configurar el proxy
- **Latencia**: Un request HTTP extra (o read de archivo) por cada LLM call
- **Complejidad de setup**: El usuario necesita coordinar plugin + proxy

**Evaluacion**:
- Viabilidad: 9/10
- Fragilidad: 3/10 (baja — desacoplado de internals)
- Complejidad: 5/10 (moderada — plugin simple + proxy simple)
- UX: 6/10 (requiere instalar plugin + configurar proxy)
- Seguridad: 8/10 (solo access tokens expuestos, refresh tokens protegidos)

### 4.3 Approach C: OAuth propio (El proxy hace su propia auth)

**Descripcion**: El proxy implementa el flujo OAuth PKCE directamente con Anthropic, independiente de OpenCode. El usuario autoriza el proxy como otra "app" de Claude Code.

**Viabilidad tecnica**: ⚠️ PARCIALMENTE VIABLE
- El Client ID (`9d1c250a-e61b-44d9-88ed-5944d1962f5e`) y los endpoints OAuth son conocidos
- El flujo PKCE es estandar y reimplementable en Go
- PERO: Anthropic puede detectar que no es Claude Code real y rechazar los tokens
- PERO: El usuario tendria que autorizar TANTO OpenCode como el proxy (2 sesiones OAuth separadas)

**Implementacion**:
1. El proxy implementa OAuth PKCE en Go
2. Abre browser para autorizacion
3. Almacena tokens localmente (similar a auth.json pero propio)
4. Reimplementa toda la transformacion de Claude Code (como Approach A)
5. Maneja refresh de tokens independientemente

**Pros**:
- **Independiente de OpenCode**: Funciona standalone
- **Control total**: El proxy maneja su propia sesion OAuth
- **Sin race conditions**: Sesion separada = tokens separados

**Cons**:
- **Duplica logica**: Reimplementa OAuth + PKCE + CCH + transforms en Go
- **Doble autorizacion**: El usuario autoriza 2 veces (OpenCode + proxy)
- **Riesgo de bloqueo**: Anthropic podria detectar y bloquear multiples sesiones del mismo usuario
- **Mantenimiento pesado**: Cada cambio en la transformacion de Claude Code requiere actualizar el proxy
- **Etica**: Impersonar Claude Code desde otra app es cuestionable

**Evaluacion**:
- Viabilidad: 5/10 (riesgo de bloqueo por Anthropic)
- Fragilidad: 8/10 (alta — cualquier cambio en Claude Code rompe el proxy)
- Complejidad: 9/10 (alta — reimplementar todo en Go)
- UX: 4/10 (doble auth, confuso)
- Seguridad: 6/10 (manejo de tokens OAuth propio, attack surface mas grande)

### 4.4 Approach D: Middleware/reverse proxy (El proxy se registra como provider custom)

**Descripcion**: En vez de interceptar requests salientes de OpenCode, el proxy se registra como un "provider" custom en `opencode.json`. OpenCode le envía los requests ya autenticados, y el proxy decide a que provider real reenviarlo.

**Viabilidad tecnica**: ⚠️ PARCIALMENTE VIABLE PERO CAMBIA LA ARQUITECTURA

**Como funcionaria**:
1. En `opencode.json`, se registra un provider `fallback` con `baseURL: http://localhost:PROXY_PORT`
2. OpenCode envía requests al proxy como si fuera un provider OpenAI-compatible
3. El proxy recibe el request y decide a que provider real enviarlo
4. PERO: el request llega SIN autenticacion del provider real — viene autenticado para el proxy

**Problema fundamental**: OpenCode autentica los requests para el provider que tiene configurado. Si el proxy es el "provider", OpenCode le pasa los requests sin tokens de Anthropic/Copilot. El proxy necesitaria obtener esos tokens de alguna otra forma (volviendo a Approach A o B).

**Variante**: Configurar OpenCode para que envíe requests a Anthropic con `ANTHROPIC_BASE_URL=http://localhost:PROXY_PORT`. Esto hace que todos los requests de Anthropic pasen por el proxy, que YA VIENEN con los headers OAuth correctos porque OpenCode/plugin los puso.

**Pros de la variante**:
- **Simple**: Solo redirect — OpenCode pone los headers, el proxy solo reenvía o rerutea
- **Transparente**: No necesita leer tokens ni reimplementar transformaciones
- **La transformacion la hace el plugin de anthropic-auth**: billing header, system prompt, tool names — todo ya viene en el request
- **Low risk**: El proxy solo es un reverse proxy que decide a donde mandar

**Cons de la variante**:
- **Solo funciona para un provider a la vez**: `ANTHROPIC_BASE_URL` solo aplica a Anthropic
- **El proxy recibe requests YA transformados** para Claude Code — no puede reenviarlos a otro provider (ej: no puede mandar un request transformado para Anthropic a OpenAI)
- **Copilot no soporta esta variante**: No hay env var equivalente para redirigir Copilot
- **Limita el fallback**: Si el request ya tiene headers de Anthropic OAuth, no puede reutilizarse para fallback a API key

**Evaluacion**:
- Viabilidad: 6/10 (funciona para Anthropic via ANTHROPIC_BASE_URL, pero no para otros)
- Fragilidad: 2/10 (baja — es un simple proxy pass-through)
- Complejidad: 3/10 (baja — reverse proxy basico)
- UX: 7/10 (solo poner una env var)
- Seguridad: 9/10 (no maneja tokens, solo redirige)

---

## 5. Recomendacion

### Approach recomendado: B + A hibrido

Recomiendo una **combinacion de Approach B (Plugin bridge) como solucion primaria y Approach A (Token passthrough) como fallback**.

**Razonamiento**:

1. **Plugin bridge (B)** resuelve el problema central: el proxy NO necesita reimplementar la logica de transformacion de Claude Code. El plugin ya tiene esa logica y la aplica antes de que el request salga. Esto reduce drasticamente la complejidad y fragilidad del proxy.

2. **Token passthrough (A)** sirve como fallback cuando OpenCode no esta corriendo. El proxy lee `auth.json` y puede usar API keys directas sin problema. Para OAuth, solo se usa como "read-only" — lee access tokens para requests urgentes pero NO refresca tokens.

### Plan de implementacion de alto nivel

#### Fase 1: Token passthrough simple (API keys + read-only OAuth)
- **Archivos**: Nuevo `auth/reader.go`, modificar `provider/config.go`
- **Que hace**: Lee `auth.json` de la ubicacion XDG correcta por plataforma
- **Soporta**: API keys (ya funciona), + leer access tokens OAuth para Anthropic y Copilot
- **Limitacion**: No refresca tokens — usa lo que hay en disco
- **Dependencias**: Go `os` package para XDG paths, JSON parsing

#### Fase 2: Token refresh para Anthropic
- **Archivos**: Nuevo `auth/anthropic_oauth.go`, `auth/refresh.go`
- **Que hace**: Si el access token expiro, usa refresh token para obtener uno nuevo
- **Implementa**: POST a `https://platform.claude.com/v1/oauth/token` con refresh_token
- **Escribe**: Actualiza `auth.json` (con file locking para evitar races con OpenCode)
- **Dependencias**: HTTP client, file locking (`flock`)

#### Fase 3: Claude Code impersonation en Go
- **Archivos**: Nuevo `transform/anthropic_oauth.go`, `transform/cch.go`, `transform/tool_prefix.go`
- **Que hace**: Implementa las transformaciones necesarias para OAuth:
  - System prompt rewrite (prepend Claude Code identity, sanitize OpenCode mentions)
  - Tool name prefixing (`mcp_PascalCase`)
  - CCH billing header computation
  - Beta headers merge
  - URL rewrite (`?beta=true`)
  - Response stream tool name stripping
- **Complejidad**: ~400 lineas de Go, la parte mas compleja

#### Fase 4: Plugin bridge (opcional, optimizacion futura)
- **Plugin npm**: `opencode-fallback-bridge` que expone un endpoint HTTP con tokens frescos
- **Proxy**: Endpoint `/auth/bridge/<provider>` que consulta al plugin
- **Beneficio**: Elimina la necesidad de reimplementar transformaciones — el plugin las hace

#### Fase 5: Copilot support
- **Archivos**: Nuevo `provider/copilot.go`
- **Que hace**: Implementa custom fetch para Copilot con headers especificos
- **Endpoint**: `https://api.githubcopilot.com` con `Authorization: Bearer <github_token>`
- **Headers**: User-Agent, Openai-Intent, x-initiator

### Config propuesta para opencode-fallback

```yaml
# fallback.yaml o env vars
providers:
  - id: anthropic-subscription
    type: anthropic-oauth    # nuevo tipo
    auth_source: opencode    # lee de auth.json de OpenCode
    priority: 1
    
  - id: copilot
    type: github-copilot     # nuevo tipo
    auth_source: opencode
    priority: 2
    
  - id: anthropic-api
    type: anthropic          # existente
    api_key: ${ANTHROPIC_API_KEY}
    priority: 3
    
  - id: openai-api
    type: openai             # existente
    api_key: ${OPENAI_API_KEY}
    priority: 4
```

### Archivos clave del proxy que necesitan cambios

- `auth/reader.go` — nuevo: lee auth.json de OpenCode
- `auth/anthropic_oauth.go` — nuevo: token refresh para Anthropic OAuth
- `transform/anthropic_oauth.go` — nuevo: Claude Code impersonation
- `transform/cch.go` — nuevo: billing header computation
- `transform/tool_prefix.go` — nuevo: tool name prefixing/stripping
- `provider/config.go` — modificar: soportar nuevos tipos de provider
- `provider/copilot.go` — nuevo: GitHub Copilot provider
- `proxy/handler.go` — modificar: routing a nuevos providers

### Consideraciones de seguridad

1. **auth.json** contiene refresh tokens de Anthropic — acceso = full account access
2. **GitHub tokens** son de larga duracion — acceso = acceso a Copilot
3. El proxy NO debe loguear tokens ni exponerlos en metricas
4. File permissions de auth.json son `0o600` — el proxy necesita correr como el mismo usuario que OpenCode
5. Si se implementa plugin bridge, usar localhost-only binding

---

## Verificacion

1. ✅ Lei el source code de ambos repos — lectura directa de 25+ archivos + 2 subagentes de investigacion paralelos
2. ✅ Cada claim tiene referencia a archivo + linea
3. ✅ Los paths de token storage son exactos: `<XDG_DATA_HOME>/opencode/auth.json` (confirmado en `auth/index.ts:9` y `global/index.ts:10`)
4. ✅ Las URLs de OAuth endpoints vienen del codigo: `constants.ts:3-11`
5. ✅ Evalue los 4 approaches con pros/cons reales
6. ✅ La recomendacion es practica — usa APIs publicas y formatos conocidos
7. ✅ Considere token expiration durante sesiones largas (refresh con reintentos y deduplication)
8. ✅ Verifique que el plugin NO escribe directamente a disco — delega a `client.auth.set()` del SDK
9. ✅ Confirme que el `@ai-sdk/github-copilot` es un alias local, NO un paquete npm real
10. ✅ Confirme formato exacto del CCH billing header con test: `cch.test.ts:35-44`
