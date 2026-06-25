# Backend Routing Guidelines

> Routing, load-balancing, sticky-session, credential, quota, retry, and circuit-breaker contracts for backend development.

---

## Pre-Attempt Selection Contract

### 1. Scope / Trigger

Read this section before changing candidate selection, primary target choice,
load-balancer ordering, sticky-session ordering, retry candidate preparation, or
request-scoped execution target state.

### 2. Contracts

- Pre-call routing has two separate phases:
  - feasible candidate construction from request facts and local hard state;
  - primary decision from the feasible candidate set.
- Feasible candidate construction keeps the full eligible candidate set. It
  must not pre-truncate candidates based on retry count or fallback depth.
- Primary decision chooses the first attempt only. It must not precompile a
  retry/fallback queue.
- Hard local state may affect eligibility before the primary decision, for
  example disabled channels, disabled credentials, archived credentials, local
  quota pause/disable, admission rejection, and circuit-breaker-open state.
- Soft observations may affect ranking only, for example latency, recent
  errors, load, and non-authoritative health observations.
- Attempt errors are local to the attempt that produced them. They must not
  rewrite another request's sticky binding or already-built execution target
  unless the system first promotes them into explicit, observable hard local
  state.
- Once an `AttemptTarget` is built for a request, do not silently replace it
  because another concurrent request changed soft observations. If the attempt
  fails, retry/fallback handles recovery after the failure.
- The primary decision must be explainable: normal rank first, sticky binding
  hit, or sticky binding ignored with a concrete reason.

### 3. Wrong vs Correct

#### Wrong

```text
candidate set -> truncate to retry budget -> sticky reorders whole queue
```

This mixes selection with failure recovery before any attempt has failed.

#### Correct

```text
known facts -> full feasible candidate set
-> rank current tier
-> primary decision
-> build attempt target
-> retry/fallback only after failure
```

This keeps selection, execution, and recovery in separate phases.

---

## API Key Route Template Contract

### 1. Scope / Trigger

Read this section before changing API key profiles, API key profile templates,
candidate ordering, channel ordering weight, sticky-session first-bind behavior,
or runtime interpretation of legacy API key profile channel filters.

### 2. Signatures

Durable route fields on `objects.APIKeyProfile`:

```go
type APIKeyProfile struct {
    Name               string
    RouteTiers         []APIKeyRouteTier
    PreferredChannelID *int
    RouteMigration     *APIKeyRouteMigration

    // Compatibility-only migration input. Runtime routing must not read these.
    ChannelIDs           []int
    ChannelTags          []string
    ChannelTagsMatchMode ChannelTagsMatchMode
}

type APIKeyRouteTier struct {
    Name       string
    ChannelIDs []int
}
```

Runtime route shape:

```text
local API key -> active API key profile/template
-> ordered route tiers -> ordered feasible candidates -> concrete target
```

### 3. Contracts

- API key route profile/template is the only runtime ordering source for local
  API key routing.
- `Channel.ordering_weight` is display ordering. It must not decide runtime
  route priority, tie-breaks, or first bind.
- New profile writes must provide non-empty `routeTiers`. Empty route tiers are
  an explicit invalid route profile, not "all channels".
- Legacy `APIKeyProfile.ChannelIDs`, `ChannelTags`, and
  `ChannelTagsMatchMode` are migration inputs only. Save/migration paths may
  convert them into explicit `routeTiers`, then clear the legacy fields.
  Runtime selection must not read them as fallback routing rules.
- Existing empty legacy profiles may be migrated once into an explicit
  "enabled channels" tier. This is persisted data, not a runtime default group.
- Runtime selection first builds a feasible candidate set from request facts and
  hard local state, then intersects that set with the active profile's route
  tiers in order.
- A route tier channel ID may match more than one feasible candidate, for
  example same-channel model fallback entries. Treat the feasible candidate set
  as a multi-value list keyed by channel, not as `map[channelID]candidate`.
  Route ordering may move a whole channel group, but must preserve the order of
  candidates inside that channel group.
- Inside a tier, `preferredChannelID` wins when feasible. Otherwise deterministic
  API-key affinity may choose one candidate within the tier. Random scoring,
  adaptive scoring, and load-balancer sorting must not run.
- Sticky-session may reorder only candidates already produced by the route
  profile. No binding, weak sticky key, or expired binding returns the existing
  route-tier order unchanged.

### 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| API key missing or unauthenticated route state | Return explicit API-key route profile error. |
| Active API key profile missing | Return explicit active profile required error. |
| Active profile has no valid route tiers | Return invalid model/route error. Do not route via legacy fields or all channels. |
| Route tiers reference channels that are not feasible for the request | Skip those channels and continue to later tiers. |
| No route tier has feasible candidates | Return invalid model/no feasible route error. |
| `preferredChannelID` is configured and feasible in a tier | Put that channel first in that tier. |
| `preferredChannelID` is outside route tiers on write | Reject the profile/template write. |
| Legacy `ChannelIDs` are submitted on save | Convert once to `routeTiers`, clear legacy fields, and store migration metadata. |
| Legacy `ChannelTags` are submitted on save with resolvable channels | Resolve once to concrete channel IDs, clear legacy fields, and store migration metadata. |

### 5. Good / Base / Bad Cases

- Good: profile route tier `[2, 1]` with `preferredChannelID=1` produces channel
  `1` before channel `2`, regardless of channel display order.
- Good: a tier containing channel `1` keeps both `channel 1 / gpt-4` and
  `channel 1 / gpt-3.5-turbo` candidates in order so same-channel model
  fallback still works.
- Good: a migrated legacy profile shows explicit `routeTiers` in GraphQL and the
  API key UI, with `routeMigration.source` explaining where it came from.
- Base: an old template without `routeTiers` is edited by expanding its legacy
  `channelIDs` into a visible tier before save.
- Bad: missing `routeTiers` silently falls back to all enabled channels.
- Bad: building `map[channelID]candidate` and overwriting earlier same-channel
  model fallback candidates.
- Bad: sticky first-bind calls load-balanced ordering when no binding exists.
- Bad: equal route candidates are sorted by `ordering_weight`.

### 6. Tests Required

When changing API key route-template routing, add or update tests for:

- Route profile ordering by tier order and `preferredChannelID`.
- Missing `routeTiers` returns a clear error and does not consult legacy
  `ChannelIDs` or `ChannelTags`.
- Save-time legacy `ChannelIDs` migration clears old fields and persists
  `routeMigration`.
- `preferredChannelID` outside route tiers is rejected.
- Sticky unbound and expired-binding paths preserve route-tier order and do not
  call load-balanced ordering.
- Same-channel multi-model candidates remain present and ordered after route
  tier intersection.
- Frontend profile/template writes send `routeTiers` and `preferredChannelID`,
  not legacy channel/tag route fields.

When writing `ChatCompletionOrchestrator.Process` integration tests that rely on
API-key-aware routing or quota behavior, always create an explicit API key
context with an active profile whose `routeTiers` are non-empty. Test bypass
context only disables auth/privacy checks; it does not provide a default API
key or a fallback route profile.

### 7. Wrong vs Correct

#### Wrong

```text
API key profile missing routeTiers
-> read legacy channelIDs/channelTags
-> if still empty, load-balance all channels
```

This keeps two routing systems alive under one product surface.

#### Correct

```text
API key active profile
-> require explicit routeTiers
-> intersect feasible candidates by tier order
-> preferredChannelID or deterministic key affinity inside the tier
```

This makes the route answerable from one data structure.

---

## Attempt Target Contract

### 1. Scope / Trigger

Read this section before changing outbound execution, credential selection,
request execution snapshots, same-channel credential fallback, or provider auth
adapters.

### 2. Contracts

- One attempt must be represented by one concrete `AttemptTarget`.
- An `AttemptTarget` includes channel, model entry, API format/endpoint, and
  credential identity.
- The outbound execution layer consumes the `AttemptTarget`; it must not
  silently select a different credential after the target has been built.
- Same-channel credential fallback means moving from one concrete
  `AttemptTarget` to another concrete `AttemptTarget` on the same channel with a
  different credential.
- Same-channel credential fallback is valid only when the provider/auth layer can
  honor request-scoped credential targets and exclusions.
- Providers whose credentials are bound during transformer construction must be
  treated as not supporting automatic same-channel credential fallback until
  they are migrated to request-scoped credential consumption.
- Request execution, usage logging, sticky binding refresh, and fallback
  diagnostics must be explainable from the concrete `AttemptTarget` and its
  result.

### 3. Wrong vs Correct

#### Wrong

```text
primary decision -> channel
-> outbound provider silently picks any eligible credential
-> fallback guesses which credential failed
```

This keeps credential choice hidden inside execution and makes fallback
ambiguous.

#### Correct

```text
primary decision -> AttemptTarget(channel, model, api_format, credential)
-> outbound executes exactly that target
-> AttemptResult records the concrete target outcome
-> fallback chooses the next concrete AttemptTarget
```

This makes retry, fallback, sticky binding, and observability operate on the
same execution unit.

---

## Attempt Error Classification Contract

### 1. Scope / Trigger

Read this section before changing upstream error parsing, retry/fallback
planning, model capability handling, request execution diagnostics, or any code
that promotes an upstream failure into local routing state.

D phase is classification only. It records what the failed attempt taught the
system. It must not choose the next target, mutate sticky bindings, or silently
disable a channel or credential.

### 2. Signatures

Required conceptual shape:

```text
AttemptFailure(
  target: AttemptTarget,
  user_visible_started: bool,
  upstream_status: safe status/code/message,
  class: AttemptFailureClass,
  scope: AttemptFailureScope,
  confidence: high | medium | low
)
```

Required classes:

- `selection_modeling_gap`: local facts were sufficient to exclude this target,
  but A/B/C still selected it.
- `runtime_capability_drift`: the target was locally feasible, but upstream
  runtime state changed or was not knowable before the attempt.
- `request_invalid`: the request is invalid for the configured system, for
  example no feasible model/profile/API-format candidate exists.
- `credential_auth`: the concrete credential is rejected or unauthorized.
- `credential_quota_or_billing`: the concrete credential/account is blocked by
  upstream quota, rate limit, billing, or similar runtime response.
- `transient_transport`: network, timeout, connection reset, empty response, or
  equivalent transport failure.
- `upstream_capacity`: provider/proxy overload, queue-full, retryable 5xx, or
  equivalent capacity failure.
- `unknown_upstream`: upstream failed but the system cannot classify it with
  useful confidence.

Required scopes:

- `request`
- `credential`
- `target`
- `channel_model_api_format`
- `channel`
- `transport`
- `unknown`

### 3. Contracts

- Classification must be based on the concrete `AttemptTarget` and the safe
  upstream error returned by that attempt.
- Classification must not hide a bad A/B/C model. If local facts already said a
  target was impossible, the result is `selection_modeling_gap`, not normal
  fallback.
- `model not found` is not one universal class:
  - if local model/API-format/channel facts should have excluded the target, it
    is `selection_modeling_gap`;
  - if the target was locally feasible and upstream dynamically withdrew the
    model or changed proxy capability, it is `runtime_capability_drift`;
  - if the request has no feasible configured candidate, it is
    `request_invalid`.
- Runtime capability drift is a real attempt failure, but it is not provider
  quota and must not become durable provider quota state.
- A single `runtime_capability_drift` or quota-like provider response must not
  disable a whole channel or credential unless a separate explicit hard-state
  mechanism promotes it with observable evidence.
- D phase records `user_visible_started`. E phase uses that field to block
  silent fallback after user-visible streaming output has started.
- Unknown errors should stay visible as unknown. Do not broaden regex matching
  until unrelated provider errors collapse into the same action.

### 4. Validation & Error Matrix

| Condition | Required Classification |
|-----------|-------------------------|
| Local model binding/API format excludes the target but it was attempted | `selection_modeling_gap`, scope `target` or `channel_model_api_format`, high confidence. |
| Upstream returns `model not found` for a locally feasible target | `runtime_capability_drift`, scope `channel_model_api_format` or `target`; do not mark provider quota. |
| Request asks for a model/profile/API format with no feasible configured candidate | `request_invalid`, scope `request`. |
| Upstream rejects the concrete key/token with auth or permission text/status | `credential_auth`, scope `credential` or `target`. |
| Upstream returns quota, billing, rate-limit, or insufficient balance style error | `credential_quota_or_billing` when credential/account scoped; otherwise `upstream_capacity` or `unknown_upstream`. |
| Network timeout, connection reset, EOF, empty response | `transient_transport`, scope `transport` or `target`. |
| 5xx, overloaded, queue full, retry later | `upstream_capacity`, scope `channel` or `target`. |
| Streaming already emitted user-visible tokens before failure | Keep the same class/scope and set `user_visible_started=true`. |

### 5. Good / Base / Bad Cases

- Good: `model not found` on a locally feasible target is recorded as
  `runtime_capability_drift`, then recovery may try another feasible target
  before output starts.
- Good: a locally impossible target being selected is reported as
  `selection_modeling_gap`, making the selection bug visible.
- Base: unknown upstream text stays `unknown_upstream` and remains visible in
  attempt diagnostics.
- Bad: all `model not found` errors are silently treated as ordinary fallback
  success paths.
- Bad: quota-like provider text is persisted as provider quota product state.
- Bad: error regexes classify broad provider text so aggressively that request
  bugs look retryable.

### 6. Tests Required

When changing attempt error classification, add or update tests for:

- `model not found` split into `selection_modeling_gap`,
  `runtime_capability_drift`, and `request_invalid` based on local feasibility.
- Provider quota-like responses are attempt failures, not provider quota state.
- `user_visible_started=true` is preserved on classified streaming failures.
- Broad or unknown upstream text remains visible as `unknown_upstream`.
- Classification alone does not mutate sticky bindings, channel status, or
  credential status.

### 7. Wrong vs Correct

#### Wrong

```text
upstream: model not found
-> mark channel bad
-> silently hop targets
-> only show final success
```

This hides whether the real issue was bad local modeling, upstream drift, or a
bad user request.

#### Correct

```text
upstream: model not found
-> compare with local feasibility facts
-> classify as selection gap, runtime drift, or invalid request
-> record failed AttemptTarget
-> let fallback planning decide the next action
```

This keeps failure knowledge separate from recovery behavior.

---

## Recovery Action Planner Contract

### 1. Scope / Trigger

Read this section before changing fallback planning, same-target retry,
same-channel credential fallback, same-priority fallback, lower-priority
fallback, sticky target escape behavior, streaming failure handling, or retry
attempt budgeting.

E phase chooses the next recovery action after D has classified a failed
attempt. It must not reinterpret the upstream error or mutate the failure
classification.

### 2. Signatures

Required conceptual input:

```text
FallbackPlannerInput(
  failure: AttemptFailure,
  candidate_set: CandidateSet,
  attempt_history: []AttemptResult,
  policy: RecoveryPolicy,
  request_state: cancellation/timeout/streaming state
)
```

Required conceptual output:

```text
FallbackDecision(
  action:
    return_error
    | retry_same_target
    | fallback_same_channel_credential
    | fallback_same_priority_target
    | fallback_lower_priority,
  next_target: AttemptTarget?,
  reason: string,
  exhausted_scope: string?
)
```

### 3. Contracts

- E phase consumes D phase classification. It must not reclassify the upstream
  error, hide `selection_modeling_gap`, or convert provider quota-like text into
  provider quota state.
- E phase runs only after an `AttemptTarget` fails. A/B must not precompile a
  fallback queue before any attempt has failed.
- If the user canceled or the configured request timeout expired, return the
  cancellation/timeout result. Do not keep fallback running.
- If `user_visible_started=true`, return the streaming failure. Silent fallback
  to another upstream target is forbidden after user-visible output starts.
- `request_invalid` returns an error directly. It is not recoverable by trying
  more targets.
- `selection_modeling_gap` returns a visible routing/modeling error. It must not
  be normalized into ordinary fallback success.
- `runtime_capability_drift` should not retry the same target. It may fallback
  to other A-feasible targets before output starts, staying in the same priority
  tier until that tier is exhausted.
- `credential_auth` and credential-scoped `credential_quota_or_billing` prefer
  same-channel credential fallback when the provider/auth path supports
  request-scoped credential targets and exclusions.
- If same-channel credential fallback is not supported by the provider/auth
  path, do not pretend it happened. Move to normal target fallback or return the
  real failure if no target remains.
- `transient_transport` may use minimal same-target retry when policy allows it,
  but repeated same-target retry must not dominate recovery while other concrete
  targets remain.
- `upstream_capacity` usually falls back to another target. Same-target retry
  should be rare and explicitly justified.
- `unknown_upstream` should be conservative: keep the failed attempt visible,
  avoid broad regex-driven action, and do not promote it into hard local state
  without separate evidence.
- Priority is a service contract. E must exhaust the current priority tier
  before lower-priority fallback begins.
- Every recovery action must produce a new concrete `AttemptTarget` or a direct
  terminal result. The outbound layer must never receive an ambiguous channel
  without the selected credential/model/API-format.

### 4. Validation & Error Matrix

| Failure / State | Required Recovery Action |
|-----------------|--------------------------|
| User canceled | `return_error`; surface cancellation. |
| Configured request timeout expired | `return_error`; surface timeout. |
| `user_visible_started=true` | `return_error`; no silent fallback. |
| `request_invalid` | `return_error`; do not try more targets. |
| `selection_modeling_gap` | `return_error` with visible routing/modeling diagnostics. |
| `runtime_capability_drift` before output starts | Fallback to another A-feasible target; same priority before lower priority; no same-target retry. |
| `credential_auth`, same channel has another eligible credential, provider supports request-scoped credentials | `fallback_same_channel_credential`. |
| `credential_auth`, provider cannot honor request-scoped credential exclusions | Use normal target fallback or `return_error`; do not fake credential fallback. |
| Credential-scoped `credential_quota_or_billing` | Prefer `fallback_same_channel_credential`, then same-priority target fallback, then lower-priority fallback after exhaustion. |
| `transient_transport` | Minimal `retry_same_target` if allowed, then fallback. |
| `upstream_capacity` | Prefer target fallback; same-target retry only with explicit justification. |
| `unknown_upstream` | Conservative fallback if policy allows and no output started; preserve diagnostics. |

### 5. Good / Base / Bad Cases

- Good: upstream dynamically withdraws a model, D classifies
  `runtime_capability_drift`, E tries another same-priority A-feasible target
  before any streaming output starts.
- Good: a key is rejected, same-channel credential fallback uses a different
  concrete credential and request detail shows both attempts.
- Good: all current-priority targets are exhausted, lower-priority fallback
  begins, and the decision reason records the priority drop.
- Base: a transport timeout gets one same-target retry, fails again, then
  escapes to another concrete target.
- Bad: a request with no feasible configured model is sprayed across every
  channel.
- Bad: a `selection_modeling_gap` is hidden behind final fallback success.
- Bad: streaming output has begun and fallback switches to a different upstream
  target.
- Bad: same-channel credential fallback is claimed even though the provider auth
  layer still binds credentials during transformer construction.

### 6. Tests Required

When changing recovery planning, add or update tests for:

- `request_invalid` and `selection_modeling_gap` terminate without ordinary
  fallback.
- `runtime_capability_drift` avoids same-target retry and stays in the current
  priority tier until exhausted.
- Same-channel credential fallback only runs when request-scoped credential
  targeting is supported.
- Credential auth/quota failures prefer same-channel credential fallback before
  cross-channel fallback when possible.
- Lower-priority fallback starts only after current-priority exhaustion.
- User cancellation, configured timeout, and `user_visible_started=true` stop
  silent fallback.
- Each fallback attempt builds a new concrete `AttemptTarget`.

### 7. Wrong vs Correct

#### Wrong

```text
failed attempt
-> regex says maybe retryable
-> prebuilt fallback queue picks any channel
-> outbound silently chooses a credential
```

This lets recovery blur classification, priority, and credential identity.

#### Correct

```text
failed AttemptTarget
-> D classifies AttemptFailure
-> E chooses one action from policy and remaining candidates
-> next action is either terminal or a new concrete AttemptTarget
```

This keeps recovery explicit and testable.

---

## Post-Success Learning Contract

### 1. Scope / Trigger

Read this section before changing sticky binding refresh, request execution
result persistence, soft routing observations, success/failure counters, latency
learning, or any code that writes routing state after a request succeeds.

F phase runs after recovery has reached a terminal success. It records what
actually happened and writes only the small amount of routing state that is safe
to learn from a successful attempt.

### 2. Signatures

Required conceptual input:

```text
PostSuccessLearningInput(
  sticky_key: string?,
  final_success: AttemptResult,
  attempt_history: []AttemptResult,
  fallback_decisions: []FallbackDecision
)
```

Required conceptual output:

```text
PostSuccessLearningOutput(
  sticky_binding_write: StickySessionBinding?,
  execution_records: []AttemptExecutionRecord,
  soft_observation_updates: []ObservationUpdate
)
```

### 3. Contracts

- Only a successful concrete `AttemptTarget` may refresh sticky binding.
- Primary success refreshes sticky binding to the primary `AttemptTarget`.
- Fallback success refreshes sticky binding to the final successful
  `AttemptTarget`, not to the original failed target.
- If all attempts fail, do not migrate, delete, or clear the old sticky binding.
  Let the binding expire by TTL and return the real failure path.
- Sticky binding target means successful `AttemptTarget` identity. It must not
  collapse back to channel-only state when credential/model/API-format identity
  is known.
- Final success must not overwrite failed attempt history, failure
  classifications, or fallback decisions. Operators must be able to see the
  full path, not only the winner.
- F may update soft observations such as latency, recent success/failure
  counters, and non-authoritative health hints.
- F must not promote one success or failure into durable hard state. Hard-state
  promotion needs a separate explicit mechanism with observable evidence.
- F must not write provider quota state. Provider quota-like upstream responses
  remain attempt failures from D/E, not learned product state.
- F must not rewrite the explanations produced by A/B/C/D/E. Learning happens
  after the fact and cannot change why the request chose, failed, recovered, or
  succeeded.

### 4. Validation & Error Matrix

| Result | Required Learning Behavior |
|--------|----------------------------|
| Primary target succeeds | Refresh sticky binding to the primary `AttemptTarget`; record the successful attempt. |
| Fallback target succeeds | Refresh sticky binding to the final successful `AttemptTarget`; keep failed attempts visible. |
| All attempts fail | Do not update sticky binding; keep old binding until TTL; return the failure. |
| Stream starts and later fails | Do not treat it as success; do not silently fallback; do not refresh sticky to a failed target. |
| Successful target has credential identity | Store/bind safe credential identity; do not downgrade to channel-only state. |
| A failed attempt had quota-like provider text | Keep it as attempt failure diagnostics; do not write provider quota state. |
| Soft observations are updated | Mark them as soft/ranking inputs, not hard eligibility facts. |

### 5. Good / Base / Bad Cases

- Good: primary target succeeds and sticky binding becomes
  `stickyKey -> successful AttemptTarget`.
- Good: target A fails, fallback target B succeeds, sticky binding refreshes to
  B while request detail still shows A's failure.
- Good: all targets fail and the previous sticky binding remains unchanged until
  TTL expiry.
- Base: no sticky key exists, the request records execution history but writes no
  sticky binding.
- Bad: fallback succeeds and the system rewrites history as if the primary
  target succeeded.
- Bad: one successful request permanently marks a channel healthy or capable.
- Bad: a failed streaming response refreshes sticky binding because it emitted
  some tokens.

### 6. Tests Required

When changing post-success learning, add or update tests for:

- Sticky binding refreshes only after success.
- Fallback success binds to the final successful `AttemptTarget`.
- All-failed requests leave previous sticky binding unchanged.
- Failed attempt history remains visible after final success.
- Soft observation updates do not become hard eligibility state.
- Provider quota-like failures are not persisted as provider quota state.
- Streaming failure after user-visible output does not refresh sticky binding.

### 7. Wrong vs Correct

#### Wrong

```text
fallback succeeds
-> overwrite attempt history with final target
-> bind sticky to channel only
-> mark failed target healthy because request eventually succeeded
```

This erases the route the request actually took and corrupts future selection.

#### Correct

```text
fallback succeeds
-> keep every AttemptResult and FallbackDecision
-> bind sticky to the final successful AttemptTarget
-> update only soft observations
```

This lets success improve locality without hiding failures.

---

## Routing Explainability Contract

### 1. Scope / Trigger

Read this section before changing request execution records, route diagnostics,
request list/detail UI payloads, log/export fields, sticky/fallback telemetry,
or any GraphQL/REST response that explains routing behavior.

G phase is observability across the whole lifecycle. It does not choose targets,
fallback, classify errors, or learn new routing state.

### 2. Signatures

Required conceptual diagnostic shape:

```text
RoutingTrace(
  candidate_set_summary: CandidateSetSummary,
  primary_decision: PrimaryDecision,
  attempts: []AttemptTrace,
  fallback_decisions: []FallbackDecision,
  sticky_learning: PostSuccessLearningOutput?,
  final_result: success | error | canceled | timeout | stream_interrupted
)
```

Each `AttemptTrace` must be explainable from:

```text
AttemptTrace(
  target: safe AttemptTarget identity,
  result: success | failure,
  failure: AttemptFailure?,
  usage: safe usage/cost/latency summary
)
```

### 3. Contracts

- Explainability must cover the lifecycle:
  - A: why candidates were feasible and which hard filters removed others;
  - B: why the primary target was chosen;
  - C: which concrete `AttemptTarget` was executed;
  - D: how each failure was classified;
  - E: why retry/fallback/direct error was chosen;
  - F: whether sticky binding or soft observations were updated;
  - final result: success, direct error, cancellation, timeout, or stream
    interruption.
- Request detail must preserve the full attempt chain. Final success must not
  hide failed attempts.
- Request list should show operator-useful labels only: channel name, credential
  name/key hint, model/API format, status, usage, cost, latency, and concise
  fallback status.
- Debug-only identifiers such as fingerprints, `secret:v1:*`, raw refs, and
  internal resource keys must not be primary list labels. If needed for
  debugging, put them in structured detail/debug fields.
- Raw secrets must never appear in GraphQL responses, logs, exports, tooltips,
  or copy actions.
- Error diagnostics must include project classifications (`AttemptFailureClass`,
  scope, confidence) rather than only provider raw text.
- Fallback success must be visible as fallback success. It must not look like a
  single-attempt success.
- Diagnostic data must be structured enough for tests and UI to consume; do not
  rely on concatenated human strings as the only source of truth.

### 4. Validation & Error Matrix

| Situation | Required Explanation |
|-----------|----------------------|
| Candidate excluded before execution | Record hard filter reason in candidate summary. |
| Sticky binding hit | Record sticky hit and the bound `AttemptTarget`. |
| Sticky binding ignored | Record ignored reason such as disabled, model-ineligible, quota-ineligible, or outside current priority tier. |
| Primary selected by normal ranking | Record rank/priority/weight reason without claiming sticky decided it. |
| Attempt executes | Record safe channel/model/API-format/credential identity. |
| Attempt fails | Record `AttemptFailure` class/scope/confidence and safe upstream status/message. |
| Fallback runs | Record `FallbackDecision` action, next target, and reason. |
| Fallback succeeds | Final result is success, with prior failed attempts still visible. |
| Streaming output starts then fails | Final result is stream interruption; no silent fallback is shown. |

### 5. Good / Base / Bad Cases

- Good: request detail shows primary target A, `runtime_capability_drift`, a
  same-priority fallback decision, final target B, and sticky refresh to B.
- Good: request list shows `input(lite)` and a masked key hint, not
  `secret:v1:*`.
- Good: an operator can tell whether a request failed because it was invalid,
  because local selection picked an impossible target, or because upstream
  drifted after selection.
- Base: single-attempt success records one primary decision and one successful
  attempt.
- Bad: final success hides the failed sticky target.
- Bad: UI uses fingerprint/ref/debug strings as normal operator-facing labels.
- Bad: request export includes raw upstream secrets or unmasked bearer tokens.
- Bad: provider raw text is the only stored explanation for retry/fallback.

### 6. Tests Required

When changing routing observability, add or update tests for:

- Request detail includes all attempts and fallback decisions.
- Request list uses user-facing labels and masked key hints, not fingerprints or
  raw refs as primary labels.
- Failure classifications are persisted or exposed in structured fields.
- Fallback success remains distinguishable from single-attempt success.
- Streaming interruption shows no silent fallback after output started.
- Copy/export paths apply the same masking rules as the UI.
- Raw secrets never appear in API responses, logs, tooltips, or exports.

### 7. Wrong vs Correct

#### Wrong

```text
request row: success
detail: channel=input, key=secret:v1:...
debug text: provider said model not found, then ok
```

This is neither operator-friendly nor structurally useful.

#### Correct

```text
request row: success via fallback
detail:
  primary: AttemptTarget A
  failure: runtime_capability_drift
  fallback: same-priority target B
  final: success on AttemptTarget B
  sticky: refreshed to B
```

This makes the route explainable without leaking credential internals.

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
    CredentialID          int
    ChannelID             int
    CredentialFingerprint string
}

type StickySessionBinding struct {
    CredentialID          int
    ChannelID             int
    CredentialFingerprint string
    ExpiresAt             time.Time
}
```

Required runtime meaning:

- `stickyKey -> channel target`, not `request -> target`.
- `target` means the successful execution target. When credential identity is
  known, the binding may also carry safe credential snapshot data, but sticky
  lookup is still by key and channel.
- TTL is 5 minutes.
- Bindings are soft. They may be ignored when the bound channel is absent from
  the current candidate list.

### 3. Contracts

- Candidate channels still come from the active API key route profile, model,
  API-format, credential local quota, and hard eligibility rules.
- Sticky-session works on the candidate list already produced by the API-key
  route profile. It can reorder that list, but it must not synthesize new
  candidates, read response-chain state, or introduce a second routing policy
  under the `sticky-session` name.
- First unbound selection starts from the existing route-profile order. It must
  not call load-balanced ordering or add sticky-specific stable channel
  scoring.
- First unbound selection must not apply sticky-specific quota-ratio balancing. If local quota balancing is needed later, expose it as a separate explicit routing policy or sub-policy.
- A binding is created or refreshed only after an upstream request succeeds.
- A selected target must not be written to the binding store before upstream success.
- If the bound target fails and fallback succeeds, refresh the binding to the successful fallback target.
- If all attempts fail, keep the previous binding until TTL expiry and return the real retry/upstream error.
- Credential choice inside the selected channel is handled separately via the
  credential selection seed. Sticky binding itself stays channel-level.
- If fallback succeeds on a different channel, binding may refresh to that
  successful target.
- Sticky-session semantic states must stay explicit: binding hit, binding ignored, and normal first-bind. Do not silently change from sticky behavior to quota balancing or another routing policy under the same strategy name.

### 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| No qualified sticky key can be generated | Preserve the current candidate order. Do not create a binding. |
| Binding exists and target is eligible in the current candidate list | Place that target first for the current attempt. |
| Binding target is absent from the current candidate list | Ignore the binding for this attempt. Route/profile order continues. |
| Bound target returns network error, timeout, 5xx, empty response, retryable 429, or queue-full error | Let existing retry/fallback escape. Do not migrate on failed execution alone. |
| Fallback target succeeds | Refresh binding to the successful target. |
| All targets fail | Keep old binding until TTL. Return the real upstream/retry result. |
| Circuit breaker is open | Circuit breaker may skip the concrete target as a hard local gate. The surfaced error must be explicit and not a raw middleware wrapper. |
| No executable candidates remain after eligibility filtering | Return an explicit no-candidate/upstream-unavailable error, not a raw middleware wrapping error. |

### 5. Good / Base / Bad Cases

- Good: no binding exists, route-tier order selects channel A, upstream
  succeeds, binding becomes `stickyKey -> A`.
- Good: binding points to A, A fails once, retry/fallback reaches B and B succeeds, binding refreshes to B.
- Good: binding points to C from a fallback target, and later requests keep
  hitting C until TTL expiry or a hard eligibility change removes C from the
  candidate list.
- Base: no qualified sticky key exists, request preserves API-key route
  ordering.
- Bad: binding to A before A succeeds.
- Bad: using `hash(stickyKey + channelID)`, rendezvous hashing, load-balancer
  score, response-chain identity, transcript prefixes, or prompt-cache
  fingerprints to define sticky identity.
- Bad: keeping one product label while silently switching to another routing
  contract underneath it.
- Bad: returning `failed to apply raw request middlewares: skip candidate by
  circuit breaker` as the client-visible 500 for sticky-session routing.

### 6. Tests Required

When changing sticky-session or neighboring routing behavior, add or update tests for:

- No binding write before upstream success.
- Successful fallback refreshes binding to the successful target.
- Failed fallback does not migrate or delete the old binding.
- Circuit-breaker hard skips are surfaced explicitly, not as raw middleware wrappers.
- No executable candidate returns an explicit routing/unavailable error instead of a raw middleware wrapper.
- Sticky key extraction depends on API-key scope only, not on response chains or request fingerprints.
- Sticky first-bind does not perform quota-ratio balancing under the `sticky-session` strategy name.

### 7. Wrong vs Correct

#### Wrong

```text
candidate list -> sticky hash score -> pick channel -> write binding -> send upstream
```

This binds on selection instead of success and lets sticky-session introduce its own distribution logic.

#### Correct

```text
candidate list
-> API-key route-tier selection
-> sticky binding reorders the current candidate list
-> send upstream through existing retry/fallback
-> on success, bind stickyKey to the successful target for 5 minutes
```

This keeps sticky-session focused on cache locality while route tiers, quota,
health, retry, and fallback remain owned by the normal routing pipeline.

## Sticky Key Extraction

Sticky keys are generated from stable server-visible API-key scope. They are not client-supplied and do not have a confidence score.

Extractor shape:

```go
type StickyKeyResult struct {
    Key    string
    OK     bool
    Reason string
}
```

Rules:

- `OK=false` leaves the current API key route ordering unchanged.
- The key derives from API-key scope only. In this codebase that means the
  stable API-key scope currently available to the server (`APIKeyID` plus
  namespace data such as `ProjectID`).
- Request shape, `previous_response_id`, prompt cache, transcript prefixes,
  tool schema, and latest-message fingerprints do not contribute to sticky
  identity.
- Do not expose `stickyKey` in API responses, logs at unsafe verbosity, or client-visible errors.

---

## Retry / Fallback Guardrails

### 1. Scope / Trigger

Read this section before changing:

- Retry classification, same-channel retry, cross-channel fallback, or cross-route-tier fallback.
- Sticky-session escape behavior after a target fails.
- Credential-aware fallback, excluded-target tracking, or retry attempt budgeting.
- Any middleware or selector that can hide an upstream failure by silently switching targets.

This section defines project-level constraints, not only one implementation. The goal is to preserve user control and keep failures observable.

### 2. Contracts

- Retry/fallback must not hide real routing or upstream problems behind aggressive silent recovery.
- Retry/fallback must not decide for the user that a request has taken "too long" before the user cancels it or the configured request timeout expires.
- The system may bound retry/fallback by structural scope such as target count,
  credential count, route-tier drop count, or total attempts. It must not add a
  separate hidden time-budget cutoff just for fallback behavior.
- Recovery should depend primarily on fallback, not on repeated same-target retry. Same-target retry should stay rare and narrowly justified.
- Same-target retry is less valuable once sticky-session and credential-aware fallback exist. In sticky routing, repeatedly hitting the same failed target usually harms escape behavior more than it helps cache locality.
- Fallback must stay local before it becomes global:
  - same target retry first when the error is plausibly transient,
  - then same-channel credential fallback,
  - then same-route-tier target fallback,
  - then lower-route-tier fallback only after the current route tier is exhausted.
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
- Route tier is a key-owned service contract. Normal routing stays inside the
  first feasible route tier. Lower-tier fallback is a deliberate degradation
  step, not a normal balancing path.

### 3. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| User has not canceled and request timeout has not expired | Retry/fallback may continue within structural attempt limits. Do not stop early because a fallback-specific time budget was reached. |
| User cancels the request | Stop retry/fallback and surface cancellation. |
| Same target fails with plausibly transient error | Same-target retry may occur if the retry policy and error classification both allow it, but keep it minimal. Prefer later fallback over repeated same-target retries. |
| Current credential fails with credential-scoped error and same channel has another eligible credential | Prefer same-channel credential fallback before switching channels. |
| Current credential fails but the channel's auth layer cannot honor request-scoped credential exclusions | Do not pretend same-channel credential fallback happened. Use normal cross-channel fallback or surface the failure when no fallback target remains. |
| Current route tier still has untried eligible targets | Do not cross to a lower route tier yet. |
| Current route tier is exhausted and lower-tier fallback is allowed | Lower-tier fallback may begin. |
| Request/model/configuration error is clearly non-retryable | Surface the error. Do not keep hopping targets just to improve apparent success rate. |
| Sticky target fails and same-target retry is not clearly justified | Escape to fallback rather than repeatedly retrying the sticky target. |
| Sticky target fails but fallback succeeds elsewhere | Return success, refresh sticky binding to the successful target, and keep the original failure observable. |
| Response has already emitted user-visible tokens | Do not perform silent fallback to a different target. |

### 4. Good / Base / Bad Cases

- Good: sticky target fails with a credential-scoped error, another credential on the same channel succeeds, and the request execution history shows both attempts.
- Good: a sticky target gets at most one transient same-target retry, then quickly escapes to credential-aware fallback when the target still fails.
- Good: all targets in the current route tier fail, fallback drops to the next
  route tier, and a later success refreshes sticky binding while leaving the
  earlier failures visible.
- Base: a long-running request continues retry/fallback within normal request lifetime because the user has not canceled it.
- Bad: fallback stops after an internal 4-second budget even though the user still wants the request to continue.
- Bad: the router keeps retrying the same sticky target several times even though other eligible credentials or channels are available.
- Bad: a failing sticky target silently causes many hidden retries and then only the final success is visible to operators.
- Bad: the router jumps to a lower route tier before exhausting the current one.
- Bad: a stream has already started, fallback switches upstreams, and the user receives mixed output from different targets.

### 5. Tests Required

When changing retry/fallback behavior, add or update tests for:

- No fallback-specific hidden time budget cuts off an otherwise valid request.
- Same-target retry remains minimal and does not dominate recovery when fallback targets are available.
- Same-channel credential fallback is attempted before cross-channel fallback when classification says the failure is credential-scoped.
- Credential fallback tests must cover both request-scoped API-key providers and construction-time auth providers, or explicitly document that construction-time auth providers are not covered by same-channel fallback yet.
- Lower-route-tier fallback does not begin until the current route tier is exhausted.
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
- Provider quota is not a credential execution model. Quota-like upstream responses are attempt errors and must not be snapshotted as provider quota state.

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
