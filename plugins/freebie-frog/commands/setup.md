---
description: Install freebie-frog, create config.json, pick a free provider, store its key safely, and verify with doctor.
argument-hint: "[provider]  (optional: groq | openrouter | deepseek | nvidia)"
allowed-tools: Bash, Read, Edit, Write
---

You are helping the user set up **freebie-frog** — an Anthropic-API-compatible,
multi-provider proxy whose binary is `freebie`. Walk through the steps below in
order. Explain what each step does before running it, stop if a step fails, and
**never print, echo, or store a secret key value** — always reference keys
indirectly (see step 4).

The user optionally named a provider: `$ARGUMENTS`. If empty, help them choose in
step 4.

### 1. Verify Go is installed
Run `go version`. freebie-frog needs **Go 1.24+**. If `go` is missing or older,
stop and point the user to <https://go.dev/dl/>; do not try to install Go for them.

### 2. Install the `freebie` binary
Run:

```sh
go install github.com/dibakshya01/freebie-frog/cmd/freebie@latest
```

Then confirm it is on `PATH` with `freebie version`. If not found, tell the user
to add their Go bin directory (usually `$(go env GOPATH)/bin`) to `PATH`.

### 3. Create a config file
If there is no `config.json` in the current directory, copy the example:

```sh
cp config.example.json config.json
```

If `config.example.json` is not present (the user is not inside a clone of the
repo), create a minimal `config.json` yourself using Write, mirroring the shape
documented in the repo's `config.example.json` (a `server.addr` of
`127.0.0.1:4000`, a `providers` map, and a `models.default` list). Read the
example first if it exists so you match the current schema exactly.

### 4. Pick a free provider and store its key safely
Free-tier, OpenAI-compatible providers freebie-frog works with include:

| Provider     | `type`   | typical `base_url`                     |
| ------------ | -------- | -------------------------------------- |
| Groq         | `openai` | `https://api.groq.com/openai/v1`       |
| OpenRouter   | `openai` | `https://openrouter.ai/api/v1`         |
| DeepSeek     | `openai` | `https://api.deepseek.com`             |
| NVIDIA NIM   | `openai` | `https://integrate.api.nvidia.com/v1`  |

Confirm the exact `base_url` and model names against the provider's own docs and
the repo's `config.example.json` before writing them.

Help the user add (or keep) a provider block in `config.json`. For the key,
**reference it, never inline it**:

- Preferred: `"api_key_ref": "keychain:FREEBIE_<PROVIDER>"` and have the user
  store the secret in their OS keychain themselves (e.g. macOS
  `security add-generic-password`, Linux `secret-tool store`). The keychain and
  encrypted-file backends are available in the security build.
- Simplest: `"api_key_ref": "env:<PROVIDER>_API_KEY"` and have the user
  `export <PROVIDER>_API_KEY=...` in their own shell.

Do **not** ask the user to paste the key into this chat, do **not** write a raw
`api_key` value into `config.json`, and do **not** read the key back out. Remind
them `config.json` is git-ignored and keys are never committed.

### 5. Verify
Run `freebie doctor` and read its output back to the user. If a check fails,
explain the failing line and suggest a fix. When it reports all checks passed,
tell them the next steps are `/freebie-frog:start` to run the proxy and
`/freebie-frog:use` to launch an agent through it.
