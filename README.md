# freebie-frog

A local, **Anthropic-API-compatible proxy** that lets coding-agent CLIs (such as
Claude Code, Codex, and Aider) run against third-party and free-tier model
providers, using **your own API keys**.

You point an agent CLI at freebie-frog via `ANTHROPIC_BASE_URL`; it receives
requests in the Anthropic Messages API format, translates them to the target
provider's format (OpenAI-compatible and others), calls the provider, and
translates the response — including the streaming event sequence — back into the
Anthropic format. A router picks the model/provider and falls back on failure.

> Independent open-source project. **Not affiliated with or endorsed by**
> Anthropic, OpenAI, or any model provider. "Claude Code", "Anthropic", "OpenAI",
> and other names are trademarks of their respective owners.

## Status

**M0 — scaffold.** The operational skeleton is in place (CLI, config loader,
health endpoint, structured logging, CI). The translating proxy itself is built
in the milestones that follow. See [`PLAN.md`](PLAN.md) for the full roadmap and
architecture, and [`docs/SOURCES.md`](docs/SOURCES.md) for the clean-room policy.

## Requirements

- Go 1.24+

## Quick start

```sh
# Build the binary into ./bin/freebie
make build

# Run diagnostics (checks config + that the port is bindable)
./bin/freebie doctor

# Print the resolved configuration (defaults + file + env)
./bin/freebie config

# Start the server (defaults to :4000; Ctrl-C to stop)
./bin/freebie serve

# In another shell:
curl -s localhost:4000/healthz
# {"service":"freebie-frog","status":"ok","version":"dev"}
```

## Configuration

Configuration is resolved in this order (later wins): **defaults → JSON file →
environment variables**, then validated.

- File: pass `--config path.json`, or drop a `config.json` in the working
  directory. See [`config.example.json`](config.example.json).
- Environment overrides: `FREEBIE_SERVER_ADDR`, `FREEBIE_LOG_LEVEL`,
  `FREEBIE_LOG_FORMAT`, `FREEBIE_SERVER_READ_TIMEOUT`,
  `FREEBIE_SERVER_WRITE_TIMEOUT`, `FREEBIE_SERVER_SHUTDOWN_TIMEOUT`.

> `write_timeout` defaults to `0` (no timeout) on purpose — the proxy's core job
> is long-lived SSE streaming, which a non-zero write timeout would cut off.

**Never commit secrets.** `config.json` and `.env` are git-ignored; provider keys
belong there (or in environment variables), not in version control.

## Development

```sh
make check   # fmt-check + vet + test + build (what CI runs)
make test    # go test -race ./...
make fmt     # gofmt -w .
```

## Layout

```
cmd/freebie/        CLI entrypoint (serve, config, doctor, version)
internal/config/    configuration + validation
internal/obs/       structured logging (log/slog)
internal/server/    HTTP server (health now; Messages API later)
internal/version/   build version
docs/               clean-room sources and notes
```

## License

TBD (a permissive license such as MIT or Apache-2.0 is planned; valid precisely
because this is a clean-room implementation — see `PLAN.md` §0 and §9).
