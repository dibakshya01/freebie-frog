---
description: Run freebie doctor and interpret the environment/config diagnostics.
argument-hint: "[--config <path>]"
allowed-tools: Bash
---

Run the **freebie-frog** diagnostics and explain the results to the user.

### Run it
Pass through any arguments the user supplied: `$ARGUMENTS`.

```sh
freebie doctor $ARGUMENTS
```

### Interpret the output
`freebie doctor` prints a short report. Read each line back to the user in plain
language and act on any failure:

- **version / go runtime** — informational: the freebie-frog version and the Go
  runtime it was built with.
- **config loaded and valid** — the config was found and parsed. A `[FAIL]
  config: ...` line means the JSON is malformed or a value is invalid; open the
  config (default `./config.json`, or the `--config` path) and fix the reported
  problem.
- **address is bindable** — the configured `server.addr` is free. A `[FAIL]
  cannot bind ...` line usually means something is already listening on that port
  (perhaps freebie-frog is already running) or the address is not permitted;
  suggest stopping the other process or changing `server.addr`.

If it ends with **all checks passed**, tell the user they are ready to
`/freebie-frog:start` the proxy and `/freebie-frog:use` an agent through it. If
any check fails, summarize the single most likely fix.
