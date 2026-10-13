# Threat Model

freebie-frog is a **local, single-user** proxy that holds your provider API keys and
relays your prompts to third-party model providers. It is trusted by the agent CLI
on the same machine. This document maps its security-relevant surfaces to current
OWASP guidance and states the controls and their limits honestly.

> Scope: this is a local developer tool. It is **not** designed as a multi-tenant
> security boundary. Exposing it on a network is opt-in, token-gated, and at your
> own risk.

## Standards referenced

- **OWASP Top 10 for LLM Applications — 2026 edition** (published 2026-08-04).
- **OWASP API Security Top 10 — 2023.**
- OWASP SSRF / Secrets Management / Logging / CSRF Cheat Sheets.

## Threats → controls

| # | Threat | OWASP | Control in freebie-frog | Honest limit |
|---|--------|-------|-------------------------|--------------|
| 1 | Provider keys readable on disk / in backups | LLM02; API8 | Secret backends: OS keychain → AES-256-GCM encrypted file (PBKDF2 key) → `0600` plaintext **+ warning**; config stores a *reference*, not the value | Plaintext mode is a last resort; a local attacker with your user account can still read what your user can read |
| 2 | Keys/tokens leak into logs | LLM02 | Header denylist (`Authorization`, `x-api-key`, `api-key`, `x-goog-api-key`, …) **and** known-secret-value scrubbing + `sk-`/bearer regex on bodies; CR/LF neutralized | Redaction is best-effort pattern matching; avoid pasting secrets into prompts |
| 3 | Prompt/response logging captures secrets/PII | LLM02 | Body logging is **opt-in** (debug) and redacted; no bodies logged by default | If you enable debug body capture, treat the logs as sensitive |
| 4 | **SSRF via user-configured upstream** (metadata/loopback/internal) | **API7**; LLM04 | SSRF guard on every outbound call: **dial-time** validation of each resolved IP against a blocklist (IPv4 + IPv6 + IPv4-mapped: loopback, RFC1918, link-local incl. `169.254.169.254`, ULA incl. `fd00:ec2::254`, CGNAT, multicast); redirects disabled; local-model access is a **scoped per-provider opt-in** to a specific host, still IP-validated at dial time | A provider you explicitly `allow_local` can reach loopback by design; keep that list minimal |
| 5 | Cost/token/DoS on the spend path | LLM06; API4 | Request size cap; per-request context timeouts; streaming cancels the upstream on client disconnect | Per-key spend/rate caps are roadmap; your provider's own limits are the primary guard |
| 6 | Dependency / supply-chain compromise | LLM04; API10 | Near-zero dependencies (stdlib + one optional keychain lib); `go.sum` pinned; `govulncheck` in CI; upstream TLS verification left on | — |
| 7 | Untrusted upstream response rendered in admin UI (XSS) | LLM10 | Admin UI escapes/encodes displayed content; never evaluates upstream content | — |
| 8 | Admin UI exposed to the LAN | API8; API2 | Binds `127.0.0.1` by default; non-loopback bind is explicit, warned, and **requires an ingress token** | — |
| 9 | DNS rebinding → browser reaches the local daemon | API8; API2 | Host-header allowlist (port-stripped) + `Sec-Fetch-Site`/Origin checks on the admin UI (cf. CVE-2025-66414) | — |
| 10 | CSRF against admin state-changing endpoints | API2/5; API8 | State changes require a non-safe method + a custom header / CSRF check; no state change on GET | — |
| 11 | Prompt injection in forwarded content | LLM01 | **Out of scope / shared responsibility** — a transparent proxy cannot neutralize injection in payloads it relays; we avoid *adding* injectable surface (we don't reflect upstream errors into prompts) | The agent and model own this risk |

## Reporting

See [../SECURITY.md](../SECURITY.md) for private vulnerability reporting.
