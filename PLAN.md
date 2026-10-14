# freebie-frog — Build Plan

A local, Anthropic-API-compatible proxy that lets agent CLIs (Claude Code, Codex,
Aider, etc.) run against third-party / free-tier model providers, using your own keys.

**Status:** planning. **License (intended):** your choice (MIT/Apache-2.0 recommended — see §9).

---

## 0. Clean-room discipline (read first)

This project is an *independent* reimplementation of a well-known pattern (an
Anthropic↔OpenAI translating proxy). To keep it genuinely independent and free of
copyleft/derivative-work entanglement:

- **Build from public specs only**, never from another project's source:
  - Anthropic Messages API reference (request/response schema, SSE event stream).
  - OpenAI Chat Completions reference (request/response, streaming chunks, tool calls).
  - Each target provider's own public API docs.
- **Do not read, copy, paste, or "port"** code, file layouts, naming schemes, or
  comments from AGPL/GPL projects (e.g. free-claude-code). Functionality and API
  shapes are not copyrightable; specific code expression is.
- Keep a short `NOTICE`/`docs/SOURCES.md` listing the public specs you referenced.
- Pick your own architecture, language, module names, and license. Divergence here
  is what makes the result non-derivative — not obfuscation of someone else's code.

---

## 1. What it does (scope)

Core product = **one translating proxy**:

1. Serve an **Anthropic-Messages-API-compatible** HTTP endpoint locally.
2. Point an agent CLI at it (`ANTHROPIC_BASE_URL` + a dummy/local auth token).
3. Translate each request → target provider format, call provider with the user's key.
4. Translate the response back to Anthropic format, **including the exact SSE event
   sequence** the agent expects.
5. Route by model/tier with **fallback** across providers on failure.

Everything else (admin UI, multi-agent launchers, Discord/Telegram, token reducers)
is optional polish layered on top of that core.

---

## 2. Architecture

```
   agent CLI (Claude Code, Codex, Aider, ...)
        │  Anthropic Messages API (HTTP + SSE)
        ▼
 ┌─────────────────────────────────────────────┐
 │  freebie-frog server                         │
 │  ┌─────────────┐   ┌───────────────────────┐ │
 │  │ Ingress     │→  │ Router / fallback      │ │
 │  │ /v1/messages│   │  (model→target chain)  │ │
 │  │ count_tokens│   └──────────┬────────────┘ │
 │  └─────────────┘              ▼               │
 │                     ┌───────────────────────┐ │
 │                     │ Provider adapter       │ │
 │                     │  req→provider / resp←  │ │
 │                     │  stream translate (SSE)│ │
 │                     └──────────┬────────────┘ │
 │   config + secrets store, logging, usage      │
 └────────────────────────────────┼─────────────┘
                                   ▼
             provider API (OpenAI-compatible / native)
```

### 2.1 Ingress (Anthropic-compatible surface)
- `POST /v1/messages` — streaming and non-streaming.
- `POST /v1/messages/count_tokens` — token estimation (approximate is fine; use a
  tokenizer lib or a heuristic, documented as approximate).
- `GET /v1/models` — optional, advertise mapped model names.
- `GET /healthz`, admin endpoints.
- Accept `x-api-key` / `Authorization: Bearer` but treat them as **local** auth only;
  real provider keys live in the secrets store.

### 2.2 Request translation (Anthropic → provider)
Map these Anthropic fields: `model`, `system` (string **or** content blocks),
`messages[]` with content blocks (`text`, `image`, `tool_use`, `tool_result`,
`thinking`), `tools[]`, `tool_choice`, `max_tokens` (required by Anthropic),
`temperature`, `top_p`, `stop_sequences`, `metadata`, `stream`.

To OpenAI Chat Completions:
- `system` → leading `role:"system"` message (join blocks).
- Content blocks → OpenAI content parts; images → `image_url` with base64 data URIs.
- `tools` (Anthropic JSON-schema tool) → OpenAI `tools:[{type:"function",...}]`.
- Assistant `tool_use` block → assistant message `tool_calls[]`.
- `tool_result` block → `role:"tool"` message, matched by `tool_call_id`.
- Param mapping incl. `max_tokens`/`max_completion_tokens`; carry `stop_sequences`→`stop`.
- **Round-trip tool-call IDs** carefully — mismatches break the agent loop.

### 2.3 Response translation (provider → Anthropic)
Non-streaming → build an Anthropic `Message`:
- `content[]` = text block(s) + `tool_use` block(s) (parse tool-call args JSON).
- `stop_reason` map: `stop→end_turn`, `length→max_tokens`, `tool_calls→tool_use`,
  content filter → `end_turn` (documented).
- `usage` = `{input_tokens, output_tokens}` from provider usage (or estimate).

### 2.4 Streaming translation (the hard, critical part)
Reconstruct the Anthropic SSE sequence from OpenAI `chat.completion.chunk` deltas:
```
message_start
  content_block_start (index 0, text)         ← when first text delta arrives
  content_block_delta (text_delta ...) *
  content_block_stop
  content_block_start (index N, tool_use)     ← when a tool_call appears
  content_block_delta (input_json_delta ...) *  ← tool args streamed as partial JSON
  content_block_stop
message_delta  (stop_reason, usage)
message_stop
(ping events interspersed)
```
Key gotchas:
- OpenAI streams tool-call **arguments as partial JSON strings**; they MUST be
  emitted as `input_json_delta`, or Claude Code's agent loop stalls.
- Track open content-block indices; open/close blocks as text vs tool-call deltas
  interleave. A single assistant turn can mix text + multiple tool calls.
- Emit `message_start` before you have usage; fill final usage in `message_delta`.
- Handle provider SSE quirks: `[DONE]` sentinels, keep-alive lines, chunked usage.
- Map `thinking`/reasoning deltas → `thinking_delta` when the provider exposes them.

Build this against a **golden/snapshot test** of the exact byte sequence.

### 2.5 Provider adapters (extensibility core)
Adapter interface (conceptual):
```
to_request(anthropic_req)        -> provider_req
from_response(provider_resp)     -> anthropic_message        # non-stream
stream(provider_stream)          -> iterator[anthropic_sse]  # stream
capabilities()                   -> {tools, images, thinking, max_ctx, ...}
```
Ship:
- **Generic OpenAI-compatible adapter** — covers the long tail in one shot:
  NVIDIA NIM, DeepSeek, OpenRouter, Groq, Together, Mistral, Fireworks, and local
  servers (Ollama, llama.cpp, vLLM, LM Studio). Config = base URL + auth + model list.
- **Google Gemini native** (optional) — different message/tool schema.
- **Anthropic passthrough** (optional) — forward to a real Anthropic-compatible host.

Each adapter declares quirks (tool support, system-prompt handling, unsupported
params to strip, context window) so the router can route around limitations.

### 2.6 Router + fallback engine
- Config maps **incoming model name / tier → ordered list of `provider/model`**.
- Fallback triggers: 429, 5xx, timeout, context-length-exceeded, auth error.
- Exponential backoff; per-provider circuit breaker; cap attempts; surface a clean
  Anthropic-format error if the whole chain fails.
- Model aliasing: the agent sends fixed model IDs — map them to real targets.
- Per-agent profiles (Claude Code vs Aider may want different chains).

### 2.7 Config + secrets
- Config file (TOML or YAML): providers, model map, fallback chains, profiles.
- Secrets: env vars, OS keychain, or an encrypted-at-rest file; **never log keys**.
- Hot reload on config change (nice-to-have).

### 2.8 Launcher / CLI UX
- `freebie serve` — start the proxy.
- `freebie run <agent>` — launch the agent with `ANTHROPIC_BASE_URL`/token pointed at
  the local proxy (and any agent-specific env).
- `freebie config` / `freebie doctor` (diagnostics) / `freebie update`.
- Installer script later; if you do `curl | sh`, document it and offer a verifiable
  alternative (checksum, versioned release) — `curl | sh` is a real security smell.

### 2.9 Observability
- Structured logs (redact secrets), per-request trace id, usage/latency metrics,
  optional request/response capture behind a debug flag.

---

## 3. Tech stack options (decide before scaffolding)

| Option | Pros | Cons |
|---|---|---|
| **Go** (recommended) | single static binary, trivial distribution, great for proxies/SSE, fast | more boilerplate for JSON shaping |
| **TypeScript/Node** | huge ecosystem, colocate admin UI, fast to build | runtime + node_modules to ship |
| **Python + FastAPI/httpx** | fastest to prototype, rich libs | packaging/distribution friction; closest to FCC's stack |
| **Rust** | max perf, single binary | slowest to build, steeper |

Recommendation: **Go** for a distributable single-binary proxy, or **TypeScript** if
you want the admin UI tightly integrated early.

---

## 4. Milestones

**M0 — Skeleton**
- Repo scaffold, chosen stack, config loader, `/healthz`, structured logging, CI.

**M1 — MVP: Claude Code ↔ one OpenAI-compatible provider (non-stream)**
- `/v1/messages` non-streaming, generic OpenAI adapter, basic model map.
- Round-trip a real Claude Code request end-to-end against one free provider.

**M2 — Streaming (the make-or-break)**
- Full SSE translation incl. `input_json_delta` tool streaming.
- Golden snapshot tests; verify Claude Code's tool loop works live.

**M3 — Tools, images, multi-turn**
- tool_use/tool_result round-trip, image input, multi-turn correctness, count_tokens.

**M4 — Router + fallback**
- Model/tier mapping, ordered fallback chains, backoff, circuit breaker, per-agent profiles.

**M5 — More providers + more agents**
- Add Gemini native; validate Codex/Aider/Cline against the proxy; provider quirk flags.

**M6 — Admin UI + packaging**
- Local web UI (keys, model map, usage/logs), installer, releases, docs.

**M7 — Optional extras**
- Token-reduction helpers, notifications/remote control, voice, etc.

---

## 5. Testing strategy
- **Unit:** each translation direction (req→provider, provider→resp) with table tests.
- **Snapshot:** exact SSE byte sequence for representative streams (text, tool call,
  mixed, error, thinking).
- **Mock provider:** an in-process fake OpenAI server to run full E2E without network.
- **Live smoke (gated):** optional tests against real free-tier providers, off by default.
- **Agent-in-the-loop:** scripted Claude Code session asserting a tool call completes.

---

## 6. Key technical risks
1. **Streaming tool-call translation** — the #1 source of "it half-works." Budget most
   of M2 here; snapshot-test it.
2. **Tool-call ID round-tripping** across multi-turn — mismatches silently break loops.
3. **Provider drift** — free tiers change models/limits; keep adapters config-driven
   and quirk-flagged.
4. **Token counting** — no exact parity; document as approximate.
5. **Context-window mismatches** — map/trim and fall back on overflow.

---

## 7. Repo layout (illustrative, stack-agnostic)
```
freebie-frog/
  cmd/ or src/        # server + CLI entrypoints
  internal/
    ingress/          # Anthropic API surface
    translate/        # req/resp/stream translation
    adapters/         # openai_compatible, gemini, anthropic_passthrough
    router/           # model map + fallback
    config/           # config + secrets
    obs/              # logging/metrics
  web/                # admin UI (later)
  test/               # unit, snapshot, mock-provider, e2e
  docs/               # SOURCES.md, provider notes, usage
```

---

## 8. Naming / branding
- Keep the neutral name (`freebie-frog`). Do **not** use "Claude"/"Anthropic" in the
  name or imply affiliation. Add a "not affiliated with or endorsed by Anthropic/
  OpenAI/etc." disclaimer in the README.

---

## 9. Legal / compliance checklist
- **License:** your own (MIT or Apache-2.0 for permissive). Valid *only* because the
  code is clean-room, not derived from AGPL sources.
- **No copied code** from AGPL/GPL projects (see §0). Keep `docs/SOURCES.md`.
- **Trademark:** no misleading names/branding (see §8).
- **Provider ToS:** respect each provider's terms and rate limits; don't build
  features whose purpose is circumventing free-tier limits (multi-account rotation to
  evade quotas, etc.). Route-and-fallback on *your own* keys is the supported path.
- **Secrets:** never log/transmit user keys anywhere except the chosen provider.
- **Installer:** avoid blind `curl | sh`; ship checksummed, versioned releases.
```
```
```
