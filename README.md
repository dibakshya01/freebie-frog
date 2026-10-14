# freebie-frog

[![CI](https://github.com/dibakshya01/freebie-frog/actions/workflows/ci.yml/badge.svg)](https://github.com/dibakshya01/freebie-frog/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![PRs welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

A single-binary, **Anthropic-API-compatible proxy** that lets coding-agent CLIs
(such as Claude Code) run against third-party, free-tier, or local model providers —
using **your own API keys**, with correct streaming and automatic fallback.

Point your agent at freebie-frog via `ANTHROPIC_BASE_URL`; it receives requests in
the Anthropic Messages API format, translates them to the target provider
(OpenAI-compatible and Gemini), calls the provider, and translates the response —
**including the streaming event sequence** — back into the Anthropic format.

> Independent open-source project. **Not affiliated with or endorsed by** Anthropic,
> OpenAI, Google, or any model provider. "Claude Code", "Anthropic", "OpenAI",
> "Gemini", and other names are trademarks of their respective owners.

**Website:** the full guide — home, plain-English ELI5, docs, and the
architecture diagrams — is live at
**<https://dibakshya01.github.io/freebie-frog/>** (source in [`docs/`](docs/)).

## Why

- **Provider choice** — one generic OpenAI-compatible adapter reaches NVIDIA NIM,
  Groq, DeepSeek, OpenRouter, Together, Mistral, and local servers (Ollama,
  llama.cpp, vLLM, LM Studio); plus a native Gemini adapter and an Anthropic passthrough.
- **Security-first defaults** — SSRF-guarded outbound calls, OS-keychain secret
  storage, loopback bind, no secret logging.
- **Honest** — it uses *your* keys and respects provider terms (single-account,
  BYO-key; no quota-evasion). Free-tier availability and limits are the providers'.
  See [docs/CAPABILITIES.md](docs/CAPABILITIES.md).

## Requirements

- Go 1.24+

## Install

```sh
go install github.com/dibakshya01/freebie-frog/cmd/freebie@latest
# or, from a clone:
make build   # -> ./bin/freebie
```

## Quick start

```sh
# 1. Create a config (see config.example.json) and set your key(s)
cp config.example.json config.json
export GROQ_API_KEY=gsk_...

# 2. Check it
freebie doctor

# 3. Run the proxy (binds 127.0.0.1:4000 by default)
freebie serve

# 4. Point an agent at it, or launch one for you:
freebie run claude        # launches Claude Code wired to the proxy
# ...or manually:
export ANTHROPIC_BASE_URL=http://127.0.0.1:4000
export ANTHROPIC_AUTH_TOKEN=local   # any non-empty value; real keys live in freebie's config
claude
```

Check health directly:

```sh
curl -s localhost:4000/healthz
curl -s localhost:4000/v1/models
```

## Configuration

Resolved in order (later wins): **defaults → JSON file → environment**, then
validated. Pass `--config path.json` or drop a `config.json` in the working directory.

```jsonc
{
  "server": { "addr": "127.0.0.1:4000" },
  "providers": {
    "groq":   { "type": "openai", "base_url": "https://api.groq.com/openai/v1",
                "api_key_ref": "env:GROQ_API_KEY", "models": ["llama-3.3-70b-versatile"] },
    "ollama": { "type": "openai", "base_url": "http://localhost:11434/v1",
                "allow_local": true, "models": ["qwen2.5-coder:7b"] }
  },
  "models": {
    "default": ["groq/llama-3.3-70b-versatile", "ollama/qwen2.5-coder:7b"],
    "tiers":   { "haiku": ["groq/llama-3.1-8b-instant", "ollama/qwen2.5-coder:7b"] }
  }
}
```

- **Keys** — prefer `api_key_ref` over an inline `api_key`. Forms: `env:NAME`
  (OS keychain and encrypted-file backends are available in the security build).
  Keys are **never logged**, and `config.json` is git-ignored.
- **Routing** — an incoming model matches an exact `aliases` entry, else a `tiers`
  entry by family substring (`opus`/`sonnet`/`haiku`/`fable`), else `default`. Each
  value is an ordered list of `provider/model` targets tried in turn (fallback).
- **`allow_local`** — required for a provider whose host is loopback/private (e.g. a
  local model); the SSRF guard still validates the resolved IP at dial time.

## CLI

```
freebie serve      Start the proxy
freebie run <a>    Launch an agent (e.g. claude) wired to the proxy
freebie config     Print the resolved configuration
freebie doctor     Environment/config diagnostics
freebie providers  List configured providers and advertised models
freebie version    Print the version
```

## Honest limits

Token counts are approximate; not every Anthropic feature maps to every provider;
a mid-stream provider failure cannot be transparently retried; and this is a local,
single-user tool, not a multi-tenant security boundary. Full list:
[docs/CAPABILITIES.md](docs/CAPABILITIES.md). Security model:
[docs/THREAT_MODEL.md](docs/THREAT_MODEL.md).

## Development

```sh
make check   # fmt-check + vet + test -race + build (what CI runs)
```

See [build-plan.md](build-plan.md) for the architecture and milestones,
[finding.md](finding.md) for the research, and [docs/SOURCES.md](docs/SOURCES.md)
for the clean-room policy.

## License

[Apache-2.0](LICENSE). Clean-room implementation (see `docs/SOURCES.md`); the
permissive license is valid precisely because it is not derived from any copyleft
source.
