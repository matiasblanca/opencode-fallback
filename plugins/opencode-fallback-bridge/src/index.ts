/**
 * index.ts — OpenCode plugin entry point for opencode-fallback-bridge.
 *
 * When loaded by OpenCode, this plugin:
 * 1. Generates a random bearer token for proxy ↔ bridge authentication
 * 2. Writes the token to <XDG_DATA_HOME>/opencode/fallback-bridge-token
 * 3. Starts an HTTP server on localhost:18787 (configurable via FALLBACK_BRIDGE_PORT)
 * 4. Exposes endpoints for auth token retrieval and request transformation
 *
 * The plugin follows OpenCode's plugin contract: it exports a default function
 * that receives PluginInput and returns PluginOutput.
 */

import { randomBytes } from "node:crypto";
import { writeFileSync, mkdirSync, unlinkSync } from "node:fs";
import { join, dirname } from "node:path";
import { homedir } from "node:os";
import type { Server } from "node:http";
import { createBridgeServer } from "./server.js";

// ─── Types ─────────────────────────────────────────────────────────────

/**
 * OpenCode plugin input — matches the contract used by opencode-anthropic-auth.
 * The client provides auth management and other SDK utilities.
 */
interface PluginInput {
  client: PluginClient;
}

interface PluginClient {
  auth: {
    get(provider: string): Promise<AuthData | null>;
    set(provider: string, data: AuthData): Promise<void>;
  };
}

interface AuthData {
  type: "oauth" | "api";
  access?: string;
  refresh?: string;
  expires?: number;
  key?: string;
  accountId?: string;
  enterpriseUrl?: string;
}

interface PluginOutput {
  name: string;
  cleanup?: () => void | Promise<void>;
}

// ─── Path Resolution ──────────────────────────────────────────────────

/**
 * Returns the OpenCode data directory path.
 * Uses OPENCODE_DATA_DIR if set, otherwise platform-specific XDG paths.
 */
function getOpenCodeDataDir(): string {
  if (process.env.OPENCODE_DATA_DIR) {
    return process.env.OPENCODE_DATA_DIR;
  }

  switch (process.platform) {
    case "win32": {
      const local = process.env.LOCALAPPDATA || join(homedir(), "AppData", "Local");
      return join(local, "opencode");
    }
    case "darwin":
      return join(homedir(), "Library", "Application Support", "opencode");
    default: {
      // Linux and others
      const xdg = process.env.XDG_DATA_HOME || join(homedir(), ".local", "share");
      return join(xdg, "opencode");
    }
  }
}

/**
 * Returns the path where the bridge token file is written.
 */
function getTokenFilePath(): string {
  return join(getOpenCodeDataDir(), "fallback-bridge-token");
}

// ─── Plugin Entry Point ───────────────────────────────────────────────

export default function plugin(input: PluginInput): PluginOutput {
  const port = parseInt(process.env.FALLBACK_BRIDGE_PORT || "18787", 10);

  // Generate a random bearer token (32 bytes, hex encoded = 64 chars).
  const token = randomBytes(32).toString("hex");

  // Write the token to disk so the proxy can read it.
  const tokenPath = getTokenFilePath();
  try {
    mkdirSync(dirname(tokenPath), { recursive: true });
    writeFileSync(tokenPath, token, { mode: 0o600 });
    console.log(`[bridge] token written to ${tokenPath}`);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    console.error(`[bridge] failed to write token file: ${message}`);
    // Continue anyway — the proxy will report the bridge as unavailable.
  }

  // Start the bridge HTTP server.
  let server: Server | null = null;
  try {
    server = createBridgeServer({
      port,
      token,
      authClient: {
        get: (provider: string) => input.client.auth.get(provider),
      },
    });
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    console.error(`[bridge] failed to start server: ${message}`);
  }

  return {
    name: "opencode-fallback-bridge",
    cleanup: () => {
      // Close the HTTP server gracefully.
      if (server) {
        return new Promise<void>((resolve) => {
          server!.close(() => {
            console.log("[bridge] server stopped");
            resolve();
          });
        });
      }

      // Clean up the token file.
      try {
        unlinkSync(tokenPath);
      } catch {
        // Ignore — file may already be gone.
      }
    },
  };
}
