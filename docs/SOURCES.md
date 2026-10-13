# Public specifications referenced (clean-room)

freebie-frog is a **clean-room implementation**. It is built only from public API
specifications and general, well-known architectural patterns. No source code,
file layout, module naming, or comments were taken from any other implementation
(open or closed). Functionality and public API shapes are not copyrightable;
only specific code expression is, and none is reused here.

## Specifications consulted

- **Anthropic Messages API** — request/response schema and the server-sent
  events (SSE) streaming event sequence the agent CLIs expect.
- **OpenAI Chat Completions API** — request/response schema, streaming chunk
  format, and tool/function calling semantics.
- **Individual provider API docs** — added here as each provider adapter is
  implemented.

## Discipline

See `PLAN.md` §0 for the clean-room rules this project follows. In short: design
from the public specs above, never from another implementation's code; keep this
file current; choose our own architecture, naming, and license.
