---
description: List the providers configured in freebie-frog and summarize them.
argument-hint: "[--config <path>]"
allowed-tools: Bash
---

Show the user which providers **freebie-frog** currently has configured.

### Run it
Pass through any arguments the user supplied: `$ARGUMENTS`.

```sh
freebie providers $ARGUMENTS
```

Each line names a provider with its `type`, how many models it advertises, its
`base_url`, and an `[allow_local]` marker for providers allowed to reach a
loopback/private host (e.g. a local model server).

### Summarize
Read the list back and briefly tell the user:

- How many providers are configured and their names.
- Which look like hosted free-tier endpoints (e.g. Groq, OpenRouter, DeepSeek,
  NVIDIA NIM) versus local ones (marked `[allow_local]`).
- If the output is **"no providers configured"**, point them to
  `/freebie-frog:setup` or to editing `config.json` (see `config.example.json`).

Do not read or print any API key values — this command only lists provider
metadata, and keys are referenced indirectly via `api_key_ref`.
