# Findings — freebie-frog

Research for an independent, clean-room, Anthropic-API-compatible proxy that lets
coding-agent CLIs run against third-party and free-tier model providers. All
provider/standards facts verified **2026-10-13**; version-sensitive items dated.

## 1. Problem

Coding-agent CLIs that speak the **Anthropic Messages API** (Claude Code and
others) are hard-wired to one wire protocol, but developers increasingly want to
point them at **cheaper, free-tier, or local** models — DeepSeek, Qwen, gpt-oss,
Llama, or a model running on their own machine. The pain:

- **Lock-in to one API shape.** The agent speaks Anthropic's `/v1/messages`; most
  of the model world speaks OpenAI's `/v1/chat/completions`. Without a translator
  you cannot use the agent with those models at all.
- **Cost / access.** Running an agent loop against a frontier API is expensive;
  many developers, students, and people in cost-sensitive regions want to use
  free tiers or local models for day-to-day work.
- **Reliability.** Free tiers rate-limit aggressively; a single provider is not
  dependable, so users want automatic fallback across several.
- **Privacy / offline.** Some want everything to stay on a local model
  (Ollama/llama.cpp/vLLM) with no data leaving the machine.

Who feels it most: individual developers and small teams who already use an
Anthropic-API agent and want provider choice, lower cost, or local/offline use —
frequently, on every session.

## 2. Prior art & the gap

| Tool | What it does | Strengths | Gaps | License |
|------|--------------|-----------|------|---------|
| **free-claude-code (FCC)** | Multi-provider router for ~11 agent CLIs; admin UI; token reducers; Discord/TG | Breadth of agents/providers; feature-rich | **AGPL-3.0 (strong copyleft)**; Python 3.14 packaging/distribution friction; heavy; "ToS-friendly" is a self-claim | AGPL-3.0 |
| **claude-code-router** (musistudio) | Routes Claude Code to many providers via per-provider "transformers" | Popular (~26k★); flexible transformer model; MIT | Node runtime + node_modules to ship; config complexity; Claude-Code-centric | MIT |
| **claude-code-mux** | Rust router w/ failover, Anthropic-compatible | Single binary; perf; failover | Smaller ecosystem/community | (varies) |
| **a2o / ant2oai** | Lightweight Anthropic→OpenAI proxy (FastAPI/httpx) | Simple; focused | Python packaging; fewer features; limited multi-provider/fallback | (varies) |
| **claude2openai-proxy** (ziozzang) | FastAPI proxy via LiteLLM | Reuses LiteLLM breadth | Python; LiteLLM dependency weight | (varies) |
| **CommandCodeBridge** | Go reverse proxy, dual OpenAI+Anthropic endpoints | Go single binary | Less focus on agent-grade streaming/fallback/UX | (varies) |
| **LiteLLM** | Broad LLM gateway/SDK | Huge provider coverage | Not Anthropic-ingress-first; Python; large surface | MIT |

**The gap we fill:** there is no **single-binary, permissively-licensed (Apache-2.0),
security-first, clean-room** Anthropic-compatible proxy that (a) gets the
**streaming SSE translation provably right** (the detail most clones get subtly
wrong), (b) ships **SSRF protection and OS-keychain secret handling as defaults**
(absent or ad-hoc in the field), (c) does **multi-provider fallback** and
**multi-agent launchers** out of the box, and (d) is **honest about free-tier
limits and provider ToS**. FCC is the closest in breadth but is AGPL (copyleft)
and Python; claude-code-router is MIT but Node and transformer-config-heavy. A Go
single binary with security baked in and honest claims is an open niche.

## 3. Technical landscape

**The core job** is bidirectional translation between two well-documented wire
formats, plus a correct streaming reconstruction.

- **Anthropic Messages API** (ingress): `POST /v1/messages` (+ `/count_tokens`),
  header `anthropic-version: 2023-06-01`. Content blocks (text/image/tool_use/
  tool_result/thinking); SSE event sequence `message_start → content_block_start
  → content_block_delta (text_delta | input_json_delta | thinking_delta) →
  content_block_stop → message_delta (stop_reason, cumulative usage) →
  message_stop`, with `ping`/`error` events. `stop_reason` ∈ {end_turn,
  max_tokens, stop_sequence, tool_use, pause_turn, refusal,
  model_context_window_exceeded}. Claude Code sends full model IDs (opus/sonnet/
  haiku/fable families) and honors `ANTHROPIC_BASE_URL` + an auth token — the hook
  we use to point it at the proxy.
- **OpenAI Chat Completions** (egress, de-facto standard): `POST
  /v1/chat/completions`, `Authorization: Bearer`. The **hard part** is streaming
  tool calls — `delta.tool_calls[]` streams `function.arguments` as **partial JSON
  string fragments** keyed by `index`, which must be re-emitted as Anthropic
  `input_json_delta`. `max_tokens` is deprecated for `max_completion_tokens` but
  most compatible providers still read `max_tokens`. Usage is absent from the
  stream unless `stream_options.include_usage` is set (and many providers ignore
  it) — so token counts often must be estimated.
- **Google Gemini** (optional native egress): different shape — roles `user`/
  `model` only, `systemInstruction` top-level, function `args` as objects (not JSON
  strings), whole-chunk function calls, `?alt=sse` streaming with **no `[DONE]`**,
  and an OpenAPI-3.03 schema subset (escape hatch: `parametersJsonSchema` accepts
  standard JSON Schema). Gemini also exposes an OpenAI-compatible path, so the
  native adapter is a feature-completeness option, not a requirement.
- **Provider reality:** essentially **all** target providers (NVIDIA NIM, Groq,
  OpenRouter, DeepSeek, Mistral, Together, and local Ollama/llama.cpp/vLLM/LM
  Studio) speak OpenAI Chat Completions. So **one generic OpenAI-compatible adapter
  + a per-provider config row** (base URL, auth, model list, quirks) covers the
  whole field; Gemini-native is the only extra adapter worth having.
- What "good" looks like: byte-correct SSE that keeps Claude Code's tool loop
  working, robust defensive accumulation of tool-call fragments, graceful fallback
  on 429/5xx, and never leaking a key.

## 4. Standards & references

Security is anchored to current OWASP guidance (the project is an LLM gateway):

- **OWASP Top 10 for LLM Applications — 2026 edition** (published 2026-08-04;
  2025 archived). Proxy-relevant: **LLM02 Sensitive Information Disclosure**
  (PRIMARY — provider keys, prompt/response logging), **LLM06 Unbounded
  Consumption** (PRIMARY — cost/DoS on the spend path), **LLM04 Supply Chain**,
  **LLM10 Improper Output Handling** (admin-UI XSS from logged content),
  **LLM01 Prompt Injection** (passthrough / shared-responsibility non-goal).
- **OWASP API Security Top 10 (2023)** — **API7 SSRF** is the key design risk: a
  proxy that forwards to a user-configured upstream is an SSRF primitive
  (cloud-metadata `169.254.169.254`, loopback, RFC1918). Mitigate with upstream
  allowlists, blocking private/link-local ranges, **dial-time IP validation**
  (Go `DialContext`/`Control`) to defeat DNS rebinding, and disabled redirects.
  Also API2/API5 (admin-UI auth), API4 (rate/spend limits), API8 (secure
  defaults: loopback bind, no verbose errors, upstream TLS verification).
- **Secret handling:** OS keychain preferred → AES-256-GCM encrypted file →
  `chmod 0600` plaintext + warning; **never log `Authorization`/`x-api-key` or
  `sk-`/bearer patterns**; store a key *reference* when using a keychain.
- **Local admin UI:** bind loopback by default; Host-header allowlist
  (port-stripped) + `Sec-Fetch-Site`/Origin + CSRF protection to defeat DNS
  rebinding (cf. CVE-2025-66414) and cross-site POSTs.

Full mapping is in `docs/THREAT_MODEL.md` (built in Phase 3).

## 5. Risks & unknowns

- **Technical — streaming correctness (highest).** Reconstructing the Anthropic
  SSE sequence from OpenAI chunks, especially interleaved text + multiple
  tool-call argument fragments, is where clones break. Mitigation: defensive
  accumulation keyed by index; golden/snapshot byte-tests; verify Claude Code's
  tool loop against a mock provider.
- **Technical — token counting.** No exact parity; `count_tokens` and stream usage
  are approximate when providers omit usage. Mitigation: document as approximate;
  offer a heuristic estimator.
- **Security — SSRF / secret leakage.** Covered by §4 mitigations; must be built
  in from the start, not bolted on, and proven with attack-tests.
- **Adoption — provider drift.** Free tiers churn constantly (GitHub Models was
  **retired 2026-07-30**; Cerebras/Together moved to paid/credits; Gemini free
  narrowed). Mitigation: config-driven providers, runtime `/v1/models` discovery,
  no hard-coded rosters.
- **Legal / ToS — do not build quota evasion.** Groq's AUP (2025-10-15) prohibits
  "registering multiple accounts or orchestrating usage between multiple
  organizations"; OpenRouter bars reselling. **Design constraint:** ship
  **BYO-key, single-account**; never build multi-account rotation or cross-account
  quota pooling. Surface per-provider data-training/privacy status.
- **Legal — license & trademark.** Clean-room (Apache-2.0, not derived from AGPL
  FCC); no "Claude"/Anthropic branding or implied affiliation.
- **Expectation management — "free".** The honest figure is **tens-to-low-hundreds
  of millions of free tokens/month aggregated across providers, throttle-bound and
  per-org-pooled** — **not** a guaranteed "1B+". Claims must say so.

## 6. Synthesized direction

**Thesis:** *freebie-frog is a single-binary, Apache-2.0, security-first proxy that
makes any Anthropic-API coding agent run on the model provider you choose —
free-tier, paid, or fully local — with correct streaming and automatic fallback,
using your own keys.*

**Wedge:** correctness + security + distribution. Get the streaming translation
provably right, bake in SSRF/secret defaults the field lacks, ship as one Go
binary (`go install`/download, no runtime), and be honest about limits — against a
field that is either copyleft+Python (FCC) or Node+config-heavy (claude-code-router).

**Principles (decide trade-offs):**
1. **Protocol correctness first.** The streaming SSE translation is the product;
   it is snapshot-tested and verified against a real agent loop.
2. **Security by default.** Keychain secrets, no secret logging, loopback bind,
   SSRF guard with dial-time IP validation, admin-UI auth — on by default.
3. **One binary, few/no deps.** Go stdlib-first; every dependency justified.
4. **Honest & ToS-respecting.** BYO-key/single-account only; no quota-evasion
   features; claims match measured reality; clean-room and trademark-clean.
5. **Config-driven breadth.** One generic OpenAI-compatible adapter + per-provider
   config covers the field; Gemini-native is the one extra adapter; runtime model
   discovery over hard-coded rosters.

## Sources

- Anthropic Messages API, streaming, errors, count_tokens, stop reasons, Claude
  Code model config — platform.claude.com/docs & code.claude.com/docs (2026-10-13).
- OpenAI Chat Completions — openai-openapi spec v2.3.0 & platform.openai.com/docs
  (2026-10-13).
- Google Gemini (Generative Language API v1beta) — official discovery doc
  (2026-10-13).
- Provider landscape (NVIDIA NIM, Groq, OpenRouter, DeepSeek, Cerebras, Gemini,
  Mistral, Together, GitHub Models [retired], Ollama/LM Studio/llama.cpp/vLLM) —
  provider dashboards & dated third-party guides (2026-10-13); limits verify-in-dashboard.
- OWASP Top 10 for LLM Applications 2026; OWASP API Security Top 10 2023; OWASP
  SSRF / Secrets Management / Logging / CSRF Cheat Sheets; CVE-2025-66414 (2026-10-13).
- Prior art: github.com/Alishahryar1/free-claude-code; github.com/musistudio
  claude-code-router; claude-code-mux; WqyJh/a2o; ziozzang/claude2openai-proxy;
  CommandCodeBridge; BerriAI/litellm.
