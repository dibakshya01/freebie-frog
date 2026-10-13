# Security Policy

freebie-frog is a local proxy that handles your **provider API keys** and relays
your prompts to third-party model providers. We take its security seriously.

## Reporting a vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report privately using **GitHub's private vulnerability reporting**:
Security → "Report a vulnerability" on this repository
(<https://github.com/dibakshya01/freebie-frog/security/advisories/new>).

Include: a description, affected version (`freebie version`), reproduction steps,
and impact. Please do not include real API keys or secrets in your report —
describe where the issue is instead.

We aim to acknowledge reports within a few days and to coordinate a fix and
disclosure timeline with you.

> Maintainers: consider adding a dedicated security contact email. See HANDOFF.md.

## Supported versions

Until a `1.0.0` release, only the latest `main` / most recent tagged release
receives security fixes.

## Scope & threat model (summary)

freebie-frog runs on your machine and is trusted by your agent CLI. Its main
security-relevant surfaces:

- **Secret handling.** Provider API keys are read from config/env/keychain and
  sent only to the upstream you configured. Keys and `Authorization` headers are
  **never** written to logs. Config files holding secrets should be mode `0600`.
- **Upstream URLs / SSRF.** Because you can configure upstream base URLs, the
  proxy restricts outbound requests to the configured providers and (by default)
  refuses upstreams pointing at link-local / loopback / private-network addresses
  unless you explicitly opt in (needed for local models like Ollama). This limits
  use of the proxy as a server-side request forgery pivot.
- **Bind address.** The server binds to loopback by default. Exposing it on a
  non-loopback interface is opt-in and warned about.
- **Untrusted content.** Model output and provider responses are treated as data
  and relayed verbatim; the proxy does not execute them. Prompt-injection risk in
  the *content* is inherent to LLM use and out of the proxy's control — see the
  "what it can & cannot do" doc.

A full threat model (mapped to OWASP LLM / API Top 10) lives in
[`docs/THREAT_MODEL.md`](docs/THREAT_MODEL.md).

## Responsible use

freebie-frog uses **your own** provider accounts and keys. Respect each
provider's Terms of Service and rate limits. The project does not implement, and
will not accept contributions that implement, features whose purpose is to evade
provider quotas or terms (e.g. automated multi-account rotation to defeat free
tiers).
