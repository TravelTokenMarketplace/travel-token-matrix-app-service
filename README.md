# Travel Token Matrix App Service

The **Travel Token Matrix App Service** is an extension component of the Travel Token Messenger network. It runs alongside the Matrix homeserver (`camino-conduit`) and observes message traffic: it checks that events are structurally well-formed and tracks multi-chunk messages, so that a bot which breaks the wire protocol can be identified.

> [!IMPORTANT]
> **This component provides no cryptographic assurance.** It does not verify signatures. See [Architectural Role & Function](#architectural-role--function) below for what it actually checks and where real verification happens.

---

## Architectural Role & Function

```
                  ┌────────────────────────┐
                  │   Matrix Homeserver    │
                  │   (camino-conduit)     │
                  └───────────┬────────────┘
                              │
               Relays ALL events (namespace: .*)
                              │
                              ▼
           ┌──────────────────────────────────┐
           │ Travel Token Matrix App Service  │
           │                                  │
           │  1. Checks events are well-formed│
           │  2. Tracks multi-chunk messages  │
           │  3. Flags protocol violations    │
           └──────────────────┬───────────────┘
                              │
                      If check fails
                              │
                              ▼
           ┌──────────────────────────────────┐
           │        Ban/Mute Action           │
           │   (Currently Not Enforced/Stub)   │
           └──────────────────────────────────┘
```

1. **Event Capture**:
   - The App Service is registered automatically by `camino-conduit` at startup with access to the global namespace (`.*`).
   - It captures `matrix.EventTypeSignedMessage` and `matrix.EventTypeMessageChunk` events from all rooms.
2. **Structural verification (not signature verification)**:
   - For every captured event, the App Service calls `eventContent.Verify()`.
   - `Verify()` checks **well-formedness only**: that the declared chunk count is non-zero, that a chunk index is non-zero, and that the message id and data are non-empty. It **does not read the `Signature` field and performs no `ecrecover`**.
   - Signatures are verified by the bots at the ends of the conversation, which have the sender's account and the key material to do it. This component deliberately does not duplicate that: it sees the same bytes but adds no independent trust, and duplicating the crypto here would suggest an assurance that is not being provided.
3. **Chunk tracking**:
   - Large payloads (such as extensive search results) are split across a signed message plus a number of `MessageChunk` events. The App Service records **which chunk indices** have arrived for each message, so it can tell when a message is complete and when a sender sent an index it never declared.
   - Indices are tracked as a set rather than counted, because Matrix redelivers events: counting arrivals cannot distinguish a redelivered chunk from a new one. Each index is recorded with the account that sent it, so an index that only later turns out to be out of range is still attributed to whoever sent it.
   - Incomplete messages are swept after a fixed time-to-live, so a peer that starts messages it never finishes cannot grow the tracking table without bound.

---

## Current Enforcement Status

> [!NOTE]
> **No cryptographic assurance.** This component does not verify signatures and holds no key material. Nothing it reports should be read as evidence that a message was genuinely sent by the account it names.

> [!NOTE]
> **Not enforced yet.** A sender that breaks the protocol is flagged, but the banning action itself is a no-op stub, so violations produce log entries and nothing more.

A sender is flagged for exactly two things, both of which only the sender can cause:

- event content that fails structural verification (zero chunk count, zero chunk index, empty message id or data);
- a chunk index at or beyond the chunk count the message declared.

The second is checked without regard to arrival order. A chunk can overtake the signed message that declares the count, and until that count is known there is nothing to judge the index against, so recorded indices are re-examined once it arrives. Checking only the index in hand would have meant an out-of-range chunk went unflagged whenever it arrived first — the order a sender doing it deliberately would choose.

What is flagged is the account that **sent** the offending chunk, which is not necessarily the sender of the event that exposed it. A message id is chosen by its sender and nothing binds one to a single account, so each chunk records who sent it; otherwise one peer could get another flagged by planting a stray index under its message id.

Deliberately **not** flagged:

- **Redelivered chunks.** A chunk arriving twice is the network's doing. It is recorded once and otherwise ignored.
- **Content that fails to parse.** The Matrix event class is not transported and has to be reconstructed on this side, so a parse failure is at least as likely to be a fault here as at the sender. Such an event is dropped with a warning and nobody is blamed.

Failures of *this component* — storage errors, for instance — are reported to the homeserver as a failed transaction so that it redelivers. A misbehaving peer is never answered that way: a homeserver retries a failed transaction indefinitely and holds the appservice's event stream while it does, so failing the transaction over one peer's bad event would stall every other sender behind it.

---

## Configuration & Deployment

### Docker Build
Build the Docker image with:
```bash
docker build -t travel-token-matrix-app-service .
```

### Registration configuration
- **Conduit Integration**: When using `camino-conduit`, registration is automated at startup using settings in `conduit.toml` (`ttm_app_service_url`, `ttm_app_service_as_token`, `ttm_app_service_hs_token`).
- **Synapse Integration**: If running with a standard Synapse server, register by placing the appservice registration yaml file at `files/matrix/.synapse/ttm.yaml` (see [example/config/synapse/ttm.yaml](example/config/synapse/ttm.yaml)).

### App-Service configuration
The app-service expects its configuration yaml at `/travel-token-matrix-app-service/travel-token-matrix-app-service.yaml`.
Refer to [example/config/travel-token-matrix-app-service.yaml](example/config/travel-token-matrix-app-service.yaml) for a configuration template.
