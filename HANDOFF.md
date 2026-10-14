# Handoff — things only you can do

This file is maintained throughout the build. Items are things that require your
credentials, your accounts, legal/irreversible decisions, or spending — Claude
prepares everything up to the final click.

## What's shipped (so you know the state)

The full proxy is implemented, tested, and pushed to branch `m0-scaffold`:
Anthropic-compatible ingress (`/v1/messages` streaming + non-streaming,
`count_tokens`, `models`), Anthropic↔OpenAI/Gemini translation, generic
OpenAI-compatible + native Gemini + Anthropic-passthrough adapters, model/tier
routing with fallback, secret backends (keychain / encrypted-file / env), SSRF
guard, ingress auth, agent launchers, and a read-only admin dashboard. `go test
-race ./...`, `go vet`, and `gofmt` are clean; it was hardened across repeated
adversarial review rounds. It is pre-1.0 — evaluate before depending on it.

Everything below needs **you** (accounts, keys, publishing, posting).

### Done in this session (no action needed)

- Code built, hardened (3 adversarial rounds), merged to `main` via PR #1.
- Verified end-to-end with the real binary against a live local provider.
- Social preview card rendered → `docs/assets/social-card.png` (ready to upload).
- Pages deploy workflow added (`.github/workflows/pages.yml`).
- CoC/SECURITY reporting finalized (GitHub private vulnerability reporting).
- Branch renamed to `feat/initial-implementation` (now merged).

### Blocked in this environment (org policy / sandbox) — trivial for you

- **Tag the release:** the git proxy denies tag pushes here (403). From your own
  clone: `git tag v0.1.0 && git push origin v0.1.0` (or GitHub UI → Releases →
  Draft new release → tag `v0.1.0` on `main` → Publish). This triggers GoReleaser
  and makes `go install github.com/dibakshya01/freebie-frog/cmd/freebie@v0.1.0` work.
- **`govulncheck`:** the vuln DB is blocked by this sandbox; it runs in CI (already
  wired) and on your machine (`govulncheck ./...`).
- **Delete the stale `m0-scaffold` remote branch** (ref deletion denied here): one
  click in the GitHub branches UI.

## 1. Run against a real provider (needed to use the tool for real)

The proxy is verified end-to-end against an in-process **mock** provider (no
network, no keys). To use it with a real free-tier provider:

1. Get a free API key from a provider (e.g. NVIDIA NIM, Groq, OpenRouter,
   DeepSeek, Google AI Studio). These require creating an account — your call.
2. Add it to freebie-frog config (see README "Configuration"). Keys live only on
   your machine; never commit them.

## 2. Repo / infra (your GitHub account)

- [ ] **Enable GitHub Pages** (Settings → Pages → Deploy from `main` `/docs`) to
      publish the site Claude built in `docs/`.
- [ ] **Upload the social preview card** (Settings → Social preview) —
      `docs/assets/social-card.png` once generated.
- [ ] **Enable Discussions** (Settings → Features) — the issue template links to it.
- [ ] (Optional) **Branch protection / ruleset** on `main` (require CI + review).
- [ ] (Optional) **Custom domain** for Pages + DNS.

## 3. Contacts to set

- [ ] Add a dedicated **conduct contact** in `CODE_OF_CONDUCT.md` (replace the
      placeholder paragraph).
- [ ] (Optional) Add a dedicated **security contact email** in `SECURITY.md`
      (GitHub private vulnerability reporting already works without one).

## 4. Distribution (publishing a release — your account/tokens)

freebie-frog is a CLI/daemon, so it is **not** eligible for the GitHub
Marketplace (that lists only Actions and Apps). Distribute it via:

- [ ] **`go install github.com/dibakshya01/freebie-frog/cmd/freebie@latest`** —
      works as soon as a version is tagged; no extra setup.
- [ ] **Tagged release with prebuilt binaries** — Claude will add a GoReleaser
      config + release workflow; you push the tag / authorize the release.
- [ ] (Optional) **Homebrew tap** — needs a `homebrew-tap` repo you own.

## 5. Launch (your voice, your accounts)

- [ ] Draft launch copy (HN / Reddit / socials) will be left in a git-ignored
      `LAUNCH.md` for you to post. Posting is yours.

---
_Nothing here blocks the build; everything not requiring you is being built now._
