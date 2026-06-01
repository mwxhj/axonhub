# Backend Routing Guidelines

> Routing, load-balancing, sticky-session, credential, quota, retry, and circuit-breaker contracts for backend development.

---

## Sticky Session Routing Contract

### 1. Scope / Trigger

Read this section before changing any of:

- Channel candidate selection, priority, weight, retry, fallback, or load-balancing strategy.
- Sticky-session key extraction, binding store, TTL, or target migration.
- Credential-aware routing, provider quota accounting, or channel/key dedupe.
- Model circuit-breaker middleware or any raw-request middleware that can skip a candidate.

Sticky-session exists to preserve upstream cache locality. It is not a health-management system and must not replace the existing retry/fallback path.

### 2. Signatures

Sticky-session state is internal server state. Clients must not provide a sticky key.

```go
type StickySessionTarget struct {
    ChannelID             int
    CredentialID          *int
    CredentialFingerprint string
}

type StickySessionBinding struct {
    StickyKey string
    Target    StickySessionTarget
    ExpiresAt time.Time
}
```

Required runtime meaning:

- `stickyKey -> target`, not `request -> target`.
- `target` means the successful execution target. When credential identity is known, it includes both channel and upstream credential identity.
- TTL is 5 minutes. Expiry intentionally lets normal priority, weight, and availability rules regain control.
- Bindings are soft. They may be ignored when the target is not eligible for the current candidate tier.

### 3. Contracts

- Candidate channels still come from the existing profile, API key, model, priority, quota, and health eligibility rules.
- Sticky-session must not cross priority tiers to preserve stickiness. It can only reorder eligible candidates inside the current retry/fallback tier.
- First unbound selection uses the existing load-balancer order for that tier, including priority and weight semantics. Do not add sticky-specific stable channel scoring or rendezvous hashing.
- A binding is created or refreshed only after an upstream request succeeds.
- A selected target must not be written to the binding store before upstream success.
- If the bound target fails and fallback succeeds, refresh the binding to the successful fallback target.
- If all attempts fail, keep the previous binding until TTL expiry and return the real retry/upstream error.
- Sticky-session may prefer the same credential across eligible same-priority channels, but it must not move to a lower-priority channel only to keep the credential.
- If retry/fallback has already entered a lower-priority tier and that tier succeeds, binding may refresh to that successful target. The 5-minute TTL is what prevents permanent priority bypass.

### 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| No qualified sticky key can be generated | Use normal load balancing. Do not create a binding. |
| Binding exists and target is eligible in the current tier | Place that target first for the current attempt. |
| Binding target is disabled, deleted, model-ineligible, quota-ineligible, or outside the current priority tier | Ignore the binding for this attempt. Normal routing continues. |
| Bound target returns network error, timeout, 5xx, empty response, retryable 429, or queue-full error | Let existing retry/fallback escape. Do not migrate on failed execution alone. |
| Fallback target succeeds | Refresh binding to the successful target. |
| All targets fail | Keep old binding until TTL. Return the real upstream/retry result. |
| Circuit breaker is open | Circuit-breaker strategy may skip candidates. Sticky-session strategy must not expose a raw-request local skip as HTTP 500. |
| No executable candidates remain after eligibility filtering | Return an explicit no-candidate/upstream-unavailable error, not a raw middleware wrapping error. |

### 5. Good / Base / Bad Cases

- Good: no binding exists, normal priority/weight routing selects channel A, upstream succeeds, binding becomes `stickyKey -> A`.
- Good: binding points to A, A fails once, retry/fallback reaches B and B succeeds, binding refreshes to B.
- Good: binding points to C from a lower-priority fallback, TTL expires after 5 minutes, the next request returns to normal priority/weight selection.
- Base: no qualified sticky key exists, request behaves exactly like the configured non-sticky load-balancer.
- Bad: binding to A before A succeeds.
- Bad: using `hash(stickyKey + channelID)` or rendezvous hashing to override the existing priority/weight selection inside sticky-session.
- Bad: crossing from high-priority A/B to low-priority C only because C has the old binding.
- Bad: returning `failed to apply raw request middlewares: skip candidate by circuit breaker` as the client-visible 500 for sticky-session routing.

### 6. Tests Required

When changing sticky-session or neighboring routing behavior, add or update tests for:

- No binding write before upstream success.
- Successful fallback refreshes binding to the successful target.
- Failed fallback does not migrate or delete the old binding.
- Sticky-session does not cross priority tiers before retry/fallback reaches that tier.
- Sticky-session does not enable model circuit-breaker raw-request skip behavior.
- No executable candidate returns an explicit routing/unavailable error instead of a raw middleware wrapper.
- Credential-aware sticky routing keeps the same credential only among eligible same-tier candidates.

### 7. Wrong vs Correct

#### Wrong

```text
candidate list -> sticky hash score -> pick channel -> write binding -> send upstream
```

This binds on selection instead of success and lets sticky-session introduce its own distribution logic.

#### Correct

```text
candidate list
-> existing priority/weight/load-balancer tier selection
-> sticky binding reorders eligible same-tier candidates only
-> send upstream through existing retry/fallback
-> on success, bind stickyKey to the successful target for 5 minutes
```

This keeps sticky-session focused on cache locality while priority, quota, health, retry, and fallback remain owned by the normal routing pipeline.

---

## Responses API Chain Routing

Responses API requests with `previous_response_id` are stronger than ordinary sticky-session hints. The upstream response ID may be provider-local state.

Required behavior:

- Track response chains internally: if `resp_2` was created from `previous_response_id=resp_1`, then `resp_2` inherits the successful target used for that chain.
- Prefer the chain target before generic request fingerprinting.
- If the chain target fails and fallback succeeds by reconstructing a complete request, bind the new response ID to the successful target.
- If the request still depends on provider-local previous response state that cannot be reconstructed, do not blindly cross providers or credentials.

Tests must distinguish:

- Chain target success.
- Chain target fail plus reconstructed fallback success.
- Chain target fail with unreconstructable provider-local state.

---

## Sticky Key Extraction

Sticky keys are generated from stable server-visible context. They are not client-supplied and do not have a confidence score.

Extractor shape:

```go
type StickyKeyResult struct {
    Key    string
    OK     bool
    Reason string
}
```

Rules:

- `OK=false` means normal load balancing. Do not force stickiness from weak material.
- Prefer explicit stable session identity from Codex, Claude Code, agent, project, window, or similar internal context when available.
- For Responses API, prefer response-chain identity over prompt fingerprinting.
- For chat/completions without a session ID, build a root-session fingerprint from API key/profile scope, model scope, client format, system/developer prompt, tool schema, and early stable user context.
- Do not hash the latest user message or the entire conversation history. That drifts every turn and destroys cache locality.
- Do not expose `stickyKey` in API responses, logs at unsafe verbosity, or client-visible errors.

---

## Credential / Quota Execution Observability

### 1. Scope / Trigger

Read this section before changing:

- Channel credential resolution, legacy credential adapters, or upstream API-key providers.
- `RequestExecution`, `UsageLog`, provider quota status, credential quota scope, or request log UI fields.
- Any route that can retry/fallback across multiple channels or credentials.

The execution target is `channel + credential + resource scope + model`. Request records must show the safe credential identity used by each attempt, while quota status must be tracked at credential/resource/quota-scope granularity instead of only at channel granularity.

### 2. Signatures

Required request execution / usage log snapshot fields:

```text
credential_id
credential_fingerprint
secret_fingerprint
resource_scope_key
quota_scope_id
quota_scope_name_snapshot
quota_scope_status_snapshot
credential_name_snapshot
credential_key_hint
credential_source
credential_quota_status_snapshot
```

Required provider quota target metadata:

```text
scope_key
channel_id
credential_id
credential_fingerprint
secret_fingerprint
resource_scope_key
quota_scope_id
status
ready
quota_data
```

### 3. Contracts

- `credential_key_hint`, fingerprints, and resource scope are safe operator identifiers. Raw upstream secrets must never be stored in request records, GraphQL responses, logs, tooltips, or exports.
- Runtime credential selection must write context values before `CreateRequestExecution`, so every retry attempt records the credential actually used by that attempt.
- Request list UI may show the latest execution credential, but request detail must show the credential for every execution attempt.
- Provider quota checks for multiple API keys must update credential-level cache/status for each checked key and store per-key summaries in aggregate quota data.
- Provider quota row identity is `provider_type + scope_key`; `channel_id` is last-observed metadata. Startup migrations must backfill legacy `scope_key="channel"` rows with a channel ID to `channel:<id>` before loading provider quota cache.
- Provider quota cache loads must be deterministic when old duplicate rows exist: read rows in ascending `updated_at`/ID order so the newest provider/scope observation overwrites older cache entries.
- Provider quota status should update `UpstreamCredential.quota_status` and, when a quota scope is known, `CredentialQuotaScope.status/source/last_error/reset_at`.
- `CredentialQuotaScope` is the shared budget pool. Multiple credentials may point to the same `quota_scope_id`; quota accounting and routing availability must respect that shared status.
- Candidate quota filtering must narrow the executable credential views before outbound selection. A channel must not remain eligible because one key is available while the API-key provider can still select another exhausted key.
- When outbound transformers hold API-key providers from the original channel snapshot, routing must pass a candidate-scoped credential allow-list through context so the provider can only choose credentials kept by the current candidate/quota decision.
- In de-prioritize mode, channel ordering may keep exhausted channels in the candidate set, but if a channel has both exhausted and available credentials, the provider should still avoid the exhausted credential when an available credential exists.

### 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| Credential selected from first-class ref | Persist credential ID, safe name, key hint, source `ref`, fingerprint, secret fingerprint, resource scope, and quota scope snapshots. |
| Credential selected from legacy inline channel key | Persist safe key hint/fingerprint/resource scope and source `legacy`; never persist the raw key. |
| Request retries across credentials | Create one `RequestExecution` per attempt and snapshot that attempt's credential, not only the final channel. |
| Credential/quota scope is renamed or archived later | Old request records remain readable from snapshots. |
| Provider quota check fails for one key | Mark that credential observation unknown/unready, retain safe error, and do not overwrite unrelated credential status. |
| Shared quota scope is exhausted/paused/disabled | Only credentials/resources attached to that scope become unavailable; unrelated credentials remain eligible. |

### 5. Good / Base / Bad Cases

- Good: channel A uses credential K1, retry falls back to channel B using K2, and request detail shows K1 on attempt 1 and K2 on attempt 2.
- Good: a provider checker sees K1 exhausted and K2 available on the same channel; K1 is skipped while K2 remains eligible.
- Base: no selected credential is known; request execution fields stay empty and normal channel observability still works.
- Bad: request list shows a channel API key raw value or any unmasked bearer token.
- Bad: provider quota writes only `channel_id -> exhausted`, disabling every credential on that channel even when only one upstream key is exhausted.

### 6. Tests Required

When changing credential/quota observability, add or update tests for:

- `CreateRequestExecution` stores credential/resource/quota snapshots from context.
- `UsageLog` stores the same safe credential/resource/quota identity and increments the selected quota scope.
- Provider quota status updates credential/quota scope observations and cache entries for the selected target.
- Provider quota startup migration rewrites legacy `scope_key="channel"` rows with channel IDs to `channel:<id>` before cache load.
- Provider quota cache load keeps the latest row when duplicate provider/scope rows exist.
- Provider quota aggregate rows clear stale credential target metadata when the row returns to channel-level aggregate status.
- Same-channel multi-key routing where K1 is exhausted and K2 is available keeps the channel eligible but constrains the API-key provider to K2.
- Same-channel all-key-exhausted routing filters the channel in exhausted-only mode and reports quota exhaustion when no executable candidates remain.
- Frontend request list/detail queries include the snapshot fields used by the UI.

### 7. Wrong vs Correct

#### Wrong

```text
provider quota check -> update channel status only -> request UI shows channel/api key
```

#### Correct

```text
credential resolver -> context safe credential target
-> request execution / usage log snapshot
-> provider quota updates credential/resource/quota scope
-> request UI displays credential name + key hint + source + resource/quota scope
```

---

## Credential Local Quota And Archive Product Contract

### 1. Scope / Trigger

Read this section before changing:

- `CreateCredentialQuotaScopeInput` or `UpdateCredentialQuotaScopeInput` handling.
- GraphQL credential archive/delete mutations.
- Credentials UI fields that display quota state or routing availability.

This contract keeps three meanings separate: local quota scope, provider quota status, and derived routing availability.

### 2. Signatures

Backend service/API signatures:

```go
func (svc *UpstreamCredentialService) ArchiveUpstreamCredential(ctx context.Context, id int) (*ent.UpstreamCredential, error)
func (svc *UpstreamCredentialService) DeleteUpstreamCredential(ctx context.Context, id int) (bool, error)
func normalizeCreateCredentialQuotaScopeInput(input CreateCredentialQuotaScopeInput, now time.Time) (CreateCredentialQuotaScopeInput, error)
func normalizeUpdateCredentialQuotaScopeInput(scope *ent.CredentialQuotaScope, input UpdateCredentialQuotaScopeInput, now time.Time) (UpdateCredentialQuotaScopeInput, error)
```

GraphQL product mutation:

```graphql
archiveUpstreamCredential(id: ID!): UpstreamCredential!
deleteUpstreamCredential(id: ID!): Boolean!
```

### 3. Contracts

- Local quota is `CredentialQuotaScope`: `status`, `unit`, `limit_amount`, `used_amount`, `reset_policy`, `reset_at`, `window_started_at`, `over_limit_action`, `pause_until`, `source`.
- Provider quota is `ProviderQuotaStatus`: `provider_type`, `status`, `ready`, `next_reset_at`, `next_check_at`, `scope_key`, resource/credential/quota-scope identifiers.
- UI must not collapse local quota status and provider quota status into one unlabeled badge. Show local quota, provider quota, and routing availability separately.
- `reset_policy=daily` defaults missing `reset_at` to the next local midnight stored as UTC and defaults missing `window_started_at` to current UTC time.
- `reset_policy=monthly` defaults missing `reset_at` to the first day of the next local month at midnight stored as UTC and defaults missing `window_started_at` to current UTC time.
- `reset_policy=custom` requires `window_started_at` and `reset_at`, with `reset_at > window_started_at`.
- Archive is a reversible product action: set `UpstreamCredential.status=archived`, preserve `ChannelCredentialRef` rows, reload channel routing state, and keep history/safe metadata readable. Runtime already excludes archived credentials because credential views require `ref.enabled && credential.status=enabled`.
- Re-enabling an archived credential must restore routing availability. To recover rows archived by older code, the archived-to-enabled transition may restore refs for that credential.
- Creating a credential with a secret that matches an archived credential should reactivate/update the archived credential instead of returning a still-archived row unchanged.
- Delete is the irreversible product action for credential management: remove channel refs, soft-delete the credential, reload channel routing state, and allow the same secret to be added again.
- Archive does not wipe `secret_payload` unless a future explicit wipe action is added.

### 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| Create daily/monthly quota without `reset_at` | Fill predictable next local reset time and save UTC. |
| Update scope from `none`/`manual` to daily/monthly with empty effective `reset_at` | Fill predictable next local reset time. |
| Custom quota missing `window_started_at` or `reset_at` | Return a specific validation error. |
| Custom quota with `reset_at <= window_started_at` | Return a specific validation error. |
| Archive credential with enabled channel refs | Preserve refs, return archived credential, and rely on credential status to remove it from runtime routing. |
| Re-enable archived credential | Set status enabled and recover refs when needed so routing availability returns. |
| Create credential with same secret as archived credential | Reactivate/update the archived credential and return it enabled by default. |
| Delete credential with channel refs | Delete refs, soft-delete credential, and allow same secret recreation. |
| Archive credential with request/usage history | Preserve history and safe snapshots; do not hard delete rows. |

### 5. Good / Base / Bad Cases

- Good: daily local quota created with no reset time returns a concrete next local reset.
- Good: credential detail shows local quota used/limit/remaining separately from provider-observed status.
- Good: archive action tells the operator routing stops, refs are preserved for restore, and history remains visible.
- Good: delete action tells the operator refs are removed and the same secret can be added again.
- Base: credential has no local quota and no provider observation; UI says no local quota and no provider observation, routing derives from status/refs.
- Bad: frontend displays `quotaScope.status || credential.quotaStatus` as one generic quota badge.
- Bad: archive is only reachable by a generic status dropdown with no side-effect confirmation.

### 6. Tests Required

When changing this contract, add or update tests for:

- Daily and monthly reset defaulting.
- Custom reset validation for missing and invalid windows.
- GraphQL create/update mutations using frontend-like quota fields.
- `archiveUpstreamCredential` preserving refs and returning an archived credential.
- Runtime credential views excluding archived credentials even when refs remain enabled.
- Archived same-secret create reactivating the credential.
- `deleteUpstreamCredential` removing refs, soft-deleting credential, and allowing same-secret recreation.
- Frontend type checks for credential quota/provider quota fields.

### 7. Wrong vs Correct

#### Wrong

```text
quota badge = quotaScope.status || credential.quotaStatus || "unknown"
delete action = set status archived from generic status dialog
```

#### Correct

```text
local quota section = CredentialQuotaScope fields
provider quota section = ProviderQuotaStatus rows
routing availability = derived from credential status + refs + local quota + provider quota
archive action = explicit confirmation -> archive mutation -> refs preserved for restore
delete action = explicit confirmation -> delete mutation -> refs removed + credential soft-deleted
```
