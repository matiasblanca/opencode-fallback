/**
 * transform.ts — Request transformation wrapper for Anthropic OAuth.
 *
 * Reimplements the Claude Code impersonation transformations from
 * opencode-anthropic-auth in TypeScript. This is the single source of truth
 * for the proxy — it replaces the Go-side transformation code.
 *
 * Transformations:
 * - System prompt rewrite (Claude Code identity, sanitize OpenCode mentions)
 * - Tool name prefixing (mcp_PascalCase)
 * - CCH billing header computation
 * - Beta headers
 * - URL with ?beta=true
 */

import { createHash, randomBytes } from "node:crypto";

// ─── Constants ─────────────────────────────────────────────────────────

const CCH_SALT = "59cf53e54c78";
const CCH_VERSION = "2.1.87";
const CCH_ENTRYPOINT = "sdk-cli";
const CHAR_POSITIONS = [4, 7, 20];

const CLAUDE_CODE_IDENTITY =
  "You are a Claude agent, built on Anthropic's Claude Agent SDK.";

const STRUCTURED_OUTPUT_TOOL = "StructuredOutput";
const TOOL_PREFIX = "mcp_";

const OAUTH_BETA = "oauth-2025-04-20";
const THINKING_BETA = "interleaved-thinking-2025-05-14";
const CLAUDE_CODE_USER_AGENT = "claude-cli/2.1.87 (external, cli)";

const ANTHROPIC_API_VERSION = "2023-06-01";
const ANTHROPIC_BASE_URL = "https://api.anthropic.com";

// ─── Types ─────────────────────────────────────────────────────────────

interface SystemBlock {
  type: "text";
  text: string;
}

interface OAuthData {
  access: string;
  refresh: string;
  expires: number;
}

export interface TransformResult {
  body: string;
  headers: Record<string, string>;
  url: string;
}

// ─── CCH Billing Header ───────────────────────────────────────────────

function computeCCH(message: string): string {
  const hash = createHash("sha256").update(message).digest("hex");
  return hash.slice(0, 5);
}

function extractChars(message: string): string {
  return CHAR_POSITIONS.map((pos) =>
    pos < message.length ? message[pos] : "0"
  ).join("");
}

function computeVersionSuffix(message: string): string {
  const chars = extractChars(message);
  const input = CCH_SALT + chars + CCH_VERSION;
  const hash = createHash("sha256").update(input).digest("hex");
  return hash.slice(0, 3);
}

function computeBillingHeader(firstUserMessage: string): string {
  const cch = computeCCH(firstUserMessage);
  const suffix = computeVersionSuffix(firstUserMessage);
  return `cc_version=${CCH_VERSION}.${suffix}; cc_entrypoint=${CCH_ENTRYPOINT}; cch=${cch};`;
}

// ─── System Prompt ────────────────────────────────────────────────────

function sanitizeSystemPrompt(prompt: string): string {
  // Split into paragraphs.
  const paragraphs = prompt.split("\n\n");
  const kept: string[] = [];

  for (const para of paragraphs) {
    const trimmed = para.trimStart();

    // Remove paragraphs starting with "You are OpenCode".
    if (trimmed.startsWith("You are OpenCode")) {
      continue;
    }

    // Remove paragraphs containing OpenCode URLs.
    if (
      para.includes("github.com/anomalyco/opencode") ||
      para.includes("opencode.ai/docs")
    ) {
      continue;
    }

    kept.push(para);
  }

  let result = kept.join("\n\n");

  // Inline replacements.
  result = result.replaceAll(
    "if OpenCode honestly",
    "if the assistant honestly"
  );

  return result;
}

function transformSystem(
  original: string,
  billingHeader: string
): SystemBlock[] {
  const blocks: SystemBlock[] = [];

  // 1. Billing header block goes first.
  if (billingHeader) {
    blocks.push({
      type: "text",
      text: "x-anthropic-billing-header: " + billingHeader,
    });
  }

  // 2. Claude Code identity block.
  blocks.push({
    type: "text",
    text: CLAUDE_CODE_IDENTITY,
  });

  // 3. Sanitized original content.
  if (original) {
    const sanitized = sanitizeSystemPrompt(original);
    if (sanitized) {
      blocks.push({
        type: "text",
        text: sanitized,
      });
    }
  }

  return blocks;
}

// ─── Tool Name Prefixing ──────────────────────────────────────────────

function prefixToolName(name: string): string {
  if (name === STRUCTURED_OUTPUT_TOOL || !name) {
    return name;
  }
  return TOOL_PREFIX + name.charAt(0).toUpperCase() + name.slice(1);
}

function unprefixToolName(name: string): string {
  if (name === STRUCTURED_OUTPUT_TOOL) {
    return name;
  }
  if (!name.startsWith(TOOL_PREFIX)) {
    return name;
  }
  const unprefixed = name.slice(TOOL_PREFIX.length);
  if (!unprefixed) {
    return name;
  }
  return unprefixed.charAt(0).toLowerCase() + unprefixed.slice(1);
}

// ─── Extract First User Message ───────────────────────────────────────

function extractFirstUserMessage(messages: unknown[]): string {
  for (const msg of messages) {
    if (
      typeof msg !== "object" ||
      msg === null ||
      !("role" in msg) ||
      (msg as Record<string, unknown>).role !== "user"
    ) {
      continue;
    }

    const content = (msg as Record<string, unknown>).content;

    // Content can be a string.
    if (typeof content === "string") {
      return content;
    }

    // Content can be an array of blocks.
    if (Array.isArray(content)) {
      for (const block of content) {
        if (
          typeof block === "object" &&
          block !== null &&
          (block as Record<string, unknown>).type === "text" &&
          typeof (block as Record<string, unknown>).text === "string"
        ) {
          return (block as Record<string, string>).text;
        }
      }
    }

    return "";
  }

  return "";
}

// ─── Extract System String ────────────────────────────────────────────

function extractSystemString(body: Record<string, unknown>): string {
  const sys = body.system;
  if (!sys) return "";

  // String form.
  if (typeof sys === "string") return sys;

  // Array of blocks.
  if (Array.isArray(sys)) {
    const parts: string[] = [];
    for (const block of sys) {
      if (
        typeof block === "object" &&
        block !== null &&
        (block as Record<string, unknown>).type === "text" &&
        typeof (block as Record<string, unknown>).text === "string"
      ) {
        parts.push((block as Record<string, string>).text);
      }
    }
    return parts.join("\n\n");
  }

  return "";
}

// ─── Main Transformation ──────────────────────────────────────────────

/**
 * Transform an Anthropic request body with full Claude Code impersonation.
 *
 * Applies:
 * - System prompt rewrite (billing header + Claude Code identity + sanitized original)
 * - Tool name prefixing (mcp_PascalCase)
 * - CCH billing header injection
 *
 * @param body - Raw JSON string of the Anthropic Messages API request body
 * @param authData - OAuth data containing the access token
 * @returns TransformResult with transformed body, headers, and URL
 */
export function transformAnthropicRequest(
  body: string,
  authData: OAuthData
): TransformResult {
  const parsed = JSON.parse(body) as Record<string, unknown>;

  // 1. Extract original system prompt.
  const originalSystem = extractSystemString(parsed);

  // 2. Build billing header from messages.
  let billingHeader = "";
  if (Array.isArray(parsed.messages)) {
    const firstUserMsg = extractFirstUserMessage(
      parsed.messages as unknown[]
    );
    if (firstUserMsg) {
      billingHeader = computeBillingHeader(firstUserMsg);
    }
  }

  // 3. Transform system prompt.
  parsed.system = transformSystem(originalSystem, billingHeader);

  // 4. Prefix tool names in tool definitions.
  if (Array.isArray(parsed.tools)) {
    for (const tool of parsed.tools as Record<string, unknown>[]) {
      if (typeof tool.name === "string") {
        tool.name = prefixToolName(tool.name);
      }
    }
  }

  // 5. Prefix tool names in messages (tool_use and tool_result blocks).
  if (Array.isArray(parsed.messages)) {
    for (const msg of parsed.messages as Record<string, unknown>[]) {
      const content = msg.content;
      if (!Array.isArray(content)) continue;

      for (const block of content as Record<string, unknown>[]) {
        const blockType = block.type;
        if (blockType === "tool_use" || blockType === "tool_result") {
          if (typeof block.name === "string") {
            block.name = prefixToolName(block.name);
          }
        }
      }
    }
  }

  // 6. Build headers.
  const headers: Record<string, string> = {
    "content-type": "application/json",
    authorization: "Bearer " + authData.access,
    "anthropic-beta": [OAUTH_BETA, THINKING_BETA].join(","),
    "user-agent": CLAUDE_CODE_USER_AGENT,
    "anthropic-version": ANTHROPIC_API_VERSION,
  };

  // 7. Build URL.
  const url = ANTHROPIC_BASE_URL + "/v1/messages?beta=true";

  return {
    body: JSON.stringify(parsed),
    headers,
    url,
  };
}

/**
 * Strip tool name prefixes from a streaming SSE data line.
 *
 * Removes the mcp_ prefix from tool names in the JSON data.
 * Exception: StructuredOutput is never stripped.
 */
export function stripToolPrefix(data: string): string {
  return data.replace(
    /"name"\s*:\s*"mcp_([^"]+)"/g,
    (_match: string, name: string) => {
      // Never strip StructuredOutput.
      if (TOOL_PREFIX + name === TOOL_PREFIX + STRUCTURED_OUTPUT_TOOL) {
        return _match;
      }

      // Lowercase first character.
      const unprefixed = name.charAt(0).toLowerCase() + name.slice(1);
      return `"name": "${unprefixed}"`;
    }
  );
}
