# Backend Routing Guidelines

> Routing, load-balancing, sticky-session, credential, quota, retry, and circuit-breaker contracts for backend development.

---

## Sticky Session Routing Contract

### 1. Scope / Trigger

Read this section before changing any of:

- Channel candidate selection, priority, weight, retry, fallback, or load-balancing strategy.
- Sticky-session key extraction, binding store, TTL, or target migration.
- Credential-aware routing, route observability snapshots, or channel/key dedupe.
- Model circuit-breaker middleware or any raw-request middleware that can skip a candidate.

For channel credential ownership, OAuth migration, channel-local quota removal, and credential/key-local quota behavior, read [Credential Routing Model](./credential-routing-model.md) first.

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
- First unbound selection starts from the existing load-balancer order for that tier, including priority and weight semantics. Do not add sticky-specific stable channel scoring or rendezvous hashing.
- First unbound selection may apply local key-quota ratio balancing only after the normal sticky first choice lands on a primary channel that still has at least one comparable local `CredentialQuotaScope` credential view. The balancing pool is limited to eligible credential views in the same priority tier, and chooses the lowest `used_amount / limit_amount` ratio for the current local daily window.
- A binding is created or refreshed only after an upstream request succeeds.
- A selected target must not be written to the binding store before upstream success.
- If the bound target fails and fallback succeeds, refresh the binding to the successful fallback target.
- If all attempts fail, keep the previous binding until TTL expiry and return the real retry/upstream error.
- Sticky-session may prefer the same credential across eligible same-priority channels, but it must not move to a lower-priority channel only to keep the credential.
- If retry/fallback has already entered a lower-priority tier and that tier succeeds, binding may refresh to that successful target. The 5-minute TTL is what prevents permanent priority bypass.
- Local key-quota ratio balancing must not treat credentials without comparable local quota data as `0%` or `100%`. If the primary channel has no comparable local quota credential views, keep normal load-balancer behavior. If the primary channel is in the local quota-managed pool, compare only local budget `CredentialQuotaScope` rows with valid positive limits, valid used amounts, daily reset policy, and a future reset time.
- Credential executability and local quota comparability are different checks. A credential view may remain executable while being excluded from local quota-ratio balancing because its local quota window is stale, auto-reset-due, provider-owned, non-daily, or otherwise non-comparable. That must not disqualify other comparable credentials on the same primary channel from quota-ratio first-bind balancing.
- Sticky-session semantic states must stay explicit: binding hit, documented rebind policy, documented degrade path. Do not silently change from quota-aware rebind semantics to ordinary load balancing without a named contract and test coverage.
- Internal credential executability summaries may affect candidate or credential eligibility, but they must not be used as ratio input for sticky first-bind balancing.
- When multiple credentials share a `quota_scope_id`, compare the shared scope once. After selecting that scope, choose the concrete credential using the existing credential selection order/seed behavior.

### 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| No qualified sticky key can be generated | Use normal load balancing. Do not create a binding. |
| Binding exists and target is eligible in the current tier | Place that target first for the current attempt. |
| Binding target is disabled, deleted, model-ineligible, quota-ineligible, or outside the current priority tier | Ignore the binding for this attempt. Normal routing continues. |
| New sticky session primary channel has no comparable local key quota views | Keep normal load-balancer behavior. Do not force it into the quota-managed pool. |
| New sticky session primary channel has comparable local daily key quota views | Reorder only comparable local quota-managed credential views in the same priority tier by lowest `used_amount / limit_amount`. |
| Seeded or preferred credential on the primary channel is stale/non-comparable, but sibling credentials on that same primary channel still have comparable local daily quota views | Still enter quota-ratio first-bind balancing using the comparable sibling views. Do not silently degrade to ordinary load balancing just because the seeded credential itself is stale. |
| Same priority tier mixes local-quota and no-quota credentials | Balance only among comparable local-quota credentials after entering that pool; leave no-quota selections to normal load balancing. |
| Comparable local quota data is missing, invalid, zero-limit, stale, provider-owned, or non-daily | Exclude that credential view from ratio balancing, while preserving existing eligibility behavior. |
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
- New sticky first-bind quota-ratio ordering chooses the lowest local daily `CredentialQuotaScope` ratio only inside the current priority tier.
- Expired or stale seeded credential quota windows on the primary channel do not block quota-ratio first-bind if sibling credentials on that same channel still have comparable local daily quota data.
- Mixed local-quota and no-quota sticky first-bind behavior preserves normal no-quota selection unless the normal first choice is already in the comparable local quota-managed pool.
- Shared `quota_scope_id` credentials are compared as one local quota pool before choosing the concrete credential.
- Missing, invalid, provider-owned, stale, non-daily, or zero-limit local quota data does not participate in sticky quota-ratio balancing.

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

## Retry / Fallback Guardrails

### 1. Scope / Trigger

Read this section before changing:

- Retry classification, same-channel retry, cross-channel fallback, or cross-priority fallback.
- Sticky-session escape behavior after a target fails.
- Credential-aware fallback, excluded-target tracking, or retry attempt budgeting.
- Any middleware or selector that can hide an upstream failure by silently switching targets.

This section defines project-level constraints, not only one implementation. The goal is to preserve user control and keep failures observable.

### 2. Contracts

- Retry/fallback must not hide real routing or upstream problems behind aggressive silent recovery.
- Retry/fallback must not decide for the user that a request has taken "too long" before the user cancels it or the configured request timeout expires.
- The system may bound retry/fallback by structural scope such as target count, credential count, priority drop count, or total attempts. It must not add a separate hidden time-budget cutoff just for fallback behavior.
- Recovery should depend primarily on fallback, not on repeated same-target retry. Same-target retry should stay rare and narrowly justified.
- Same-target retry is less valuable once sticky-session and credential-aware fallback exist. In sticky routing, repeatedly hitting the same failed target usually harms escape behavior more than it helps cache locality.
- Fallback must stay local before it becomes global:
  - same target retry first when the error is plausibly transient,
  - then same-channel credential fallback,
  - then same-priority target fallback,
  - then lower-priority fallback only after the current priority tier is exhausted.
- Same-channel credential fallback requires the channel transformer/auth layer to
  select credentials at request time and honor request-scoped credential
  exclusions. API-key providers that read context can do this. OAuth/token
  providers that bind the credential during channel construction must first be
  migrated to a request-scoped provider before they can support automatic
  same-channel credential fallback.
- Sticky-session is a preference, not a promise. A failed sticky target must be allowed to escape through normal retry/fallback.
- Successful fallback does not erase the original failure. The failed target, failed attempt count, and final successful target must remain observable in logs, request executions, and metrics.
- Retry/fallback must not silently change user-visible semantics after output has started. Once a response has begun streaming user-visible tokens, silent fallback to a different upstream target is forbidden.
- Fallback must only occur when switching targets has a plausible chance to succeed. Clearly non-retryable request/model/configuration errors should be surfaced, not spread across more targets.
- Priority is a service contract. Normal routing stays inside the best eligible priority tier. Lower-priority fallback is a deliberate degradation step, not a normal balancing path.

### 3. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| User has not canceled and request timeout has not expired | Retry/fallback may continue within structural attempt limits. Do not stop early because a fallback-specific time budget was reached. |
| User cancels the request | Stop retry/fallback and surface cancellation. |
| Same target fails with plausibly transient error | Same-target retry may occur if the retry policy and error classification both allow it, but keep it minimal. Prefer later fallback over repeated same-target retries. |
| Current credential fails with credential-scoped error and same channel has another eligible credential | Prefer same-channel credential fallback before switching channels. |
| Current credential fails but the channel's auth layer cannot honor request-scoped credential exclusions | Do not pretend same-channel credential fallback happened. Use normal cross-channel fallback or surface the failure when no fallback target remains. |
| Current priority tier still has untried eligible targets | Do not cross to a lower priority tier yet. |
| Current priority tier is exhausted and lower-priority fallback is allowed | Lower-priority fallback may begin. |
| Request/model/configuration error is clearly non-retryable | Surface the error. Do not keep hopping targets just to improve apparent success rate. |
| Sticky target fails and same-target retry is not clearly justified | Escape to fallback rather than repeatedly retrying the sticky target. |
| Sticky target fails but fallback succeeds elsewhere | Return success, refresh sticky binding to the successful target, and keep the original failure observable. |
| Response has already emitted user-visible tokens | Do not perform silent fallback to a different target. |

### 4. Good / Base / Bad Cases

- Good: sticky target fails with a credential-scoped error, another credential on the same channel succeeds, and the request execution history shows both attempts.
- Good: a sticky target gets at most one transient same-target retry, then quickly escapes to credential-aware fallback when the target still fails.
- Good: all targets in the current priority tier fail, fallback drops to the next priority tier, and a later success refreshes sticky binding while leaving the earlier failures visible.
- Base: a long-running request continues retry/fallback within normal request lifetime because the user has not canceled it.
- Bad: fallback stops after an internal 4-second budget even though the user still wants the request to continue.
- Bad: the router keeps retrying the same sticky target several times even though other eligible credentials or channels are available.
- Bad: a failing sticky target silently causes many hidden retries and then only the final success is visible to operators.
- Bad: the router jumps to a lower priority tier before exhausting the current one.
- Bad: a stream has already started, fallback switches upstreams, and the user receives mixed output from different targets.

### 5. Tests Required

When changing retry/fallback behavior, add or update tests for:

- No fallback-specific hidden time budget cuts off an otherwise valid request.
- Same-target retry remains minimal and does not dominate recovery when fallback targets are available.
- Same-channel credential fallback is attempted before cross-channel fallback when classification says the failure is credential-scoped.
- Credential fallback tests must cover both request-scoped API-key providers and construction-time auth providers, or explicitly document that construction-time auth providers are not covered by same-channel fallback yet.
- Lower-priority fallback does not begin until the current priority tier is exhausted.
- Successful fallback preserves observability of the failed attempts instead of only recording the final success.
- Silent fallback is blocked after user-visible streaming output has started.
- Non-retryable request/model/configuration errors surface directly without extra target hopping.

### 6. Wrong vs Correct

#### Wrong

```text
request fails
-> fallback timer budget reached at 4s
-> stop retrying early
-> return timeout-like failure even though user did not cancel
```

This lets internal fallback heuristics override user intent and makes long-request failures harder to reason about.

#### Correct

```text
request fails
-> classify failure
-> retry locally if plausible
-> same-channel credential fallback if applicable
-> same-priority fallback
-> lower-priority fallback only after exhaustion
-> continue until success, user cancel, configured timeout, or structural attempt limits
```

This keeps fallback conservative, observable, and subordinate to user intent.

---

## Credential Execution Observability

### 1. Scope / Trigger

Read this section before changing:

- Channel credential resolution, legacy credential adapters, or upstream API-key providers.
- `RequestExecution`, `UsageLog`, legacy quota-scope snapshot fields, or request log UI fields.
- Any route that can retry/fallback across multiple channels or credentials.

The execution target is `channel + credential + resource scope + model`. Request records must show the safe credential identity used by each attempt. `CredentialQuotaScope` is credential/key-local quota, not channel quota.

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

`quota_scope_*` snapshot fields are safe key-local quota metadata for the credential used by that attempt. They must not be interpreted as channel quota or provider quota truth.

### 3. Contracts

- `credential_key_hint`, fingerprints, and resource scope are internal correlation fields. Raw upstream secrets must never be stored in request records, GraphQL responses, logs, tooltips, or exports.
- Runtime credential selection must write context values before `CreateRequestExecution`, so every retry attempt records the credential actually used by that attempt.
- Request list UI may show the latest execution credential, but request detail must show the credential for every execution attempt.
- `CredentialQuotaScope` is credential/key-local quota. Runtime routing may use it to filter the affected credential view, but must not mark the entire channel unavailable while another credential view remains eligible.
- Candidate quota filtering must narrow the executable credential views before outbound selection. A channel must not remain eligible because one key is available while the API-key provider can still select another exhausted or locally blocked key.
- When outbound transformers hold API-key providers from the original channel snapshot, routing must pass a candidate-scoped credential allow-list through context so the provider can only choose credentials kept by the current candidate/quota decision.
- In de-prioritize mode, channel ordering may keep exhausted channels in the candidate set, but if a channel has both exhausted and available credentials, the provider should still avoid the exhausted credential when an available credential exists.

### 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| Credential selected from first-class ref | Persist credential ID, safe name, key hint, source `ref`, fingerprint, secret fingerprint, resource scope, and quota scope snapshots. |
| Credential selected from legacy inline channel key | Persist safe key hint/fingerprint/resource scope and source `legacy`; never persist the raw key. |
| Request retries across credentials | Create one `RequestExecution` per attempt and snapshot that attempt's credential, not only the final channel. |
| Credential is renamed, archived, or deleted later | Old request records remain readable from snapshots. |
| Local quota scope is exhausted with action `warn` | Keep that credential selectable and snapshot the local quota state. |
| Local quota scope is exhausted with action `pause` or `disable` | Remove only that credential view from executable candidates. Keep the channel eligible when another credential view remains. |
| Local quota scope is paused or disabled | Remove only that credential view from executable candidates unless the pause has expired or an automatic reset is due. |

### 5. Good / Base / Bad Cases

- Good: channel A uses credential K1, retry falls back to channel B using K2, and request detail shows K1 on attempt 1 and K2 on attempt 2.
- Good: a local quota evaluation sees K1 paused and K2 available on the same channel; K1 is skipped while K2 remains eligible.
- Base: no selected credential is known; request execution fields stay empty and normal channel observability still works.
- Bad: request list shows a channel API key raw value or any unmasked bearer token.
- Bad: request UI falls back to key hints, fingerprints, or credential source strings as default operator-facing labels.

### 6. Tests Required

When changing credential/quota observability, add or update tests for:

- `CreateRequestExecution` stores credential/resource/quota snapshots from context.
- `UsageLog` stores the same safe credential/resource/quota identity from context without persisting raw secrets.
- Credential local quota filters only the affected credential view, not unrelated credentials on the same channel.
- Same-channel multi-key routing where K1 is exhausted and K2 is available keeps the channel eligible but constrains the API-key provider to K2.
- Same-channel all-key-exhausted routing filters the channel in exhausted-only mode and reports quota exhaustion when no executable candidates remain.
- Frontend request list/detail queries include the snapshot fields used by the UI.

### 7. Wrong vs Correct

#### Wrong

```text
runtime chooses credential but UI falls back to channel/api key fragments
```

#### Correct

```text
credential resolver -> context safe credential target
-> request execution / usage log snapshot
-> request UI displays credential name and request outcome
```

---

## Credential Archive/Delete Product Contract

### 1. Scope / Trigger

Read this section before changing:

- GraphQL credential archive/delete mutations.
- Credential status mutations.
- Credential/channel ref restoration or deletion behavior.
- Credentials UI actions that archive, restore, delete, or explain routing availability.

For channel ownership, OAuth migration, and quota semantics, read [Credential Routing Model](./credential-routing-model.md). This section only covers archive/delete product behavior.

### 2. Signatures

Backend service/API signatures:

```go
func (svc *UpstreamCredentialService) ArchiveUpstreamCredential(ctx context.Context, id int) (*ent.UpstreamCredential, error)
func (svc *UpstreamCredentialService) DeleteUpstreamCredential(ctx context.Context, id int) (bool, error)
```

GraphQL product mutation:

```graphql
archiveUpstreamCredential(id: ID!): UpstreamCredential!
deleteUpstreamCredential(id: ID!): Boolean!
updateUpstreamCredentialStatus(id: ID!, status: UpstreamCredentialStatus!): UpstreamCredential!
```

### 3. Contracts

- Archive is a reversible product action: set `UpstreamCredential.status=archived`, preserve `ChannelCredentialRef` rows, reload channel routing state, and keep history/safe metadata readable. Runtime already excludes archived credentials because credential views require `ref.enabled && credential.status=enabled`.
- Re-enabling an archived credential must restore routing availability. To recover rows archived by older code, the archived-to-enabled transition may restore refs for that credential.
- Creating a credential with a secret that matches an archived credential should reactivate/update the archived credential instead of returning a still-archived row unchanged.
- Delete is the irreversible product action for credential management: remove channel refs, soft-delete the credential, reload channel routing state, and allow the same secret to be added again.
- Archive does not wipe `secret_payload` unless a future explicit wipe action is added.
- `CredentialQuotaScope` may explain key-local quota filtering, but archive/delete route availability must be explained by credential status and refs.

### 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| Archive credential with enabled channel refs | Preserve refs, return archived credential, and rely on credential status to remove it from runtime routing. |
| Re-enable archived credential | Set status enabled and recover refs when needed so routing availability returns. |
| Create credential with same secret as archived credential | Reactivate/update the archived credential and return it enabled by default. |
| Delete credential with channel refs | Delete refs, soft-delete credential, and allow same secret recreation. |
| Archive credential with request/usage history | Preserve history and safe snapshots; do not hard delete rows. |

### 5. Good / Base / Bad Cases

- Good: archive action tells the operator routing stops, refs are preserved for restore, and history remains visible.
- Good: delete action tells the operator refs are removed and the same secret can be added again.
- Base: credential has no local quota scope; routing still derives from channel status, ref status, credential status, and model eligibility.
- Bad: frontend displays `quotaScope.status || credential.quotaStatus` as one generic quota badge.
- Bad: archive is only reachable by a generic status dropdown with no side-effect confirmation.
- Bad: deleting a credential only sets `status=archived`, leaving refs in place and preventing same-secret recreation.

### 6. Tests Required

When changing this contract, add or update tests for:

- `archiveUpstreamCredential` preserving refs and returning an archived credential.
- Runtime credential views excluding archived credentials even when refs remain enabled.
- Archived same-secret create reactivating the credential.
- `deleteUpstreamCredential` removing refs, soft-deleting credential, and allowing same-secret recreation.
- Frontend type checks for archive/delete/restore actions and quota display fields.

### 7. Wrong vs Correct

#### Wrong

```text
delete action = set status archived from generic status dialog
restore action = create a new credential and lose old refs/history
```

#### Correct

```text
archive action = explicit confirmation -> archive mutation -> refs preserved for restore
restore action = set status enabled -> old refs can become routable again
delete action = explicit confirmation -> delete mutation -> refs removed + credential soft-deleted
```
