# freebie-frog — Claude Code plugin

Slash commands for driving the **freebie-frog** proxy (`freebie`) from inside
Claude Code: install it, write a config, pick a free provider and store its key
safely, start the proxy, check its health, and launch an agent through it.

> This plugin wraps the `freebie` CLI with guided prompts. It does **not** bundle
> the binary — it helps you install and run the one you build or `go install`.

## Install

In a Claude Code terminal session:

```text
/plugin marketplace add dibakshya01/freebie-frog
/plugin install freebie-frog
```

The first line adds this repository as a plugin marketplace (answer `y` to add
it, then pick a scope); the second installs the plugin. Commands are then
available as `/freebie-frog:<command>`.

On Claude Code 2.1.275 or later you can do both in one step:

```text
/plugin install freebie-frog --marketplace dibakshya01/freebie-frog
```

From a shell instead:

```sh
claude plugin marketplace add dibakshya01/freebie-frog
claude plugin install freebie-frog
```

## Commands

| Command                   | What it does                                                                                 |
| ------------------------- | ------------------------------------------------------------------------------------------- |
| `/freebie-frog:setup`     | Verify Go, `go install` the binary, create `config.json`, pick a free provider, store its key via a keychain/env ref, then run `freebie doctor`. |
| `/freebie-frog:start`     | Start `freebie serve` (binds loopback `127.0.0.1:4000`); explains how to reach and stop it. |
| `/freebie-frog:doctor`    | Run `freebie doctor` and interpret the environment/config diagnostics.                        |
| `/freebie-frog:providers` | Run `freebie providers` and summarize the configured providers.                               |
| `/freebie-frog:use`       | Launch an agent via `freebie run claude` (or show the manual env-var path).                   |

## The base-URL nuance

`/freebie-frog:use` cannot redirect the **current** Claude Code session: the agent
reads `ANTHROPIC_BASE_URL` only at startup. To use the proxy you either launch a
new agent with `freebie run claude` (which sets the base URL for the child
process), or relaunch your agent yourself with `ANTHROPIC_BASE_URL` exported. The
commands say this plainly.

## Safety

- Keys are referenced indirectly via `api_key_ref` (`keychain:NAME` or
  `env:NAME`) — the commands never print, echo, or store raw key values, and
  `config.json` is git-ignored.
- `freebie serve` binds loopback by default and refuses a non-loopback bind
  without an auth token.

## See also

- Main project: <https://github.com/dibakshya01/freebie-frog>
- Full guide: <https://dibakshya01.github.io/freebie-frog/>
- Config template: [`config.example.json`](../../config.example.json)

Licensed under [Apache-2.0](../../LICENSE).
