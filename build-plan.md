# Build Plan — freebie-frog

Canonical spec. Supersedes the earlier high-level `PLAN.md`. Nothing gets built
that isn't written here; when reality forces a change, this file is edited first.

## 0. Thesis & principles

**Thesis:** freebie-frog is a single-binary, Apache-2.0, security-first proxy that
makes any Anthropic-API coding agent run on the model provider you choose —
free-tier, paid, or fully local — with correct streaming and automatic fallback,
using your own keys.

**Principles (decide trade-offs):**
1. **Protocol correctness first** — the streaming SSE translation is the product.
2. **Security by default** — keychain secrets, no secret logging, loopback bind,
   SSRF guard, admin-UI auth, all on by default.
3. **One binary, few/no deps** — Go stdlib-first; every dependency justified.
4. **Honest & ToS-respecting** — BYO-key/single-account; no quota-evasion; claims
   match measured reality; clean-room; trademark-clean.
5. **Config-driven breadth** — one generic OpenAI adapter + per-provider config;
   Gemini-native the one extra adapter; runtime model discovery over hard-coded rosters.

## 1. Goals & non-goals

**Goals**
- Serve an Anthropic-Messages-API-compatible endpoint that coding agents (Claude
  Code first) can target via `ANTHROPIC_BASE_URL`.
- Correctly translate requests/responses, **including streaming**, to/from
  OpenAI-compatible providers and (natively) Gemini.
- Route by model/tier with automatic multi-provider fallback.
- Handle provider API keys securely; guard against SSRF; ship a safe local admin UI.
- Launch supported agents pre-pointed at the proxy.

**Non-goals (deliberate)**
- **No multi-account rotation / cross-account quota pooling / key resale** — ToS
  violations (e.g. Groq AUP). BYO-key, single-account only.
- **No prompt-injection defense of forwarded content** — a transparent proxy can't
  neutralize injection in the payloads it relays; that's the agent's/model's
  responsibility (documented).
- **No model hosting / inference** — we proxy to providers, we don't run models.
- **No exact token-count parity** — counts are best-effort/estimated.
- **No guarantee of free-tier availability** — tiers and limits are the providers'.

## 2. Users & jobs-to-be-done

- **Primary: the individual developer** already using an Anthropic-API agent, who
  wants to run it on a cheaper/free/local model. Job: "point my agent at a free or
  local model without it breaking, and fall back when one is rate-limited."
- **Privacy-focused developer.** Job: "keep everything on a local model (Ollama/
  vLLM) with nothing leaving my machine."
- **Tinkerer/self-hoster.** Job: "a single binary I can run anywhere, configure in
  one file or a local UI, with no runtime to install."

## 3. Functional spec (testable)

Ingress (Anthropic-compatible):
- **FR-1** `POST /v1/messages` (non-streaming) accepts an Anthropic request and
  returns a valid Anthropic `Message`.
- **FR-2** `POST /v1/messages` with `stream:true` emits the exact Anthropic SSE
  event sequence (`message_start`→blocks→`message_delta`→`message_stop`, `ping`).
- **FR-3** `POST /v1/messages/count_tokens` returns `{"input_tokens":N}`.
- **FR-4** `GET /v1/models` lists the model IDs the proxy exposes (from config/map).
- **FR-5** `GET /healthz` returns service/version (done in M0).

Translation:
- **FR-6** Anthropic→OpenAI request: system (string|blocks), messages, content
  blocks (text/image/tool_use/tool_result), tools + tool_choice, params
  (`max_tokens`, temperature, top_p, stop_sequences), images as data URIs.
- **FR-7** OpenAI→Anthropic response (non-stream): text + tool_use blocks,
  `stop_reason` mapping, usage.
- **FR-8** OpenAI→Anthropic **streaming**: reconstruct blocks from chunk deltas;
  **tool-call arguments accumulated defensively by index** and emitted as
  `input_json_delta`; map `finish_reason`; synthesize cumulative usage.
- **FR-9** Round-trip multi-turn tool use (tool_use → tool_result → next turn) with
  correct tool-call ID matching.

Providers:
- **FR-10** Generic **OpenAI-compatible adapter**: configurable base URL, bearer
  auth, model list, and per-provider quirk flags (send `max_tokens`, usage-in-stream
  on/off, images on/off, drop-unsupported-params).
- **FR-11** **Gemini native adapter**: `generateContent`/`streamGenerateContent?alt=sse`,
  role mapping (user/model, systemInstruction), function calling via
  `parametersJsonSchema`, whole-chunk tool calls, `thoughtSignature` round-trip,
  no `[DONE]` sentinel.
- **FR-12** **Anthropic passthrough adapter**: forward to a real Anthropic-compatible
  upstream (for mixing paid Anthropic with free providers).

Routing:
- **FR-13** Map incoming model **or tier** (opus/sonnet/haiku/fable, matched by
  family substring) → an ordered list of `provider/model` targets.
- **FR-14** **Fallback**: on 429/5xx/timeout/context-length/auth error, try the next
  target; exponential backoff; per-provider circuit breaker; return a clean
  Anthropic-format error if the whole chain fails.

Config & security:
- **FR-15** Config: defaults → JSON file (providers, key refs, model map, fallback,
  per-agent profiles, server) → env overrides; validated with clear errors.
- **FR-16** Secret backends: env (default), OS keychain (optional), AES-256-GCM
  encrypted file, or `0600` plaintext + warning; resolution order configurable;
  **keys/`Authorization`/`x-api-key`/`sk-` never logged**.
- **FR-17** **SSRF guard**: upstream allowlist; block loopback/link-local/RFC1918/
  metadata ranges by default (per-provider opt-in for local models); **dial-time IP
  validation** (defeats DNS rebinding); redirects disabled.
- **FR-18** Server binds **loopback by default**; non-loopback bind is explicit +
  warned.

UX:
- **FR-19** `freebie run <agent>` launches a supported agent CLI with env pointed at
  the proxy (`ANTHROPIC_BASE_URL`, auth token); `claude` plus a generic exec.
- **FR-20** **Admin UI** (local web): view/edit providers, keys (masked, never
  shown), model map, fallback; view recent requests/usage (redacted). Protected by
  Host-header allowlist (port-stripped) + Origin/`Sec-Fetch-Site` + custom-header
  CSRF defense + optional token auth.
- **FR-21** Observability: structured logs (slog), per-request trace id, usage/
  latency, debug body capture (opt-in, redacted).
- **FR-22** CLI: `serve`, `run`, `config`, `doctor`, `providers`, `version`.

## 4. Architecture

```
 agent CLI ──Anthropic Messages API (HTTP+SSE)──▶ freebie-frog
   │                                                 │
   │   ingress (/v1/messages, count_tokens, models)  │
   │        │                                         │
   │   router (model/tier → ordered targets, fallback)│
   │        │                                         │
   │   translate (anthropic↔openai | anthropic↔gemini)│
   │        │                                         │
   │   provider adapter ── SSRF-guarded HTTP ─────────┼──▶ provider API
   │   config+secrets · obs(log/metrics) · admin UI   │
```

**Trust boundaries / untrusted input:**
- Incoming agent requests — trusted-ish (local agent) but validated.
- **Upstream base URLs from config** — treated as attacker-influenced → SSRF guard.
- **Provider responses** — untrusted data; relayed verbatim, never executed; escaped
  before any admin-UI rendering.
- **Secrets** — never logged, never rendered, stored via chosen backend.

**Key interfaces (Go):**
```go
// provider adapter
type Adapter interface {
    Name() string
    Capabilities() Capabilities
    // non-streaming
    Complete(ctx, *anthropic.Request, Target) (*anthropic.Message, error)
    // streaming: emits Anthropic SSE events on the channel
    Stream(ctx, *anthropic.Request, Target, func(anthropic.StreamEvent) error) error
}
// router
type Router interface { Route(model string) []Target }  // ordered targets
// secret store
type SecretStore interface { Get(ref string) (string, error) }
```

**Tech choices + rationale:**
- **Go 1.24, stdlib-first.** `net/http` (incl. 1.22+ method routing), `log/slog`
  (structured logs), `encoding/json`, `crypto/aes`+`cipher` (GCM) for the encrypted
  secret file, `net` custom `DialContext` for SSRF dial-time validation, `go:embed`
  for the admin UI assets. Single static binary, trivial distribution — our wedge.
- **Dependencies (justified, minimal):**
  - `github.com/zalando/go-keyring` — OS keychain backend (OWASP-recommended secret
    storage). Optional backend; the binary runs without a keychain present.
  - *(Everything else stdlib.)* No web framework (admin UI is embedded static +
    stdlib handlers); no LLM SDKs (we speak the wire formats directly); no YAML (JSON
    config).
- **Rejected:** Python/Node (runtime+packaging friction — the gap we exploit);
  LiteLLM/SDKs (heavy, not Anthropic-ingress-first); a web framework (unneeded).
- **Config format:** JSON (zero-dep, swappable). Secrets referenced, not embedded.

**File layout:**
```
cmd/freebie/                CLI entrypoint (serve/run/config/doctor/providers/version)
internal/
  anthropic/                Anthropic wire types (request, message, content blocks, SSE events)
  openai/                   OpenAI Chat Completions wire types (request, response, chunk)
  gemini/                   Gemini wire types
  translate/                anthropic<->openai and anthropic<->gemini; SSE reconstruction
  provider/                 Adapter interface + openai_compatible, gemini, anthropic_passthrough
  router/                   model/tier map + fallback engine + circuit breaker
  config/                   config load/validate (extends M0)
  secret/                   SecretStore: env, keychain, encrypted-file, plaintext
  security/                 SSRF dialer, redaction, host-allowlist/CSRF middleware
  server/                   ingress handlers + middleware (extends M0)
  admin/                    admin UI handlers + embedded assets (go:embed)
  obs/                      logging/metrics (extends M0)
  launch/                   agent launchers
internal/mockprovider/      in-process OpenAI/Gemini mock for tests & e2e
test/ (e2e, snapshots)      golden SSE snapshots, e2e against mock
docs/                       SOURCES.md, THREAT_MODEL.md, CAPABILITIES.md, site/
```

## 5. Milestones (build order)

M0 (**done**) — scaffold: serve/config/doctor/version, `/healthz`, slog, CI.

- **M1 — Non-streaming core.** `internal/anthropic`,`openai` types; `translate`
  (anthropic→openai req, openai→anthropic resp); generic OpenAI adapter; minimal
  model map; `POST /v1/messages` non-stream. *FRs: 1,6,7,10,13.* **Accept:**
  request through the in-process mock returns a correct Anthropic `Message`.
- **M2 — Streaming.** SSE reconstruction with defensive tool-call accumulation →
  `input_json_delta`; `finish_reason`/usage mapping. *FRs: 2,8.* **Accept:**
  golden SSE snapshot tests (text; text+tool; multi-tool) pass; a simulated tool
  loop against the mock completes.
- **M3 — Tools/images/multiturn + tokens.** tool_use/tool_result round-trip, image
  data URIs, `count_tokens` + usage estimation. *FRs: 3,4,9,20-partial.* **Accept:**
  multi-turn round-trip tests; count_tokens returns a number.
- **M4 — Router + fallback.** model/tier map, ordered targets, backoff, circuit
  breaker, clean error. *FRs: 13,14.* **Accept:** failing-mock fallback tests.
- **M5 — Security.** SSRF dialer (allowlist + range block + dial-time validation +
  no redirects), secret store (env/keychain/encrypted/plaintext), log redaction,
  loopback default. *FRs: 16,17,18,21.* **Accept:** SSRF attack-tests (metadata/
  loopback/RFC1918 blocked; rebinding blocked); redaction tests; secret round-trip.
- **M6 — Gemini + passthrough.** Gemini native adapter; Anthropic passthrough.
  *FRs: 11,12.* **Accept:** Gemini mock round-trip (incl. function call, stream).
- **M7 — Launchers.** `freebie run <agent>`. *FR: 19.* **Accept:** launcher sets env
  and execs a fake agent that echoes `ANTHROPIC_BASE_URL`.
- **M8 — Admin UI.** Embedded local UI; Host-allowlist + CSRF + auth; edit config,
  view redacted usage. *FR: 20.* **Accept:** UI served on loopback; rebinding/CSRF
  tests; config edit persists.

CAP = milestones + 3 (hard ceiling 12). Independent milestones (M6 Gemini, M8 UI)
may be built in parallel via subagents; core translation (M1–M2) is built directly.

## 6. Test strategy

- **Unit (table tests)** for each translation direction: request mapping, response
  mapping, schema/tool conversion, stop_reason/finish_reason maps, edge cases
  (empty content, tool-only turns, images, unicode).
- **Golden/snapshot** for streaming: assert the exact Anthropic SSE byte sequence
  for representative OpenAI/Gemini chunk streams (text, text+tool, parallel tools,
  error mid-stream, usage present/absent).
- **In-process mock provider** (`internal/mockprovider`): a fake OpenAI/Gemini
  server for full e2e with no network/keys. This is how "verify it runs" is met.
- **Security attack-tests:** SSRF (metadata/loopback/RFC1918/rebinding all refused;
  configured provider allowed; local opt-in works); log redaction (no secret ever
  appears in captured logs); admin-UI Host-allowlist + CSRF (cross-site POST
  refused).
- **CLI tests:** serve/doctor/config/run behavior.
- **CI:** gofmt, `go vet`, `go test -race ./...`, `go build`, `govulncheck`.
- **What "self-checks clean" means:** `make check` + `govulncheck` green; the mock
  e2e completes a streamed tool loop; every README/site claim reproduced by a test
  or the mock.

## 7. Docs, site & deck plan (Phase 4)

- README: value prop, quick start (first screen), install, config, "use with Claude
  Code", honest limits link, rendered deck slides + hero, badges.
- `docs/` repo: `SOURCES.md` (clean-room), `THREAT_MODEL.md` (OWASP mapping),
  `CAPABILITIES.md` ("what it can & cannot do"), provider notes.
- Site (GitHub Pages, `docs/`): Home, ELI5, Docs, Architecture (SVG diagrams),
  Contact — one design system, gated motion, verified responsive/dark/contrast.
- Social preview card.

## 8. Honest limits (written before building)

- **Token counts are approximate** — providers often omit stream usage; `count_tokens`
  is an estimate, not Anthropic-exact.
- **Not every Anthropic feature maps** — prompt caching, server-side tools
  (web_search/code_execution), fine-grained `thinking` budgets, and some params have
  no OpenAI equivalent; they are dropped or approximated (documented per adapter).
- **Provider parity varies** — some providers ignore params, lack tool/vision
  support, or stream tool calls non-standardly; quirks are handled best-effort and
  flagged, not guaranteed.
- **Free tiers are the providers'** — availability/limits change without notice; the
  realistic aggregate is tens-to-low-hundreds of millions of tokens/month,
  throttle-bound and per-org-pooled — not "unlimited" or "1B+".
- **Prompt injection in forwarded content is out of scope** (shared responsibility).
- **Not a security boundary for untrusted multi-tenant use** — designed as a local,
  single-user tool; exposing it to a network is opt-in and at your own risk.

## 9. Definition of Done (Phase 3 exit)

- [ ] FR-1…FR-22 implemented (full suite).
- [ ] `make check` + `govulncheck` green; `go test -race ./...` passes.
- [ ] Streaming SSE snapshot tests pass; mock e2e completes a streamed tool loop.
- [ ] Security attack-tests (SSRF, redaction, admin-UI CSRF/Host) pass.
- [ ] `freebie serve` runs; `freebie run claude` wiring verified against a fake agent;
      admin UI serves on loopback.
- [ ] Docs present (`CAPABILITIES.md`, `THREAT_MODEL.md`, `SOURCES.md`); every README
      claim reproducible by a test or the mock.
- [ ] `BUILD_LOG.md` current; `HANDOFF.md` lists all user-only actions.
