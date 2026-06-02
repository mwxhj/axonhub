# credential-aware sticky fallback

## Goal

Implement credential-aware sticky fallback for backend routing so recovery depends primarily on fallback rather than repeated same-target retry. When a sticky target fails, the router should first escape locally within the same channel/priority using credential-aware exclusions, then degrade across channels and priorities in a controlled way, while preserving observability of every failed attempt.

## What I already know

* Current sticky binding already stores `channel + credential` identity in `StickySessionTarget`.
* Current retry/fallback execution still runs on a channel candidate list and `NextChannel()` is basically `CurrentCandidateIndex++`.
* Current `ChannelModelsCandidate` is channel-scoped, not credential-scoped.
* Actual API key / upstream credential selection happens later in outbound transformation through `TraceStickyKeyProvider`.
* Existing routing already has context fields for:
  * preferred credential
  * candidate-scoped allowed credentials
  * sticky seed
  * selected credential metadata snapshots
* Project routing rules now explicitly state:
  * fallback must not hide failures
  * no hidden fallback time budget
  * same-target retry should stay minimal
  * recovery should primarily depend on fallback

## Assumptions

* We will not add a frontend toggle for old vs new fallback behavior.
* We may keep a backend-only rollout/config switch only if implementation risk requires it, but the target behavior is a single routing model.
* MVP should avoid a full `channel + credential` candidate explosion unless implementation proves it is required.
* Request timeout / cancellation remains the only time boundary; this task should not add fallback-specific time budgets.

## Requirements

* Distinguish clearly between:
  * same-target retry
  * fallback to another credential in the same channel
  * fallback to another channel in the same priority tier
  * fallback to lower-priority tiers
* Keep same-target retry minimal and error-classification-based.
* Add per-request exclusion tracking so a failed credential can be skipped on subsequent attempts.
* Prefer same-channel credential fallback before cross-channel fallback when the failure is credential-scoped and another eligible credential exists.
* Do not cross to a lower priority tier until the current priority tier is exhausted.
* Preserve sticky success rebinding to the final successful `channel + credential`.
* Keep failed attempts observable in request execution / logs / metrics instead of only exposing the final success.
* Do not perform silent fallback after user-visible streaming output has started.

## Acceptance Criteria

* [x] A sticky-bound credential failure can escape to another eligible credential on the same channel without repeatedly reselecting the failed credential.
* [x] A credential-scoped failure does not immediately jump to another priority tier while the current tier still has eligible targets.
* [x] Same-target retry is reduced to a narrow, classification-based path instead of being the main recovery mechanism.
* [x] When fallback succeeds, sticky binding refreshes to the successful target and failed attempts remain visible in execution history.
* [x] No fallback-specific time budget is introduced.
* [x] Streaming requests do not silently switch upstream targets after user-visible output has started.

## Definition of Done

* [x] Routing implementation updated.
* [x] Relevant backend spec/routing docs remain aligned with implementation.
* [x] Focused backend tests added or updated for retry/fallback behavior.
* [x] No frontend toggle added for this routing behavior.

## Implementation Notes

* Added request-scoped credential exclusions and passed them into the channel API key provider.
* Added pipeline-level target fallback so the orchestrator can switch execution target with access to the triggering error.
* Credential-scoped failures exclude the current credential and prefer same-channel API-key fallback before moving to another channel.
* Cross-channel fallback now searches the current priority tier first, then lower-priority candidates after the current tier is exhausted.
* Same-target retry is skipped for credential-scoped errors and for retryable errors when another fallback target exists; empty-response detection may still use same-target retry as the narrow transient path.
* The MVP covers API-key multi-credential fallback. OAuth transformer construction still selects OAuth material at channel build time, so request-level OAuth credential fallback remains a future migration item.

## Verification

* `go test ./internal/server/biz`
* `go test ./internal/server/orchestrator`
* `cd llm && go test ./pipeline`

## Out of Scope

* Full frontend configuration UI for fallback policy.
* Large routing-architecture rewrite that explodes candidates into every `channel + credential` combination unless strictly required.
* New health-score system or adaptive hidden timing heuristics.
* Changes to the already-started local quota column UI task beyond keeping work compatible.

## Technical Notes

* Likely implementation areas:
  * `internal/server/orchestrator/outbound.go`
  * `internal/server/orchestrator/sticky_session.go`
  * `internal/server/orchestrator/retry.go`
  * `internal/server/orchestrator/state.go`
  * `internal/server/biz/channel_apikey_provider.go`
  * possibly candidate/channel helper code to narrow credential views for fallback
* Existing spec source:
  * `.trellis/spec/backend/routing-guidelines.md`
  * `.trellis/spec/backend/credential-routing-model.md`
