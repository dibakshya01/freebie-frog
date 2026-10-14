---
description: Launch an agent (e.g. Claude Code) wired to the freebie-frog proxy, and explain the base-URL nuance.
argument-hint: "[agent]  (default: claude)"
allowed-tools: Bash
---

Help the user actually route a coding agent through **freebie-frog**.

The agent to launch is `$ARGUMENTS` (default: `claude`).

### Important: you cannot re-route the session you are in
A Claude Code session reads `ANTHROPIC_BASE_URL` (and the auth token) **once, at
startup**. This plugin runs *inside* an already-started session, so it **cannot
redirect the current session** to the proxy. Be honest about this. There are two
honest ways to "use freebie-frog in a session":

1. **Launch a new agent through the proxy** (recommended). `freebie run` sets
   `ANTHROPIC_BASE_URL` for the child process it spawns:

   ```sh
   freebie run claude
   ```

   This starts a fresh `claude` already pointed at `http://127.0.0.1:4000`. The
   proxy must be running first (`/freebie-frog:start`).

2. **Relaunch with the env var set yourself**, then start the agent:

   ```sh
   export ANTHROPIC_BASE_URL=http://127.0.0.1:4000
   export ANTHROPIC_AUTH_TOKEN=local   # any non-empty value; real keys live in freebie's config
   claude
   ```

### What to do
1. Confirm the proxy is up: `curl -s localhost:4000/healthz`. If it is not,
   tell the user to run `/freebie-frog:start` first.
2. Offer option 1 and run it for them if they agree:

   ```sh
   freebie run $ARGUMENTS
   ```

   (`freebie run <agent>` launches the named agent; `claude` is the common case.)
3. If they would rather wire it manually, give them option 2's commands to run in
   their own terminal.

Make clear that whichever path they choose starts a **new** agent process pointed
at the proxy — the session issuing this command keeps its original base URL.
