---
description: Start the freebie-frog proxy (freebie serve) and explain how to reach and stop it.
argument-hint: "[--config <path>]"
allowed-tools: Bash
---

Start the **freebie-frog** proxy for the user.

### What it does
`freebie serve` runs the Anthropic-compatible proxy. By default it binds the
**loopback address `127.0.0.1:4000`**, so it is reachable only from this machine.
It refuses to bind a non-loopback address unless `server.auth_token` is set in the
config. Config is read from `--config <path>` if given, else `./config.json` if
present, else built-in defaults plus environment overrides.

### Run it
Pass through any arguments the user supplied: `$ARGUMENTS`.

```sh
freebie serve $ARGUMENTS
```

`serve` runs in the **foreground** and stays up until stopped. Decide with the
user how they want it:

- To keep working in this session, run it in the background (e.g. append `&`, or
  use a separate terminal / `tmux`), then confirm it is up with
  `curl -s localhost:4000/healthz`.
- To watch its logs, run it in the foreground; it blocks until you stop it.

### Reach it
Once running:

```sh
curl -s localhost:4000/healthz
curl -s localhost:4000/v1/models
```

### Stop it
Press **Ctrl-C** in its terminal (it handles SIGINT/SIGTERM and shuts down
gracefully). If it was backgrounded, stop it with `kill <pid>` for the `freebie
serve` process.

### Notes
- If `serve` warns that no providers are configured, the Messages API returns 503
  until one is added — run `/freebie-frog:setup` or edit `config.json`.
- To actually route an agent through the proxy, use `/freebie-frog:use` — note a
  Claude Code session reads `ANTHROPIC_BASE_URL` only at startup (see that
  command).
