# Design: Matrix App-Service Rebrand (Camino Messenger → Travel Token Messenger)

Date: 2026-07-18
Status: Approved (ready for implementation plan)

## Goal

Rebrand this repository from **Camino Messenger** to **Travel Token
Messenger**, in lockstep with the already-rebranded sibling repos
(contracts, protocol, and the messenger **bot**). This is the last of the
four Go/Solidity ecosystem repos to migrate.

The service itself is a small **mautrix-based Matrix appservice bridge**
(~9 `.go` files). It relays custom Matrix events opaquely between the bot
and the conduit; it does **not** hard-filter on event-type strings or read
the sender-account field (proven empirically: the bot's end-to-end suite
passed while this service was still built against the pre-rebrand bot). So
the rebrand here is about **module-path, import, and identifier
consistency** — not about unbreaking message relay.

## Authoritative naming decisions (ecosystem-wide, already settled)

These were decided during the contracts rebrand and apply to every repo.
Do not re-litigate them.

- Brand name: **"Travel Token Messenger"** (three words, title case in prose).
- One-word / PascalCase form: `TravelTokenMessenger`.
- Identifier prefix: **`TTM`** replaces `CM` / `CaminoMessenger`
  (and `CMP` → `TTM` for the protocol-release version fields here).
- Protocol/service namespace: **`ttm.`** replaces `cmp.`; the buf.build
  module is **`buf.build/ttm/messenger-protocol`** (owner `ttm`, module
  `messenger-protocol`), replacing owner `chain4travel` / module
  `camino-messenger-protocol`.
- Go module paths live under
  `github.com/TravelTokenMarketplace/travel-token-messenger-<thing>`.
- Copyright headers: `Chain4Travel AG` → **`Travel Token Marketplace`**
  (matches the bot; the `camino-license` header-check tool and the header
  name `c4t` are kept as-is — only the copyright entity string changes).

### Intentionally-kept external `camino-*` references

These are **not** in scope — they are external dependencies/tools that keep
their upstream names, exactly as the bot did:

- `github.com/chain4travel/camino-license@v0.1.0` — the license-header
  checker invoked by `scripts/lint.sh`.

(Note: this repo has **no** `camino-matrix-go` submodule or mautrix
`replace` — it depends on upstream `maunium.net/go/mautrix` directly. The
two `camino-matrix-go` entries in `.golangci.yml` exclusion lists are dead
references to a path that does not exist here and are removed in Phase 3.)

## Start state (facts)

- Local clone on branch `dev` @ `20e033e` ("Fix bot repo SHA"), clean tree.
- Remote `origin` still points at the **old** GitHub repo
  `git@github.com:TravelTokenMarketplace/camino-matrix-app-service.git`
  (exists, **public**, default branch `dev`). The new repo
  `travel-token-matrix-app-service` does **not** exist yet.
- `go.mod`: `module github.com/chain4travel/camino-matrix-app-service`;
  `require github.com/chain4travel/camino-messenger-bot/v13 v13.0.0` with a
  `replace` pointing that at a **pre-rebrand** bot commit
  (`...98f847f3a5e6`). Two unrelated `replace`s (`gnark-crypto`, `quic-go`)
  are left untouched.
- Sibling bot is done: module
  `github.com/TravelTokenMarketplace/travel-token-messenger-bot/v13`, branch
  `dev` @ `c49ae0e`, with the rebranded wire contract in `pkg/matrix`
  (`EventTypeSignedMessage = "m.room.ttm-signed-msg"`,
  `EventTypeMessageChunk = "m.room.ttm-msg-chunk"`, field
  `SenderTTMAccountAddress`).

## Coupling (what this repo actually imports)

- **From the bot (real source imports):**
  - `.../v13/pkg/matrix` — used in `internal/service/service.go`.
  - `.../v13/pkg/database/sqlite` — used in
    `internal/storage/sqlite/{storage,chunked_messages}.go`.
- **Protocol & contracts (NOT source imports):** they appear only as
  dependency-**path strings** in `internal/version/version.go`, matched
  against `debug.ReadBuildInfo().Deps` to report versions, plus the buf
  owner/module strings in `scripts/resolve_protocol_release.sh`. These deps
  reach the build graph **transitively through the bot**, so re-pinning the
  bot is what changes their paths — the version-string updates must land in
  the same phase as the re-pin, or version reporting silently degrades to
  `"Unspecified"`.
- **No chain surface:** this repo has no chain IDs, RPC URLs, contract
  addresses, or on-chain config. The ecosystem "chain move" (Base Sepolia)
  does not touch it.

## Key derived constraint: the built-binary name

`Dockerfile` runs `go build -o build/` (no explicit binary name), so the
output binary is named after the **module path's last segment**. Renaming
the module in Phase 1 changes the binary from `camino-matrix-app-service` to
`travel-token-matrix-app-service`, which would break the Dockerfile
`ENTRYPOINT` and the CI `build_docker` job. Therefore the Dockerfile's
functional paths (`WORKDIR`, `COPY`, `ENTRYPOINT`) and `scripts/build.sh`'s
`-ldflags -X <module>/internal/version.*` package paths **move in Phase 1**,
because they are direct consequences of the new module/binary name. The
cosmetic image *tag* string and prose wait for Phase 3.

## Process

Subagent-driven-development: one fresh implementer per phase, a task review
after each, and a whole-branch review at the end. **One squash-merged PR per
phase → `dev`; the user performs every merge.** Every phase must leave
`scripts/build_test.sh` + `scripts/lint.sh` (+ the `build_docker` CI job)
green. Env on this disk-tight box routes all Go scratch to `/hgst` and sets
`GOPRIVATE='github.com/TravelTokenMarketplace/*'`.

## Phases

### Phase 0 — GitHub migration (critical, outward-facing, user-gated)

Not a code change; the irreversible publish step. On the user's explicit go:
create `TravelTokenMarketplace/travel-token-matrix-app-service` (**public**,
matching the old repo), rewire remotes (`git remote rename origin old`;
add `origin` → new), `git push origin --all && git push origin --tags`, set
the new repo's default branch to `dev`. The old repo stays as remote `old`
for reference. Mirrors exactly how the bot repo was migrated.

### Phase 1 — Go module path rename

- `module github.com/chain4travel/camino-matrix-app-service` →
  `github.com/TravelTokenMarketplace/travel-token-matrix-app-service` in
  `go.mod`.
- Every internal self-import (`main.go`, `cmd/`, `internal/app`,
  `internal/storage/sqlite`, `internal/version`, etc.).
- `scripts/build.sh` ldflags `-X <old-module>/internal/version.*` →
  new module path (module-path-coupled; a wrong path silently no-ops the
  version stamping).
- `Dockerfile` functional paths (`WORKDIR`, `COPY`, `ENTRYPOINT`) to the new
  binary name `travel-token-matrix-app-service` (binary-name-coupled — see
  "Key derived constraint").
- **Out of scope for this phase:** bot/protocol/contracts import paths, any
  `CM`/`CMP`/`camino` *identifier* or *prose* changes, the Docker image tag.
- **Green gate:** `go build ./...`, `scripts/build_test.sh`,
  `scripts/lint.sh`, `go mod tidy` clean.

### Phase 2 — Re-pin bot dependency + version-reporting strings

- `go.mod`: `require` →
  `github.com/TravelTokenMarketplace/travel-token-messenger-bot/v13` at the
  rebranded-`dev` pseudo-version (bot `dev` @ `c49ae0e`, resolved via
  `go get .../travel-token-messenger-bot/v13@c49ae0e`); **delete** the old
  `replace github.com/chain4travel/camino-messenger-bot/v13 => ...98f847f`.
- Update the two bot source-import paths (`pkg/matrix`,
  `pkg/database/sqlite`) to the new module. This pulls in the
  `m.room.ttm-*` event types and `SenderTTMAccountAddress`.
- `internal/version/version.go` `debug.ReadBuildInfo` switch cases:
  - `buf.build/gen/go/chain4travel/camino-messenger-protocol/protocolbuffers/go`
    → `buf.build/gen/go/ttm/messenger-protocol/protocolbuffers/go`
  - `.../grpc/go` likewise.
  - `github.com/chain4travel/camino-messenger-contracts/go/contracts` →
    `github.com/TravelTokenMarketplace/travel-token-messenger-contracts/go/contracts`
- `scripts/resolve_protocol_release.sh`: buf `owner` `chain4travel` →
  `ttm`, `module` `camino-messenger-protocol` → `messenger-protocol`, and
  the usage-example dep path.
- **Green gate:** `go mod tidy` clean, `scripts/build_test.sh` passes;
  the transitive protocol/contracts dep paths present in the new build graph
  match the updated `version.go` cases (spot-check that version reporting is
  not `"Unspecified"`).

### Phase 3 — Identifiers, infra (cosmetic), config, docs, prose

- Version identifiers: `CMP` → `TTM` in `internal/version/version.go`
  (`BufBuildPBCMPRelease` → `BufBuildPBTTMRelease`, etc.) and the matching
  `-ldflags -X` names in `scripts/build.sh`, plus the log lines in
  `cmd/app_service.go` (`(CMP %s)` → `(TTM %s)`,
  `camino-messenger-contracts` label → `travel-token-messenger-contracts`).
- `cmd/app_service.go`: command `Use`/`Short`/aliases and any
  `camino`/`Camino Messenger` prose → Travel Token forms.
- `config/flags.go`: default config filename
  `camino-matrix-app-service.yaml` → `travel-token-matrix-app-service.yaml`;
  default db dir `camino-matrix-app-service-db` →
  `travel-token-matrix-app-service-db`. Update `config/test_config.yaml`
  and the `example/config/` files to match; `git mv`
  `example/config/camino-matrix-app-service.yaml` →
  `travel-token-matrix-app-service.yaml`.
- `example/config/synapse/camino.yaml`: `git mv` → `ttm.yaml`; registration
  `id`/`sender_localpart` `camino` → `ttm`; the `hs_token` comment mentioning
  `camino_app_service_hs_token`.
- `.github/workflows/ci.yml`: PR trigger `branches: [c4t, dev]` → `[dev]`
  (the pre-rebrand `c4t` default branch is gone); Docker tag
  `c4tplatform/camino-matrix-app-service:temp` →
  `travel-token-matrix-app-service:temp` (drop the abandoned `c4tplatform/`
  Docker Hub org, matching the bot's GHCR-only move; `push:false`, so the
  tag is only a local build label here).
- `.golangci.yml`: remove the two dead `camino-matrix-go` exclusion-path
  entries (no such path in this repo).
- `header.yaml`: copyright entity `Chain4Travel AG` → `Travel Token
  Marketplace` (keep header name `c4t` and the tool).
- Copyright headers in all `.go` files: `Chain4Travel AG` → `Travel Token
  Marketplace`.
- `README.md`: prose pass — Camino Messenger → Travel Token Messenger,
  config filenames, and the `camino-matrix-app-service` product name.
  (Leave the `camino-conduit` references: that sibling is not being renamed
  as part of this job; if it is later, it is a separate change.)
- **Delete `DATA_PROTECTION.md`** — it names Camino Network / Chain4Travel
  AG / Camino Network Foundation as legal entities, exactly the document the
  protocol and bot repos removed. A fresh Travel Token data-protection text
  is a deferred loose-end, not part of this rebrand.
- Any `scripts/constants.sh` / `scripts/build.sh` remaining prose comments
  referencing the old repo name.
- **Final sweep:** `grep -rin camino --exclude-dir={.git,vendor} .` — the
  only allowed leftovers are the intentionally-kept `camino-license` tool
  reference and this dated design doc. Everything else must be gone.
- **Green gate:** `scripts/build_test.sh`, `scripts/lint.sh`,
  `scripts/shellcheck.sh`, `go mod tidy` clean.

## Deferred loose-ends (record, do not fix here)

- The example Matrix homeserver / access tokens are placeholders (as they
  always were); no live homeserver rename is part of this job.
- A fresh `DATA_PROTECTION.md` for the Travel Token product, when legal text
  exists.

## Out of scope

- Renaming or touching the `camino-conduit` sibling repo.
- The ecosystem chain move (Base Sepolia) — no surface in this repo.
- Any dev→main promotion or release; the user handles all merges/releases.
