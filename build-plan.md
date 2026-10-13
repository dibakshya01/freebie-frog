# Build Plan — freebie-frog

Canonical spec. Supersedes the earlier high-level `PLAN.md`. Nothing gets built
that isn't written here; when reality forces a change, this file is edited first.

> Spec review: ExpertSpecReviewer returned REVISE with 5 blocking issues; all five
> are incorporated below (streaming/fallback boundary, tool-call index→block mapping,
> fixture provenance/honest claims, encrypted-file key management, IPv6 SSRF), plus
> adopted non-blocking improvements. Build authorized by the user.

## 0. Thesis & principles

**Thesis:** freebie-frog is a single-binary, Apache-2.0, security-first proxy that
makes any Anthropic-API coding agent run on the model provider you choose —
free-tier, paid, or fully local — with correct streaming and automatic fallback,
using your own keys.

**Principles (decide trade-offs):**
1. **Protocol correctness first** — the streaming SSE translation is the product;
   it is snapshot-tested against spec fixtures and an in-process mock (and the real
   Claude Code loop where credentials allow).
2. **Security by default** — keychain secrets, no secret logging, loopback bind,
   SSRF guard (wired from M1), admin-UI auth, all on by default.
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
- **Not a multi-tenant security boundary** — designed as a local, single-user tool.

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
- **FR-8** OpenAI→Anthropic **streaming** — the core mechanic, specified exactly:
  - (a) **Index mapping.** OpenAI `delta.tool_calls[].index` lives in its own 0-based
    space; the Anthropic `content_block` index is **offset by any preceding text
    block** (when text is present it is block 0 and tool calls start at block 1).
  - (b) **Block lifecycle / ordering.** Open a text block on the first `delta.content`;
    emit its `content_block_stop` **before** the first tool-use `content_block_start`;
    map the OpenAI function `name` to the tool-use `content_block_start` and each
    `function.arguments` fragment to an `input_json_delta`. Accumulate defensively by
    index: set `id`/`name` on first non-empty occurrence; never require they appear
    only on chunk 0.
  - (c) **Reasoning.** Map `reasoning_content`/reasoning deltas (DeepSeek-R1 et al.) to
    Anthropic `thinking_delta`.
  - (d) **Stop/usage.** Map `finish_reason`→`stop_reason`; source
    `message_start.usage.input_tokens` from provider stream usage when present, else a
    local token estimate (**never emit 0**); emit cumulative `output_tokens` in
    `message_delta`.
- **FR-9** Round-trip multi-turn tool use (tool_use → tool_result → next turn) with
  correct tool-call ID matching.

Providers:
- **FR-10** Generic **OpenAI-compatible adapter**: configurable base URL, bearer
  auth, model list, and per-provider quirk flags (`max_tokens` vs
  `max_completion_tokens`, usage-in-stream on/off, images on/off,
  drop-unsupported-params).
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
  target; exponential backoff; per-provider circuit breaker; clean Anthropic-format
  error if the chain is exhausted. **Streaming boundary (critical):** fallback is
  legal **only before the first SSE event is emitted** to the client. Once
  `message_start` has been sent, a mid-stream upstream failure cannot be retried
  transparently — the proxy emits an Anthropic `error` event, then a terminal
  `message_stop`, and ends. (Golden snapshot: "upstream fails mid-stream".)

Config & security:
- **FR-15** Config: defaults → JSON file (providers, key refs, model map, fallback,
  per-agent profiles, server) → env overrides; validated with clear errors.
- **FR-16** Secret backends: env (default, zero-dep), OS keychain (optional,
  `go-keyring`), **AES-256-GCM encrypted file whose key is derived from a user
  passphrase via `crypto/pbkdf2` (Go 1.24 stdlib — no new dependency)**, or `0600`
  plaintext + warning; resolution order configurable; config stores a key *reference*,
  not the value. **Redaction:** strip a header-name denylist (`Authorization`,
  `x-api-key`, `api-key`, `x-goog-api-key`, `Proxy-Authorization`) **and** scrub every
  configured secret value plus `sk-`/bearer patterns from any logged body; neutralize
  CR/LF (log injection).
- **FR-17** **SSRF guard** on all outbound provider calls: upstream host allowlist;
  **dial-time** validation of every resolved IP (defeats DNS rebinding) via a custom
  `net.Dialer.Control`; redirects disabled. Blocked by default — IPv4 `127.0.0.0/8`,
  `0.0.0.0/8`, RFC1918 (`10/8`,`172.16/12`,`192.168/16`), link-local `169.254.0.0/16`
  (incl. metadata `169.254.169.254`); **IPv6** `::1/128`, `fe80::/10`, `fc00::/7`,
  multicast, **IPv4-mapped** `::ffff:*`, and IPv6 metadata (`fd00:ec2::254`). The
  local-model opt-in is **not** a blanket disable: it allowlists a **specific
  configured host:port**, and dial-time validation still refuses any resolved IP other
  than the one that host legitimately resolves to.
- **FR-18** Server binds **loopback by default**; a non-loopback bind is explicit, is
  warned, and **requires an ingress auth token** (refused otherwise) so the proxy is
  never exposed to a network unauthenticated.

UX / ops:
- **FR-19** `freebie run <agent>` launches a supported agent CLI with env pointed at
  the proxy (`ANTHROPIC_BASE_URL`, auth token); `claude` plus a generic exec.
- **FR-20** **Admin UI** (local web): view/edit providers, keys (masked, never
  shown), model map, fallback; view recent requests/usage (redacted). Protected by
  Host-header allowlist (port-stripped) + Origin/`Sec-Fetch-Site` + custom-header
  CSRF defense + optional token auth.
- **FR-21** Observability: structured logs (slog), per-request trace id, usage/
  latency, debug body capture (opt-in, redacted).
- **FR-22** CLI: `serve`, `run`, `config`, `doctor`, `providers`, `version`.
- **FR-23** Streams **cancel the upstream request on client disconnect** (request
  context propagation) — no orphaned provider calls or token burn.

## 4. Architecture

```
 agent CLI ──Anthropic Messages API (HTTP+SSE)──▶ freebie-frog
   │   ingress (/v1/messages, count_tokens, models)
   │   router (model/tier → ordered targets, pre-first-event fallback)
   │   translate (anthropic↔openai | anthropic↔gemini)
   │   provider adapter ── SSRF-guarded HTTP client ──▶ provider API
   │   config+secrets · obs(log/metrics) · admin UI
```

**Trust boundaries / untrusted input:**
- Incoming agent requests — local but validated.
- **Upstream base URLs from config** — treated as attacker-influenced → SSRF guard.
- **Provider responses** — untrusted data; relayed verbatim, never executed; escaped
  before any admin-UI rendering.
- **Secrets** — never logged, never rendered, stored via chosen backend.

**Key interfaces (Go):**
```go
type Adapter interface {
    Name() string
    Capabilities() Capabilities
    Complete(ctx context.Context, req *anthropic.Request, t Target) (*anthropic.Response, error)
    // Stream emits translated Anthropic SSE events via sink; it must emit nothing
    // until it has a first event or has decided to error (so the router can still
    // fall back before the first event).
    Stream(ctx context.Context, req *anthropic.Request, t Target, sink func(anthropic.StreamEvent) error) error
}
type Router interface { Route(model string) []Target }
type SecretStore interface { Get(ref string) (string, error) }
```

**Tech choices + rationale:**
- **Go 1.24, stdlib-first.** `net/http` (1.22+ routing), `log/slog`, `encoding/json`,
  `crypto/aes`+`cipher` (GCM) + `crypto/pbkdf2` (Go 1.24 stdlib) for the encrypted
  secret file, `net` custom `DialContext`/`Control` for SSRF dial-time validation,
  `go:embed` for admin-UI assets. Single static binary — our distribution wedge.
- **Dependencies (justified, minimal):** `github.com/zalando/go-keyring` — optional OS
  keychain backend (OWASP-recommended); the binary runs without a keychain present.
  *Everything else stdlib.* No web framework, no LLM SDKs, no YAML. PBKDF2 is stdlib
  in Go 1.24, so the encrypted-file backend adds **no** dependency.
- **Rejected:** Python/Node (runtime/packaging friction — the gap we exploit);
  LiteLLM/SDKs (heavy, not Anthropic-ingress-first); a web framework (unneeded).
- **Config format:** JSON (zero-dep, swappable). Secrets referenced, not embedded.

**File layout:**
```
cmd/freebie/            CLI (serve/run/config/doctor/providers/version)
internal/
  anthropic/            Anthropic wire types + SSE event writer
  openai/               OpenAI Chat Completions wire types
  gemini/               Gemini wire types
  translate/            anthropic<->openai and anthropic<->gemini; SSE reconstruction
  provider/             Adapter iface + openai_compatible, gemini, anthropic_passthrough
  router/               model/tier map + fallback + circuit breaker
  config/               config load/validate (extends M0)
  secret/               SecretStore: env, keychain, encrypted-file(PBKDF2+GCM), plaintext
  security/             SSRF dialer, redaction, host-allowlist/CSRF middleware, http client
  server/               ingress handlers + middleware (extends M0)
  admin/                admin UI handlers + embedded assets (go:embed)
  obs/                  logging/metrics (extends M0)
  launch/               agent launchers
  mockprovider/         in-process OpenAI/Gemini mock for tests & e2e
test/                   golden SSE snapshots, e2e against mock
docs/                   SOURCES.md, THREAT_MODEL.md, CAPABILITIES.md, site/
```

## 5. Milestones (build order)

M0 (**done**) — scaffold: serve/config/doctor/version, `/healthz`, slog, CI.

- **M1 — Non-streaming core.** `internal/anthropic`,`openai` types; `translate`
  (anthropic→openai req, openai→anthropic resp); generic OpenAI adapter; config
  (providers, key refs, model map) — *FR-15*; **the SSRF-guarded HTTP client is
  established here** (principle 2) even though M1 tests hit only the mock; `POST
  /v1/messages` non-stream. *FRs: 1,6,7,10,13,15,17(client).* **Accept:** request
  through the in-process mock returns a correct Anthropic `Message`.
- **M2 — Streaming.** SSE reconstruction per FR-8 (index→block mapping, lifecycle,
  reasoning, usage); client-disconnect cancellation (FR-23); mid-stream error path.
  *FRs: 2,8,23.* **Accept:** golden SSE snapshot tests (text; text+tool; multi-tool;
  mid-stream error) pass; simulated tool loop against the mock completes.
- **M3 — Tools/images/multiturn + tokens.** tool_use/tool_result round-trip, image
  data URIs, `count_tokens` + usage estimation, `GET /v1/models`. *FRs: 3,4,9.*
  **Accept:** multi-turn round-trip tests; count_tokens returns a number.
- **M4 — Router + fallback.** model/tier map, ordered targets, backoff, circuit
  breaker, **pre-first-event-only** streaming fallback, clean error. *FR: 14.*
  **Accept:** failing-mock fallback tests incl. "fails before first event" (retries)
  vs "fails mid-stream" (emits error event, no retry).
- **M5 — Security hardening.** SSRF range blocks + dial-time validation + constrained
  local opt-in (complete FR-17); secret store (env/keychain/encrypted/plaintext,
  FR-16); log redaction (FR-16); loopback default + ingress token (FR-18);
  observability (FR-21). **Accept:** SSRF attack-tests (IPv4+IPv6 metadata/loopback/
  RFC1918/rebinding refused; configured provider allowed; local opt-in works);
  redaction tests (no secret ever in captured logs); secret round-trip.
- **M6 — Gemini + passthrough.** Gemini native adapter; Anthropic passthrough.
  *FRs: 11,12.* **Accept:** Gemini mock round-trip (function call, stream).
- **M7 — Launchers + providers CLI.** `freebie run <agent>`; `freebie providers`.
  *FRs: 19,22.* **Accept:** launcher sets env and execs a fake agent that echoes
  `ANTHROPIC_BASE_URL`.
- **M8 — Admin UI.** Embedded local UI; Host-allowlist + CSRF + auth; edit config,
  view redacted usage. *FR: 20.* **Accept:** UI served on loopback; rebinding/CSRF
  tests; config edit persists.

CAP = milestones + 3 (hard ceiling 12). Independent milestones (M6 Gemini, M8 UI,
and the Phase-4 site) may be built in parallel via subagents against stable
interfaces; the correctness-critical core (M1–M2) is built directly.

## 6. Test strategy

- **Unit (table tests)** for each translation direction: request mapping, response
  mapping, schema/tool conversion, stop_reason/finish_reason maps, edge cases
  (empty content, tool-only turns, images, unicode).
- **Golden/snapshot** for streaming: assert the exact Anthropic SSE byte sequence for
  representative OpenAI/Gemini chunk streams (text, text+tool, parallel tools,
  mid-stream error, usage present/absent). **Fixture provenance:** egress chunk
  fixtures are taken from the official spec examples (OpenAI/Anthropic/Gemini docs) and
  real captures where credentials allow; fixtures are labeled with their source, and
  correctness claims are scoped to what the fixtures prove (no "byte-correct against
  real traffic" claim unless a real capture backs it).
- **In-process mock provider** (`internal/mockprovider`): a fake OpenAI/Gemini server
  for full e2e with no network/keys — how "verify it runs" is met.
- **Security attack-tests:** SSRF (IPv4+IPv6 metadata/loopback/RFC1918/rebinding
  refused; configured provider allowed; local opt-in scoped); log redaction (no secret
  value or auth header ever in captured logs); admin-UI Host-allowlist + CSRF.
- **CLI tests:** serve/doctor/config/run behavior.
- **CI:** gofmt, `go vet`, `go test -race ./...`, `go build`, `govulncheck`.
- **"Self-checks clean"** = `make check` + `govulncheck` green; mock e2e completes a
  streamed tool loop; every README/site claim reproduced by a test or the mock.

## 7. Docs, site & deck plan (Phase 4)

- README: value prop, quick start (first screen), install, config, "use with Claude
  Code", honest-limits link, rendered deck slides + hero, badges.
- `docs/` repo: `SOURCES.md` (clean-room), `THREAT_MODEL.md` (OWASP mapping),
  `CAPABILITIES.md` ("what it can & cannot do"), provider notes.
- Site (GitHub Pages, `docs/`): Home, ELI5, Docs, Architecture (SVG diagrams),
  Contact — one design system, gated motion, verified responsive/dark/contrast.
- Social preview card.

## 8. Honest limits (written before building)

- **Token counts are approximate** — providers often omit stream usage; `count_tokens`
  is an estimate, not Anthropic-exact.
- **Not every Anthropic feature maps** — prompt caching, server-side tools, fine-grained
  `thinking` budgets, and some params have no OpenAI equivalent; dropped/approximated
  (documented per adapter).
- **Provider parity varies** — some providers ignore params, lack tool/vision support,
  or stream tool calls non-standardly; handled best-effort and flagged, not guaranteed.
- **Mid-stream provider failure is not transparently recoverable** — fallback only
  happens before the first event; after that the client gets an error event.
- **Free tiers are the providers'** — availability/limits change without notice; the
  realistic aggregate is tens-to-low-hundreds of millions of tokens/month,
  throttle-bound and per-org-pooled — not "unlimited" or "1B+".
- **Prompt injection in forwarded content is out of scope** (shared responsibility).
- **Not a security boundary for untrusted multi-tenant use** — local single-user tool;
  network exposure is opt-in, token-gated, and at your own risk.

## 9. Definition of Done (Phase 3 exit)

- [ ] FR-1…FR-23 implemented (full suite).
- [ ] `make check` + `govulncheck` green; `go test -race ./...` passes.
- [ ] Streaming SSE snapshot tests pass (incl. mid-stream error); mock e2e completes a
      streamed tool loop.
- [ ] Security attack-tests (IPv4+IPv6 SSRF, redaction, admin-UI CSRF/Host) pass.
- [ ] `freebie serve` runs; `freebie run claude` wiring verified against a fake agent;
      admin UI serves on loopback.
- [ ] Docs present (`CAPABILITIES.md`, `THREAT_MODEL.md`, `SOURCES.md`); every README
      claim reproducible by a test or the mock.
- [ ] `BUILD_LOG.md` current; `HANDOFF.md` lists all user-only actions.
