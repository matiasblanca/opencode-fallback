/**
 * server.ts — Bridge HTTP server.
 *
 * Exposes endpoints for the Go proxy to obtain fresh auth tokens and
 * pre-transformed request bodies/headers. Binds to 127.0.0.1 ONLY.
 *
 * Endpoints:
 * - GET  /health                  — Health check (no auth)
 * - GET  /auth/:provider          — Get fresh auth tokens (bearer auth)
 * - POST /transform/anthropic     — Transform Anthropic request (bearer auth)
 * - POST /transform/strip-response — Strip tool prefixes (bearer auth, optional)
 */

import { createServer, type Server, type IncomingMessage, type ServerResponse } from "node:http";
import { transformAnthropicRequest, stripToolPrefix } from "./transform.js";

const VERSION = "0.1.0";

// ─── Types ─────────────────────────────────────────────────────────────

interface AuthClient {
  get(provider: string): Promise<AuthData | null>;
}

interface AuthData {
  type: "oauth" | "api";
  // OAuth fields
  access?: string;
  refresh?: string;
  expires?: number;
  // API fields
  key?: string;
}

interface BridgeServerOptions {
  port: number;
  token: string;
  authClient: AuthClient;
}

// ─── Helpers ──────────────────────────────────────────────────────────

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => chunks.push(chunk));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf-8")));
    req.on("error", reject);
  });
}

function jsonResponse(
  res: ServerResponse,
  statusCode: number,
  data: unknown
): void {
  const body = JSON.stringify(data);
  res.writeHead(statusCode, {
    "Content-Type": "application/json",
    "Content-Length": Buffer.byteLength(body),
  });
  res.end(body);
}

function parseUrl(raw: string | undefined): { path: string; segments: string[] } {
  const url = raw || "/";
  const qIndex = url.indexOf("?");
  const path = qIndex >= 0 ? url.slice(0, qIndex) : url;
  const segments = path.split("/").filter(Boolean);
  return { path, segments };
}

// ─── Server ───────────────────────────────────────────────────────────

export function createBridgeServer(options: BridgeServerOptions): Server {
  const { port, token, authClient } = options;

  const server = createServer(async (req: IncomingMessage, res: ServerResponse) => {
    const startTime = Date.now();
    const method = req.method || "GET";
    const { path, segments } = parseUrl(req.url);

    try {
      // ── Health check (no auth required) ──
      if (method === "GET" && path === "/health") {
        jsonResponse(res, 200, { status: "ok", version: VERSION });
        logRequest(method, path, 200, startTime);
        return;
      }

      // ── Auth check for all other endpoints ──
      if (!verifyToken(req, token)) {
        jsonResponse(res, 401, { error: "unauthorized" });
        logRequest(method, path, 401, startTime);
        return;
      }

      // ── GET /auth/:provider ──
      if (method === "GET" && segments[0] === "auth" && segments.length === 2) {
        const provider = segments[1];
        await handleGetAuth(res, provider, authClient);
        logRequest(method, path, 200, startTime);
        return;
      }

      // ── POST /transform/anthropic ──
      if (method === "POST" && path === "/transform/anthropic") {
        const body = await readBody(req);
        await handleTransformAnthropic(res, body, authClient);
        logRequest(method, path, 200, startTime);
        return;
      }

      // ── POST /transform/strip-response ──
      if (method === "POST" && path === "/transform/strip-response") {
        const body = await readBody(req);
        handleStripResponse(res, body);
        logRequest(method, path, 200, startTime);
        return;
      }

      // ── 404 ──
      jsonResponse(res, 404, { error: "not found" });
      logRequest(method, path, 404, startTime);
    } catch (err) {
      const message = err instanceof Error ? err.message : "internal error";
      // Never log token values — only log the error type/message.
      console.error(`[bridge] error handling ${method} ${path}: ${message}`);
      jsonResponse(res, 500, { error: "internal error" });
      logRequest(method, path, 500, startTime);
    }
  });

  // Bind to 127.0.0.1 ONLY — never expose to the network.
  server.listen(port, "127.0.0.1", () => {
    console.log(`[bridge] fallback bridge started on :${port}`);
  });

  return server;
}

// ─── Route Handlers ───────────────────────────────────────────────────

async function handleGetAuth(
  res: ServerResponse,
  provider: string,
  authClient: AuthClient
): Promise<void> {
  const authData = await authClient.get(provider);

  if (!authData) {
    jsonResponse(res, 404, { error: `no auth found for provider: ${provider}` });
    return;
  }

  if (authData.type === "oauth") {
    jsonResponse(res, 200, {
      type: "oauth",
      access: authData.access,
      expires: authData.expires,
    });
  } else if (authData.type === "api") {
    jsonResponse(res, 200, {
      type: "api",
      key: authData.key,
    });
  } else {
    jsonResponse(res, 400, { error: `unknown auth type` });
  }
}

async function handleTransformAnthropic(
  res: ServerResponse,
  body: string,
  authClient: AuthClient
): Promise<void> {
  // Get fresh auth for anthropic.
  const authData = await authClient.get("anthropic");
  if (!authData || authData.type !== "oauth" || !authData.access) {
    jsonResponse(res, 400, {
      error: "no OAuth auth found for anthropic",
    });
    return;
  }

  const result = transformAnthropicRequest(body, {
    access: authData.access,
    refresh: authData.refresh || "",
    expires: authData.expires || 0,
  });

  jsonResponse(res, 200, result);
}

function handleStripResponse(res: ServerResponse, body: string): void {
  let parsed: { data?: string };
  try {
    parsed = JSON.parse(body);
  } catch {
    jsonResponse(res, 400, { error: "invalid JSON body" });
    return;
  }

  if (typeof parsed.data !== "string") {
    jsonResponse(res, 400, { error: "missing 'data' field" });
    return;
  }

  const stripped = stripToolPrefix(parsed.data);
  jsonResponse(res, 200, { data: stripped });
}

// ─── Auth Verification ────────────────────────────────────────────────

function verifyToken(req: IncomingMessage, expected: string): boolean {
  const authHeader = req.headers.authorization;
  if (!authHeader) return false;

  const parts = authHeader.split(" ");
  if (parts.length !== 2 || parts[0] !== "Bearer") return false;

  // Constant-time comparison to prevent timing attacks.
  const provided = parts[1];
  if (provided.length !== expected.length) return false;

  let result = 0;
  for (let i = 0; i < provided.length; i++) {
    result |= provided.charCodeAt(i) ^ expected.charCodeAt(i);
  }
  return result === 0;
}

// ─── Logging ──────────────────────────────────────────────────────────

function logRequest(
  method: string,
  path: string,
  statusCode: number,
  startTime: number
): void {
  const elapsed = Date.now() - startTime;
  // Never log token values — only log paths and timing.
  console.log(`[bridge] ${method} ${path} ${statusCode} ${elapsed}ms`);
}
