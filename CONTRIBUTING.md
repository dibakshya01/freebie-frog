# Contributing to freebie-frog

Thanks for your interest in improving freebie-frog! This document explains how to
build, test, and submit changes.

## Ground rules

- Be respectful — see [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
- **Clean-room policy.** freebie-frog is a clean-room implementation built only
  from public API specifications (see [docs/SOURCES.md](docs/SOURCES.md)). Do
  **not** copy, paste, or "port" source code, file layouts, or comments from
  other projects — especially copyleft (GPL/AGPL) projects. Contribute only code
  you wrote yourself (or that is permissively licensed and clearly attributed).
- By contributing, you agree your contribution is licensed under the project's
  [Apache License 2.0](LICENSE).

## Development setup

Requirements: **Go 1.24+**.

```sh
git clone https://github.com/dibakshya01/freebie-frog
cd freebie-frog
make check     # fmt-check + vet + test + build (what CI runs)
```

Common targets:

```sh
make build     # compile ./bin/freebie
make test      # go test -race ./...
make fmt       # gofmt -w .
make run       # build and start the server
```

## Making a change

1. Open an issue first for anything non-trivial, so we can agree on the approach.
2. Create a branch from `main`.
3. Keep changes focused. Match the surrounding code's style; run `make fmt`.
4. **Add tests.** Every bug fix gets a regression test; every feature gets
   coverage. Translation changes need table tests and, for streaming, a snapshot
   of the exact SSE byte sequence.
5. Run `make check` and make sure it passes.
6. Write a clear commit message (imperative mood; explain the *why*).
7. Open a pull request against `main` and fill in the template.

## Architecture orientation

See [PLAN.md](PLAN.md) and `build-plan.md` for the design, and
[docs/SOURCES.md](docs/SOURCES.md) for the clean-room policy. In short:

- `internal/anthropic` / `internal/openai` — wire-format types.
- `internal/translate` — the bidirectional Anthropic↔provider translation,
  including the streaming SSE reconstruction (the correctness-critical part).
- `internal/provider` — provider adapters.
- `internal/router` — model/tier routing and fallback.
- `internal/server` — the Anthropic-compatible HTTP surface.

## Security

Please do **not** open public issues for security vulnerabilities. See
[SECURITY.md](SECURITY.md) for private reporting.

## Reporting bugs / requesting features

Use the issue templates. Include your OS, Go version, `freebie version`, the
provider/model involved, and a minimal reproduction. Never paste real API keys,
tokens, or full prompts containing secrets into an issue.
