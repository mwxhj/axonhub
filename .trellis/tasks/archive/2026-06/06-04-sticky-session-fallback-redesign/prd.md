# Sticky Session Fallback Redesign

## Goal

Redesign backend routing so sticky-session, credential selection, retry,
fallback, post-success learning, and request observability have one consistent
runtime model.

The implementation target is an A-G request lifecycle:

- A: build the complete feasible candidate set from request facts and local hard
  state.
- B: choose the first attempt only.
- C: execute one concrete `AttemptTarget`.
- D: classify failed attempts.
- E: choose recovery action after failure.
- F: learn only from final success.
- G: expose the full route in structured diagnostics and user-facing UI.

## Problem

Current routing behavior is hard to reason about because several phases are
mixed together:

- `sticky-session` can embed quota-ratio first-bind balancing while still using
  the sticky strategy name.
- Candidate selection, ordering, sticky binding, retry preparation, and fallback
  behavior are spread across `selectCandidates`, `orderCandidates`,
  `loadBalancedCandidates`, `StickySessionRouter`, and outbound execution.
- Candidate preparation can truncate or order future attempts before any attempt
  has failed.
- Outbound execution can still hide the concrete credential choice inside
  provider internals.
- Provider quota is treated like product/routing state even though quota-like
  upstream responses are runtime attempt failures.
- Request list/detail views can expose low-value debug identifiers while hiding
  the route and fallback chain that operators actually need.

This task fixes the model first. Code changes must make the runtime follow the
same boundaries.

## Non-Goals

- Do not remove sticky-session as a feature.
- Do not remove retry/fallback entirely.
- Do not add hidden fallback time budgets.
- Do not reintroduce provider quota as product state, durable routing state, or
  UI state.
- Do not keep channel-owned secret/OAuth write paths as first-class behavior.
- Do not use final success to hide failed attempts.

## Design Decisions

### Sticky Session

- `sticky-session` is pure sticky:
  - reuse an eligible successful sticky binding when one exists;
  - otherwise choose the primary target from normal current-tier ordering;
  - create or refresh binding only after upstream success.
- Sticky-session must not perform quota-ratio first-bind balancing.
- Sticky-session must not silently degrade into a different routing policy while
  still presenting itself as sticky.
- If local quota-ratio balancing is added later, it must be an explicit routing
  policy or sub-policy, not hidden inside sticky-session.

### Provider Quota

- Provider quota does not exist as a product, domain, or routing model.
- Only local credential quota exists as quota.
- Provider quota-like upstream responses are attempt failures classified by
  scope. They must not become provider quota state.
- Existing provider quota storage, UI, GraphQL fields, snapshots, or runtime
  readers are cleanup targets in the implementation slice.

### Candidate / Decision / Attempt Split

- `CandidateSet`: complete eligible candidate pool grouped by priority tier.
- `PrimaryDecision`: first attempt choice and reason.
- `AttemptTarget`: concrete execution target containing channel, model entry,
  API format/endpoint, and credential.
- `AttemptFailure`: classified failure for one failed `AttemptTarget`.
- `FallbackDecision`: one recovery decision after one failure.
- `AttemptResult`: success/failure result for one `AttemptTarget`.
- `RoutingTrace`: structured explanation of A-G lifecycle.

Selection time must keep the full feasible candidate set. Retry/fallback limits
must not pre-truncate the candidate set.

## Runtime Lifecycle

### A. Feasible Candidate Set

A phase builds the complete feasible candidate set from request facts and local
hard state.

Hard request facts include:

- requested model/profile/tag;
- API format and endpoint family;
- stream/non-stream request shape;
- tools, response format, image or other capability requirements when locally
  modeled;
- channel enabled state;
- model/channel binding;
- credential enabled/archived/deleted state;
- local credential quota hard actions;
- admission rejection;
- circuit-breaker open state.

Soft observations such as latency, recent errors, load, and non-authoritative
health may rank candidates later, but they must not become hard filters unless a
separate explicit hard-state mechanism promotes them.

### B. Primary Decision

B phase chooses only the first attempt.

Allowed inputs:

- priority tier;
- rank/weight;
- sticky binding eligibility;
- hard health state;
- soft observations as ranking hints only.

Forbidden behavior:

- precompile a retry/fallback queue;
- pre-truncate candidates by retry budget;
- update sticky binding;
- update health learning state;
- mix quota balancing into `sticky-session`.

Concurrent attempt failures are local to the request that produced them. A
failure in another request must not rewrite this request's already-built
`AttemptTarget` unless the system first promotes that information into explicit
observable hard state.

### C. Concrete Attempt Target

One attempt is one `AttemptTarget`:

```text
AttemptTarget(
  channel,
  model_entry,
  api_format_or_endpoint,
  credential
)
```

Outbound execution must consume this target exactly. It must not silently
reselect a credential after the target is built.

Same-channel credential fallback is valid only for provider/auth paths that can
honor request-scoped credential targets and exclusions. Providers whose auth is
bound during transformer construction must be marked as not supporting automatic
same-channel credential fallback until migrated.

Same-channel multi-model retry has no special hidden behavior. If multiple model
entries are feasible, they are represented as candidates/targets in A/B/C and
handled through normal fallback rules.

### D. Attempt Error Classification

D phase classifies what a failed attempt taught the system. It does not choose
the next target, mutate sticky bindings, or disable channels/credentials.

Required failure dimensions:

- class;
- scope;
- confidence;
- safe upstream status/message;
- whether user-visible streaming output already started.

Required failure classes:

- `selection_modeling_gap`
- `runtime_capability_drift`
- `request_invalid`
- `credential_auth`
- `credential_quota_or_billing`
- `transient_transport`
- `upstream_capacity`
- `unknown_upstream`

`model not found` must be split by local feasibility:

- local facts should have excluded the target: `selection_modeling_gap`;
- local target was feasible but upstream dynamically changed: `runtime_capability_drift`;
- no feasible configured candidate exists: `request_invalid`.

Runtime capability drift is a real attempt failure. It is not provider quota and
must not become durable provider quota state.

### E. Recovery Action Planning

E phase consumes `AttemptFailure`, `CandidateSet`, `AttemptHistory`, policy, and
request state. It returns either a terminal result or one new concrete
`AttemptTarget`.

Allowed actions:

- `return_error`
- `retry_same_target`
- `fallback_same_channel_credential`
- `fallback_same_priority_target`
- `fallback_lower_priority`

Recovery mapping:

- `request_invalid`: return direct error.
- `selection_modeling_gap`: return visible routing/modeling error.
- `runtime_capability_drift`: do not retry same target; fallback to other
  A-feasible targets before output starts.
- `credential_auth`: prefer same-channel credential fallback when supported.
- credential-scoped `credential_quota_or_billing`: prefer same-channel
  credential fallback, then same-priority target fallback, then lower-priority
  fallback after exhaustion.
- `transient_transport`: allow minimal same-target retry when policy allows,
  then fallback.
- `upstream_capacity`: usually fallback to another target; same-target retry
  must be rare and justified.
- `unknown_upstream`: conservative fallback only when policy allows and no output
  has started.

Terminal conditions:

- user canceled;
- configured request timeout expired;
- `user_visible_started=true`.

Lower-priority fallback is a deliberate degradation step. It may begin only after
the current priority tier is exhausted.

### F. Post-Success Learning

F phase runs only after terminal success.

Allowed writes:

- sticky binding refresh to the final successful concrete `AttemptTarget`;
- request execution records for all attempts;
- soft observation updates such as latency and recent success/failure hints.

Rules:

- Primary success binds to the primary target.
- Fallback success binds to the final successful fallback target.
- All-failed requests do not migrate, delete, or clear the old sticky binding.
- Sticky target identity must not collapse back to channel-only state when
  credential/model/API-format identity is known.
- Final success must not overwrite failed attempt history, failure
  classifications, or fallback decisions.
- F must not write provider quota state.
- F must not rewrite A/B/C/D/E explanations after the fact.

### G. Explainability

G phase exposes the route. It does not choose targets, fallback, classify
errors, or learn state.

Every request must be explainable by phase:

- A: feasible candidates and hard filter reasons.
- B: primary decision reason.
- C: concrete executed `AttemptTarget`.
- D: failure class, scope, confidence, and safe upstream error.
- E: retry/fallback/direct-error decision and reason.
- F: sticky refresh and soft observation updates.
- Final result: success, direct error, cancellation, timeout, or stream
  interruption.

Request detail must preserve the full attempt chain. Final success must not hide
failed attempts.

Request list should show operator-useful labels only:

- channel name;
- credential name/key hint;
- model/API format;
- status;
- usage/cost/latency;
- concise fallback status.

Fingerprints, `secret:v1:*`, raw refs, and internal resource keys are
debug/detail fields, not primary UI labels. Raw secrets must never appear in
GraphQL responses, logs, exports, tooltips, or copy actions.

## Implementation Scope

The implementation slice should include:

- Remove sticky quota-ratio first-bind behavior from `sticky-session`.
- Keep full candidate sets through A and B; stop selection-time retry/fallback
  queue preparation.
- Introduce or refactor toward explicit `CandidateSet`, `PrimaryDecision`,
  `AttemptTarget`, `AttemptFailure`, `FallbackDecision`, `AttemptResult`, and
  `RoutingTrace` structures.
- Make outbound execution consume concrete `AttemptTarget` credentials.
- Mark provider/auth paths that cannot honor request-scoped credentials as not
  supporting same-channel credential fallback.
- Implement D classification with the required failure classes and the
  three-way `model not found` split.
- Implement E recovery mapping and priority exhaustion rules.
- Refresh sticky binding only after success and only to the final successful
  `AttemptTarget`.
- Preserve attempt/fallback history in request execution diagnostics.
- Remove provider quota from routing decisions, durable snapshots, and normal UI
  surfaces.
- Clean request list/detail labels so debug identifiers are not primary UI text.

## Implementation Inventory

Before changing code, inventory these areas and update the implementation plan:

- `internal/server/orchestrator/select_candidates.go`
- `internal/server/orchestrator/candidates.go`
- `internal/server/orchestrator/sticky_session.go`
- `internal/server/orchestrator/outbound.go`
- provider/auth adapters that currently bind credentials during construction;
- request execution and usage log snapshot fields;
- GraphQL/REST fields exposing provider quota or debug credential identifiers;
- frontend request list/detail and credential/channel list display fields.

This inventory is not an architecture open question. It decides file-level work
order and compatibility handling.

## Acceptance Criteria

- `sticky-session` is a pure sticky strategy and does not run quota-ratio
  first-bind balancing.
- Candidate selection keeps the full feasible candidate set and does not
  precompile fallback queues.
- Primary decision chooses only the first attempt and is explainable.
- Every outbound attempt uses one concrete `AttemptTarget`.
- Outbound execution does not silently reselect a credential after
  `AttemptTarget` construction.
- Same-channel credential fallback runs only for request-scoped credential-aware
  provider/auth paths.
- `model not found` is classified as `selection_modeling_gap`,
  `runtime_capability_drift`, or `request_invalid` based on local feasibility.
- `request_invalid` and `selection_modeling_gap` return visible errors instead
  of ordinary fallback success.
- `runtime_capability_drift` does not retry the same target and may fallback only
  to remaining A-feasible targets before output starts.
- Credential auth/quota failures prefer same-channel credential fallback when
  supported.
- Lower-priority fallback starts only after current-priority exhaustion.
- Silent fallback is blocked after user-visible streaming output starts.
- Sticky binding refreshes only after success and only to the final successful
  concrete `AttemptTarget`.
- Failed attempts and fallback decisions remain visible after final success.
- Provider quota is not used for routing, not persisted as product state, and not
  shown as normal UI state.
- Request detail exposes structured A-G diagnostics.
- Request list uses operator-facing labels and does not display fingerprints,
  `secret:v1:*`, raw refs, internal resource keys, or raw secrets as primary UI
  labels.
- Copy/export/log/API paths preserve credential masking.

## Validation Plan

Add or update tests for:

- sticky binding writes only after success;
- sticky first-bind does not run quota-ratio balancing;
- selection keeps full candidate sets independent of retry budget;
- primary decision does not pre-plan fallback;
- `AttemptTarget` includes channel, model/API format, and credential;
- request-scoped credential fallback supported vs unsupported provider/auth
  paths;
- D classification matrix, including three `model not found` cases;
- E recovery matrix and terminal conditions;
- priority exhaustion before lower-priority fallback;
- no silent fallback after streaming output starts;
- fallback success preserving failed attempt history;
- provider quota removal from routing/UI/snapshots;
- request list/detail masking and copy/export masking.

Do not run lint/build unless explicitly requested by the user.

## Definition of Done

- PRD and backend routing specs describe the same A-G contract.
- No architecture open question remains that blocks implementation.
- Implementation inventory is complete before code edits begin.
- Code changes follow the A-G contract.
- Tests or targeted verification cover the acceptance criteria touched by the
  implementation slice.

## Reference Specs

- `.trellis/spec/backend/routing-guidelines.md`
- `.trellis/spec/backend/credential-routing-model.md`
- `.trellis/spec/backend/quality-guidelines.md`
