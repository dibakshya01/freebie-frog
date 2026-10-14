# Build Log

A running, one-line-per-step record of the freebie-frog build.

## Phase 0 — Intake
- Repo `dibakshya01/freebie-frog`; dir `/home/user/freebie-frog`; stack Go; license Apache-2.0.
- Scope: full suite in one pass (core proxy + Gemini adapter + launchers + admin UI).
- Marketplace not eligible (CLI) → skipped; distribution on handoff.
- Verification: in-process mock provider (real-provider run is a handoff item).

## Phase 1 — Research
- Fanned out 5 parallel research threads: Anthropic Messages API + SSE; OpenAI
  Chat Completions + streaming; Google Gemini native API; free-tier provider
  landscape (2026); OWASP LLM/API security + proxy threat model.
- Wrote Apache-2.0 LICENSE, NOTICE, CONTRIBUTING, CODE_OF_CONDUCT, SECURITY,
  issue/PR templates, HANDOFF.md while research ran.
- All 5 research reports synthesized into `finding.md` (problem, prior-art gap,
  technical landscape, OWASP 2026 mapping, risks, thesis + 5 principles).

## Phase 2 — Spec
- Wrote `build-plan.md` (canonical; supersedes PLAN.md): 22 functional requirements,
  architecture + interfaces + trust boundaries, tech choices (Go stdlib-first; one
  justified dep: go-keyring), milestones M1–M8, test strategy, honest limits, DoD.
- Spawned a fresh memory-free ExpertSpecReviewer (given only finding.md + build-plan.md).
- Reviewer verdict: REVISE — 5 blocking issues, all legitimate:
  (1) streaming+fallback boundary (pre-first-event only);
  (2) tool-call index→content-block index mapping + block lifecycle + usage sourcing;
  (3) fixture provenance / downgrade "byte-correct" claims to what fixtures prove;
  (4) encrypted-file key management (PBKDF2 stdlib, no new dep);
  (5) IPv6 + IPv4-mapped SSRF ranges + constrained local opt-in.
  Applied all 5 to build-plan.md + adopted non-blocking improvements (SSRF client from
  M1, header-denylist+known-value redaction, client-disconnect cancellation FR-23,
  ingress token for non-loopback, reasoning_content->thinking_delta, FR/milestone map
  fixes). Softened finding.md framing (Apache=patent grant; claude-code-mux caveat).
- User approved the corrected spec and authorized the build (no re-review cycle).

## Phase 3 — Build
- M1 (non-streaming core) + M2 (streaming) DONE and verified:
  - internal/anthropic, internal/openai wire types + Anthropic SSE event writer.
  - internal/translate: Anthropic->OpenAI request, OpenAI->Anthropic response, token
    estimator, and the streaming SSE reconstruction (index->block mapping, lifecycle,
    reasoning_content->thinking_delta, usage, mid-stream error) per FR-8.
  - internal/provider: Adapter interface + generic OpenAI-compatible adapter (Complete
    + Stream) with typed provider errors + retryable classification.
  - internal/security: SSRF guard (IPv4+IPv6+mapped ranges, dial-time validation,
    constrained local opt-in) + guarded HTTP client (redirects disabled) — wired from M1.
  - internal/secret: env/literal resolver (keychain/encrypted land in M5).
  - internal/router: registry (builds adapters from config, SSRF client per provider)
    + model/tier routing + dispatch/fallback engine with the streaming pre-first-event
    boundary (FR-14).
  - internal/config: providers + model routing config + validation.
  - internal/server: /v1/messages (stream + non-stream), /v1/messages/count_tokens,
    /v1/models (FR-1..4 ingress).
  - internal/mockprovider: in-process OpenAI mock for empirical e2e.
  - Tests: translate request/response tables; streaming snapshots (text; text+tool w/
    index-1 mapping; tool-only index-0; reasoning->thinking; mid-stream error);
    SSRF IPv4/IPv6 ranges; e2e via mock (non-stream, stream, 503, 400, count_tokens).
  - Early partials: M3 (count_tokens, /v1/models, images, multiturn), M4 (fallback
    engine), M5 (SSRF client). `go test -race ./...` green; vet + gofmt clean; binary builds.
- M3 complete: tools/images/multi-turn round-trips, count_tokens, /v1/models (tested).
- M4 complete: router fallback (retryable vs non-retryable, no-route, pre-first-event
  stream fallback) with dispatch tests.
- M5 complete (delegated streaming core done by me): keychain + PBKDF2/AES-GCM
  encrypted-file + file/env/literal secret backends; log redaction (header denylist +
  known-value scrub + key patterns); ingress auth + loopback-default + non-loopback
  refuses without token. Attack-tests green.
- M6 complete (subagent, reviewed + wired by me): Gemini native adapter + Anthropic
  passthrough; 21 tests. Edge case (allow_local + empty base_url) hardened.
- M7 complete: `freebie run <agent>` launcher (verified against a fake agent) +
  `freebie providers`.
- M8 complete: read-only admin dashboard (go:embed), keys masked, Host-allowlist
  (DNS-rebinding defense) + CSRF guard; verified live (state leaks no keys, rebinding
  Host -> 403). Spec FR-20 + CAPABILITIES updated to read-only scope (honesty).
- Phase 3 DONE. Full `go test -race ./...` green across 11 packages; gofmt + vet clean.
- Website (Phase 4) built in parallel by a subagent (docs/); corrected stale
  "scaffold/roadmap" copy to reflect the shipped full suite.

## Phase 5 — Adversarial hardening
- Round 1 (fresh devil's-advocate subagent, read-only, isolated probes): found 1
  critical + 8 moderate + 4 minor, all legitimate. Fixed:
  - CRITICAL: SSE Flusher silently dropped by the logging wrapper -> added
    statusRecorder Flush()+Unwrap() (streaming now delivered per event).
  - Mid-stream errors now terminate the SSE (error + content_block_stop + message_stop).
  - 64 MiB stream read caps; non-streaming 120s timeout.
  - Defensive tool-call accumulation (name/id after args; index-less parallel tools).
  - Loopback default bind; allow_local scoped to the configured host; NAT64/reserved
    SSRF ranges; secret scrubbing wired into upstream error text + logs; CR/LF
    neutralized in logs. Docs/spec reconciled to actual behavior (honesty).
  - Added regression/attack tests for each. Full suite green with -race.
- Round 2 (fresh subagent, read-only, isolated probes): verified ALL 7 round-1 fixes
  hold under dynamic attack; no criticals/regressions; -race clean; no leaks/panics.
  Fixed 2 moderate + 2 minor: wired a live known-value redactor (RegisterSecrets from
  router.Build; broadened patterns) so odd-shaped keys scrub by value; namespaced
  synthetic tool-call keys (no collision with negative indices); placeholder name/id
  for nameless tools; case-insensitive admin Host check. Tests added; green with -race.
- Round 3 (final, fresh subagent): verdict SHIP-READY — all round-2 fixes hold under
  dynamic attack; no criticals; -race clean; ~1.2M fuzz execs with zero panics; no
  goroutine leaks; no auth bypass; licensing/claims correct. Found 2 low-likelihood
  🟡 nits, both fixed here:
  - F1: provider error messages scrubbed BEFORE truncation (a key straddling the
    500-char cut could leak a prefix) — fixed in all 3 provider message functions.
  - F2: nameless/idless tool_use placeholder extended to the other 3 tool paths
    (OpenAI non-stream, Gemini stream + non-stream) for schema validity.
  Tests added for both. Full suite green with -race.
- Phase 5 DONE: 3 adversarial rounds, severity trended critical->moderate->minor,
  all fixes guarded by tests, no criticals survive. (govulncheck is the one unverified
  item — the vuln DB is blocked by this sandbox's proxy; it runs in CI / a connected
  environment. Deps are minimal: one direct, go-keyring.)
