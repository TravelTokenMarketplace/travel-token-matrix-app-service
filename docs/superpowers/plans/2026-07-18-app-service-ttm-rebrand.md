# Matrix App-Service Rebrand (Camino → Travel Token Messenger) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebrand the `travel-token-matrix-app-service` repo from Camino Messenger to Travel Token Messenger — module path, bot dependency, version strings, identifiers, infra, and prose — in three build-green phases, each a squash-merged PR to `dev`.

**Architecture:** Mechanical, case-aware find/replace ordered by dependency layer (module path → bot re-pin + transitive version strings → identifiers/infra/prose), with each phase gated on `scripts/build_test.sh` + `scripts/lint.sh` passing. The service is a small mautrix appservice bridge (~9 `.go` files); it relays events opaquely, so no behavioral logic changes — verification is build + existing test suite + lint + a `grep` sweep, not new unit tests (writing tests for a rename adds nothing: YAGNI).

**Tech Stack:** Go 1.25.10, mautrix, golangci-lint v2.7.1, `camino-license` header checker (kept), Docker.

## Global Constraints

- **Env (disk-tight box) — export before ANY `go`/build/test/lint command in every task:**
  ```bash
  export TMPDIR=/hgst/work/.ttm-scratch GOTMPDIR=/hgst/work/.ttm-scratch \
         GOCACHE=/hgst/work/.ttm-scratch/gocache \
         GOMODCACHE=/hgst/work/.ttm-scratch/gomod \
         GOPRIVATE='github.com/TravelTokenMarketplace/*'
  ```
  (`/hgst/work/.ttm-scratch` already exists.)
- **Old → new Go module path:** `github.com/chain4travel/camino-matrix-app-service` → `github.com/TravelTokenMarketplace/travel-token-matrix-app-service`.
- **Old → new bot module:** `github.com/chain4travel/camino-messenger-bot/v13` → `github.com/TravelTokenMarketplace/travel-token-messenger-bot/v13`, pinned at bot `dev` @ commit `c49ae0e` (drop the existing `replace`).
- **Old → new buf.build protocol module:** owner `chain4travel` / module `camino-messenger-protocol` → owner `ttm` / module `messenger-protocol`; Go gen paths `buf.build/gen/go/chain4travel/camino-messenger-protocol/...` → `buf.build/gen/go/ttm/messenger-protocol/...`.
- **Old → new contracts module:** `github.com/chain4travel/camino-messenger-contracts/go/contracts` → `github.com/TravelTokenMarketplace/travel-token-messenger-contracts/go/contracts`.
- **Identifier prefix:** `CMP` → `TTM` (version-release fields); `camino` → `ttm` in config/registration identifiers; product name `camino-matrix-app-service` → `travel-token-matrix-app-service`.
- **Copyright entity:** `Chain4Travel AG` → `Travel Token Marketplace` (keep header name `c4t` and the `camino-license` tool).
- **Intentionally kept (do NOT rename):** `github.com/chain4travel/camino-license@v0.1.0` (external tool); `camino-conduit` references in README (separate sibling repo, not in this job).
- **Each phase must end green:** `scripts/build_test.sh` passes and `scripts/lint.sh` passes; `go mod tidy` leaves the tree clean.
- **One squash-merged PR per phase → `dev`; the USER performs every merge.** Do not merge.

---

### Task 0: GitHub repo migration (CONTROLLER-EXECUTED, user-gated — NOT a subagent)

This task is the one irreversible, outward-facing step. It is performed by the controller **only after the user gives explicit go-ahead**, not dispatched to an implementer subagent. It creates the public GitHub repo and publishes full history. Skip if `git remote get-url origin` already points at `travel-token-matrix-app-service`.

**Files:** none (git/GitHub state only).

- [ ] **Step 1: Confirm start state**

Run: `cd /hgst/work/github.com/TravelTokenMarketplace/travel-token-messenger/travel-token-matrix-app-service && git remote -v && git status --short && git log --oneline -1`
Expected: `origin` → `...camino-matrix-app-service.git`; clean tree; HEAD is the design-doc commit on `dev`.

- [ ] **Step 2: Create the new repo (public, matching old)**

Run: `gh repo create TravelTokenMarketplace/travel-token-matrix-app-service --public --description "Travel Token Messenger — Matrix appservice bridge"`
Expected: prints the new repo URL.

- [ ] **Step 3: Rewire remotes (keep old for reference)**

```bash
git remote rename origin old
git remote add origin git@github.com:TravelTokenMarketplace/travel-token-matrix-app-service.git
```

- [ ] **Step 4: Push all history + tags to the new repo**

```bash
git push origin --all
git push origin --tags
```
Expected: `dev` (and any other branches) + tags land on the new repo.

- [ ] **Step 5: Set the new repo's default branch to `dev`**

Run: `gh repo edit TravelTokenMarketplace/travel-token-matrix-app-service --default-branch dev`
Expected: no error.

- [ ] **Step 6: Verify**

Run: `git remote -v && gh repo view TravelTokenMarketplace/travel-token-matrix-app-service --json name,visibility,defaultBranchRef`
Expected: `origin` → new repo; `old` → old repo; visibility `PUBLIC`; default branch `dev`.

---

### Task 1: Go module path rename

Rename the Go module and everything mechanically coupled to it: internal self-imports, `scripts/build.sh` `-ldflags -X` package paths, and the `Dockerfile` binary name (the built binary is named after the module's last path segment, so the old `ENTRYPOINT` would break `build_docker`). **Do NOT touch** bot/protocol/contracts import paths, any `CM`/`CMP`/`camino` identifier or prose, or the Docker image *tag* — those are later phases.

**Files:**
- Modify: `go.mod` (module line)
- Modify: `main.go`, `cmd/app_service.go`, `internal/app/app.go`, `internal/app/http_server.go`, `internal/storage/sqlite/storage.go`, `internal/storage/sqlite/chunked_messages.go` (internal self-imports)
- Modify: `scripts/build.sh` (four `-X <module>/internal/version.*` flags + the module-path in build messages that use the full path)
- Modify: `Dockerfile` (`WORKDIR`, `COPY`, `ENTRYPOINT` binary name)

**Interfaces:**
- Produces: new module path `github.com/TravelTokenMarketplace/travel-token-matrix-app-service` and built binary name `travel-token-matrix-app-service`, consumed by Tasks 2–3.

- [ ] **Step 1: Create the phase branch from `dev`**

```bash
cd /hgst/work/github.com/TravelTokenMarketplace/travel-token-messenger/travel-token-matrix-app-service
git checkout dev && git checkout -b rebrand/phase-1-module-path
```

- [ ] **Step 2: Rewrite the full module path everywhere it appears as a path**

This single substitution covers the `go.mod` module line, all internal `.go` self-imports, and the `build.sh` ldflags package paths (they all share the full old prefix; bare `camino-matrix-app-service` prose is NOT matched and stays for Task 3):

```bash
OLD='github.com/chain4travel/camino-matrix-app-service'
NEW='github.com/TravelTokenMarketplace/travel-token-matrix-app-service'
grep -rl --include='*.go' -e "$OLD" . | grep -v '/vendor/' | xargs sed -i "s|$OLD|$NEW|g"
sed -i "s|$OLD|$NEW|g" go.mod scripts/build.sh
```

- [ ] **Step 3: Rename the built-binary paths in the Dockerfile**

The Dockerfile's only `camino` references are the working-dir and the binary name; after the module rename the binary is `travel-token-matrix-app-service`:

```bash
sed -i 's|camino-matrix-app-service|travel-token-matrix-app-service|g' Dockerfile
```

- [ ] **Step 4: Tidy modules**

```bash
export TMPDIR=/hgst/work/.ttm-scratch GOTMPDIR=/hgst/work/.ttm-scratch GOCACHE=/hgst/work/.ttm-scratch/gocache GOMODCACHE=/hgst/work/.ttm-scratch/gomod GOPRIVATE='github.com/TravelTokenMarketplace/*'
go mod tidy
```
Expected: exits 0; `go.mod` module line now reads `module github.com/TravelTokenMarketplace/travel-token-matrix-app-service`; `git diff go.sum` empty or trivial.

- [ ] **Step 5: Build + unit tests (green gate)**

Run: `scripts/build_test.sh`
Expected: build succeeds; `go test ... ./...` passes (the one test package `config` passes); exit 0.

- [ ] **Step 6: Lint (green gate)**

Run: `scripts/lint.sh`
Expected: `golangci-lint` clean; `camino-license` header check passes (headers unchanged this phase); exit 0.

- [ ] **Step 7: Confirm no stray old module path remains**

Run: `grep -rn 'chain4travel/camino-matrix-app-service' --include='*.go' --include='*.mod' --include='*.sh' . | grep -v '/vendor/'`
Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "Rebrand Phase 1: rename Go module path to travel-token-matrix-app-service"
```

---

### Task 2: Re-pin bot dependency + version-reporting dependency-path strings

Point the bot dependency at the rebranded module (bot `dev` @ `c49ae0e`), delete the pre-rebrand `replace`, move the two bot source-import paths, and update every place that matches protocol/contracts dependency **paths** for version reporting (`version.go`, `resolve_protocol_release.sh`, `constants.sh`). These transitive deps only exist under their new paths once the bot is re-pinned, so they change together. **Do NOT** rename the `CMP`→`TTM` identifiers yet (Task 3) — only the dependency-path strings.

**Files:**
- Modify: `go.mod` (bot `require` + delete bot `replace`)
- Modify: `internal/service/service.go` (bot `pkg/matrix` import)
- Modify: `internal/storage/sqlite/storage.go`, `internal/storage/sqlite/chunked_messages.go` (bot `pkg/database/sqlite` import)
- Modify: `internal/version/version.go` (three `debug.ReadBuildInfo` switch-case dep paths)
- Modify: `scripts/resolve_protocol_release.sh` (buf `owner`/`module` + usage-example path)
- Modify: `scripts/constants.sh` (two `resolve_protocol_release.sh` buf dep-path arguments)

**Interfaces:**
- Consumes: new module path from Task 1.
- Produces: dependency on rebranded bot providing `matrix.EventTypeSignedMessage = "m.room.ttm-signed-msg"`, `matrix.EventTypeMessageChunk = "m.room.ttm-msg-chunk"`, and `matrix.SignedMessageEventContent.SenderTTMAccountAddress`.

- [ ] **Step 1: Branch from the merged `dev`**

> The controller creates this branch from `dev` only after Task 1's PR is merged. Bot commit `c49ae0e` must already be on the new bot repo's `dev` (it is).

```bash
git checkout dev && git pull && git checkout -b rebrand/phase-2-repin-bot
export TMPDIR=/hgst/work/.ttm-scratch GOTMPDIR=/hgst/work/.ttm-scratch GOCACHE=/hgst/work/.ttm-scratch/gocache GOMODCACHE=/hgst/work/.ttm-scratch/gomod GOPRIVATE='github.com/TravelTokenMarketplace/*'
```

- [ ] **Step 2: Move the three bot source-import paths**

```bash
sed -i 's|github.com/chain4travel/camino-messenger-bot/v13|github.com/TravelTokenMarketplace/travel-token-messenger-bot/v13|g' \
  internal/service/service.go internal/storage/sqlite/storage.go internal/storage/sqlite/chunked_messages.go
```

- [ ] **Step 3: Remove the old bot `require` and the old `replace` from `go.mod`**

Delete these two lines from `go.mod` (the `require` line in the require block, and the bot `replace` line inside the `replace (...)` block). Leave the `gnark-crypto` and `quic-go` replaces untouched:

```
require github.com/chain4travel/camino-messenger-bot/v13 v13.0.0
github.com/chain4travel/camino-messenger-bot/v13 => github.com/TravelTokenMarketplace/camino-messenger-bot/v13 v13.1.0-rc.1.0.20260711115550-98f847f3a5e6
```

- [ ] **Step 4: Add the rebranded bot at `dev` @ `c49ae0e`**

```bash
go get github.com/TravelTokenMarketplace/travel-token-messenger-bot/v13@c49ae0e
go mod tidy
```
Expected: `go.mod` now has `require github.com/TravelTokenMarketplace/travel-token-messenger-bot/v13 v13.x.y-0.<timestamp>-c49ae0e...` (a pseudo-version resolving to `c49ae0e`); no bot `replace` remains; exit 0.

- [ ] **Step 5: Update the version-reporting dependency-path switch cases**

In `internal/version/version.go`, update the three `case` strings in the `debug.ReadBuildInfo` loop:

```bash
sed -i \
  -e 's|buf.build/gen/go/chain4travel/camino-messenger-protocol/protocolbuffers/go|buf.build/gen/go/ttm/messenger-protocol/protocolbuffers/go|g' \
  -e 's|buf.build/gen/go/chain4travel/camino-messenger-protocol/grpc/go|buf.build/gen/go/ttm/messenger-protocol/grpc/go|g' \
  -e 's|github.com/chain4travel/camino-messenger-contracts/go/contracts|github.com/TravelTokenMarketplace/travel-token-messenger-contracts/go/contracts|g' \
  internal/version/version.go
```

- [ ] **Step 6: Update the buf owner/module in `resolve_protocol_release.sh` and its dep-path args in `constants.sh`**

In `scripts/resolve_protocol_release.sh`: change the JSON `"owner": "chain4travel"` → `"owner": "ttm"`, `"module": "camino-messenger-protocol"` → `"module": "messenger-protocol"`, and the usage-example line `buf.build/gen/go/chain4travel/camino-messenger-protocol/grpc/go` → `buf.build/gen/go/ttm/messenger-protocol/grpc/go`.

In `scripts/constants.sh`, the two invocation arguments:

```bash
sed -i 's|buf.build/gen/go/chain4travel/camino-messenger-protocol/|buf.build/gen/go/ttm/messenger-protocol/|g' scripts/constants.sh
sed -i -e 's|"owner": "chain4travel"|"owner": "ttm"|' \
       -e 's|"module": "camino-messenger-protocol"|"module": "messenger-protocol"|' \
       -e 's|buf.build/gen/go/chain4travel/camino-messenger-protocol/|buf.build/gen/go/ttm/messenger-protocol/|g' \
       scripts/resolve_protocol_release.sh
```

- [ ] **Step 7: Build + unit tests (green gate)**

Run: `scripts/build_test.sh`
Expected: builds against the rebranded bot; `go test ./...` passes; exit 0.

- [ ] **Step 8: Verify the version strings resolve against the real build graph (not `Unspecified`)**

Run:
```bash
go list -deps -f '{{.ImportPath}}' ./... 2>/dev/null | grep -E 'buf.build/gen/go/ttm/messenger-protocol/(protocolbuffers|grpc)/go' | sort -u
```
Expected: prints both `buf.build/gen/go/ttm/messenger-protocol/protocolbuffers/go` and `.../grpc/go` — confirming the transitive dep paths now match the `version.go` switch cases. (If empty, the switch cases will silently report `Unspecified` — fix before proceeding.)

- [ ] **Step 9: Confirm no old bot/protocol/contracts path remains**

Run: `grep -rn 'chain4travel/camino-messenger-bot\|chain4travel/camino-messenger-protocol\|chain4travel/camino-messenger-contracts\|camino-messenger-bot/v13' --include='*.go' --include='*.mod' --include='*.sh' . | grep -v '/vendor/'`
Expected: no output.

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "Rebrand Phase 2: re-pin bot to travel-token-messenger-bot dev (c49ae0e), repoint protocol/contracts version paths"
```

---

### Task 3: Identifiers, infra (cosmetic), config, docs, prose

The final sweep: `CMP`→`TTM` version identifiers, cmd/config/registration names, CI/Docker cosmetics, copyright, dead-config removal, `DATA_PROTECTION.md` deletion, and the closing `camino` grep sweep.

**Files:**
- Modify: `internal/version/version.go` (`BufBuildPBCMPRelease`→`BufBuildPBTTMRelease`, `BufBuildGRPCCMPRelease`→`BufBuildGRPCTTMRelease`, the `camino-messenger-contracts` label string, copyright header)
- Modify: `scripts/build.sh` (the two `-X ...CMPRelease` flag names; prose messages/comments with bare `camino-matrix-app-service`)
- Modify: `cmd/app_service.go` (`(CMP %s)` log labels, `camino-messenger-contracts` label, command `Use`/`Short`/aliases and any Camino prose)
- Modify: `config/flags.go` (default config filename + db dir defaults)
- Modify: `config/test_config.yaml` (db path default)
- Rename+modify: `example/config/camino-matrix-app-service.yaml` → `example/config/travel-token-matrix-app-service.yaml` (db path)
- Rename+modify: `example/config/synapse/camino.yaml` → `example/config/synapse/ttm.yaml` (`id`, `sender_localpart`, `hs_token` comment)
- Modify: `.github/workflows/ci.yml` (`branches: [c4t, dev]` → `[dev]`; Docker tag)
- Modify: `.golangci.yml` (remove two dead `camino-matrix-go` exclusion entries)
- Modify: `scripts/lint.sh` (`CAMINO_APP_SERVICE_PATH` → `TTM_APP_SERVICE_PATH`; keep the `camino-license` install/check lines)
- Modify: `scripts/constants.sh` (`CAMINO_APP_SERVICE_COMMIT`/`_TAG` env-var names; the "camino-matrix-app-service git tag and sha" comment)
- Modify: `header.yaml` (copyright entity)
- Modify: all remaining `.go` files' copyright headers
- Modify: `README.md` (prose, config filenames, product name; keep `camino-conduit`)
- Delete: `DATA_PROTECTION.md`

**Interfaces:**
- Consumes: version-field names from `version.go` (renamed here in lockstep with `build.sh`).

- [ ] **Step 1: Branch from the merged `dev`**

```bash
git checkout dev && git pull && git checkout -b rebrand/phase-3-identifiers-prose
export TMPDIR=/hgst/work/.ttm-scratch GOTMPDIR=/hgst/work/.ttm-scratch GOCACHE=/hgst/work/.ttm-scratch/gocache GOMODCACHE=/hgst/work/.ttm-scratch/gomod GOPRIVATE='github.com/TravelTokenMarketplace/*'
```

- [ ] **Step 2: Rename the `CMP` version-release identifiers (version.go + build.sh in lockstep)**

```bash
sed -i -e 's|BufBuildPBCMPRelease|BufBuildPBTTMRelease|g' -e 's|BufBuildGRPCCMPRelease|BufBuildGRPCTTMRelease|g' \
  internal/version/version.go scripts/build.sh cmd/app_service.go
```
(Also handles the `cmd/app_service.go` references if any; verify with grep in Step 11.)

- [ ] **Step 3: Fix the `(CMP %s)` log labels and contracts label in `cmd/app_service.go` and `version.go`**

Edit `cmd/app_service.go`: `buf.build protocolbuffers version: %s (CMP %s)` → `(TTM %s)`; `buf.build grpc version: %s (CMP %s)` → `(TTM %s)`; `camino-messenger-contracts version: %s` → `travel-token-messenger-contracts version: %s`.
Edit `internal/version/version.go`: the `"camino-messenger-contracts"` label string in `FullVersion` → `"travel-token-messenger-contracts"` (adjust padding to keep column alignment).

- [ ] **Step 4: Command identity + prose in `cmd/app_service.go`**

Edit the cobra command `Use`/`Short`/`Aliases` and any Camino prose: `Use: "camino-matrix-app-service"` → `"travel-token-matrix-app-service"`; `Short: "...camino matrix app-service"` → Travel Token wording; alias `"camino-app-service"` → `"ttm-app-service"` (keep the short `"app-service"` alias).

- [ ] **Step 5: Config defaults + example configs**

```bash
sed -i 's|camino-matrix-app-service|travel-token-matrix-app-service|g' config/flags.go config/test_config.yaml
git mv example/config/camino-matrix-app-service.yaml example/config/travel-token-matrix-app-service.yaml
sed -i 's|camino-matrix-app-service|travel-token-matrix-app-service|g' example/config/travel-token-matrix-app-service.yaml
git mv example/config/synapse/camino.yaml example/config/synapse/ttm.yaml
```
Then edit `example/config/synapse/ttm.yaml`: `id: camino` → `id: ttm`, `sender_localpart: camino` → `sender_localpart: ttm`. Edit the `access_token` comment in the app-service example that mentions `camino_app_service_hs_token` → `ttm_app_service_hs_token`.

- [ ] **Step 6: CI + Docker cosmetics**

Edit `.github/workflows/ci.yml`: `branches: [c4t, dev]` → `branches: [dev]`; Docker tag `c4tplatform/camino-matrix-app-service:temp` → `travel-token-matrix-app-service:temp`.

- [ ] **Step 7: Remove the dead `camino-matrix-go` golangci exclusions**

In `.golangci.yml`, delete the two `- camino-matrix-go` lines (under `linters.exclusions.paths` and `formatters.exclusions.paths`). No such path exists in this repo.

- [ ] **Step 8: Script env-var names + copyright header template**

```bash
sed -i 's|CAMINO_APP_SERVICE_PATH|TTM_APP_SERVICE_PATH|g' scripts/lint.sh
sed -i -e 's|CAMINO_APP_SERVICE_COMMIT|TTM_APP_SERVICE_COMMIT|g' -e 's|CAMINO_APP_SERVICE_TAG|TTM_APP_SERVICE_TAG|g' scripts/constants.sh
sed -i 's|camino-matrix-app-service git tag and sha|travel-token-matrix-app-service git tag and sha|' scripts/constants.sh
sed -i 's|Chain4Travel AG|Travel Token Marketplace|g' header.yaml
```
(`scripts/lint.sh`'s `camino-license` install and check lines are the kept external tool — do not touch them.)

- [ ] **Step 9: Copyright headers in all `.go` files**

```bash
grep -rl 'Chain4Travel AG' --include='*.go' . | grep -v '/vendor/' | xargs sed -i 's|Chain4Travel AG|Travel Token Marketplace|g'
```

- [ ] **Step 10: README prose + delete DATA_PROTECTION.md**

Edit `README.md`: `Camino Messenger` → `Travel Token Messenger`; `camino-matrix-app-service` (product name, config filenames) → `travel-token-matrix-app-service`; config file references `camino-matrix-app-service.yaml` → `travel-token-matrix-app-service.yaml` and `synapse/camino.yaml` → `synapse/ttm.yaml`. **Leave `camino-conduit` references unchanged** (separate sibling repo).

```bash
git rm DATA_PROTECTION.md
```

- [ ] **Step 11: Build + tests + lint + shellcheck (green gate)**

```bash
scripts/build_test.sh
scripts/lint.sh
scripts/shellcheck.sh
go mod tidy && git diff --exit-code go.mod go.sum
```
Expected: all four exit 0; `camino-license` header check passes with the new copyright; `go mod tidy` leaves no diff.

- [ ] **Step 12: Final `camino` sweep**

Run: `grep -rin camino --exclude-dir={.git,vendor} . `
Expected: the ONLY matches are (a) the kept `camino-license` tool lines in `scripts/lint.sh`, (b) `camino-conduit` references in `README.md`, and (c) this dated design/plan doc under `docs/superpowers/`. Any other `camino` is a miss — fix it before committing.

- [ ] **Step 13: Commit**

```bash
git add -A
git commit -m "Rebrand Phase 3: identifiers, config, CI/Docker, copyright, prose; delete DATA_PROTECTION.md"
```

---

## Post-plan: whole-branch review + finish

After Task 3's PR is merged, run the whole-branch review (superpowers:requesting-code-review) over `dev` vs the pre-rebrand base, then superpowers:finishing-a-development-branch. Record any deferred loose-ends (fresh `DATA_PROTECTION.md`, live Matrix homeserver host) outside the repo.
