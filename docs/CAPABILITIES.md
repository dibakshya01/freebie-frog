# What freebie-frog can & cannot do

Written honestly, before and during the build. If something here stops being true,
it's a bug — please report it.

## It can

- **Run an Anthropic-API coding agent against another provider.** It serves a
  `/v1/messages`-compatible endpoint; point your agent at it with
  `ANTHROPIC_BASE_URL`.
- **Translate correctly, including streaming.** Text, tool use (function calling),
  images, and multi-turn tool round-trips are translated both ways. The streaming
  path reconstructs the Anthropic SSE event sequence from provider chunks, including
  the tricky tool-call-argument fragments (`input_json_delta`).
- **Talk to the whole OpenAI-compatible world with one adapter** — NVIDIA NIM, Groq,
  DeepSeek, OpenRouter, Together, Mistral, and local servers (Ollama, llama.cpp,
  vLLM, LM Studio) — plus **native Gemini** and an **Anthropic passthrough**.
- **Route and fall back.** Map a model or tier (opus/sonnet/haiku/fable) to an
  ordered list of provider targets; on a retryable failure it tries the next.
- **Handle secrets reasonably.** OS keychain, encrypted file, or env — never logged.
- **Guard against SSRF** on outbound calls and bind to loopback by default.
- **Show a read-only local dashboard** (`/admin/`) of providers (keys masked),
  routing, and server info — Host-allowlisted against DNS rebinding.

## It cannot (by design or by limitation)

- **Exact token counts.** `count_tokens` and streamed usage are **estimates** —
  providers frequently omit usage, and tokenizers differ. Don't rely on exact parity.
- **Every Anthropic feature.** Prompt caching, Anthropic server-side tools
  (web_search/code_execution), and fine-grained `thinking` budgets have no general
  provider equivalent; they are dropped or approximated (per-adapter). Thinking/
  reasoning is surfaced when a provider exposes it (e.g. `reasoning_content`).
- **Guarantee provider parity.** Some providers ignore parameters, lack vision or
  tool support, or stream tool calls non-standardly. Quirks are handled best-effort
  and flagged; a given provider+model may still behave differently.
- **Transparently recover from a mid-stream provider failure.** Fallback to another
  provider only happens **before the first streamed event**. After streaming has
  started, a provider failure ends the response with an error event — it cannot be
  silently retried (the client already holds a partial message).
- **Make models free or unlimited.** It uses **your own** provider accounts and keys.
  Free-tier availability and limits are the providers', and change without notice.
  A realistic aggregate across free tiers is **tens to hundreds of millions of
  tokens per month**, bound by per-minute/day throttles and per-provider (often
  per-org, not per-key) limits — **not** "unlimited" or "1B+".
- **Evade provider limits.** It is single-account / bring-your-own-key by design. It
  does **not** implement multi-account rotation or cross-account quota pooling —
  those violate providers' terms (e.g. Groq's AUP).
- **Defend against prompt injection in your content.** A transparent proxy relays
  payloads; injection defense is the agent's and model's responsibility.
- **Serve as a multi-tenant security boundary.** It's a local, single-user tool.
  Network exposure is opt-in, token-gated, and at your own risk.
- **Edit configuration from the admin UI.** The dashboard is read-only in this
  release; change providers/routing in `config.json` and restart. (UI editing is a
  deliberate future enhancement — it keeps the local daemon's attack surface small.)
