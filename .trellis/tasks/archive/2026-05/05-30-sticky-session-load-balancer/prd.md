# Sticky Session Load Balancing Strategy

## Goal

Add a standalone `sticky-session` load-balancing strategy that routes model-cache-compatible request contexts back to the channel that most recently served that cache context successfully. The strategy must not mix behavior into the existing `adaptive`, `failover`, or `circuit-breaker` strategies. Sticky keys are internal cache-prefix identities, not client-provided session IDs.

## What I Already Know

* The strategy must be independent and selectable by the existing load-balance strategy mechanism.
* Sticky binding is `stickyKey -> channel`, not request-level routing.
* The client must not provide a sticky key and no sticky key should be exposed externally.
* The service generates the sticky key from stable request context.
* Key extraction returns `key, ok, reason`; there is no key confidence score.
* If a suitable sticky key cannot be generated, the request falls back to ordinary load balancing.
* Existing candidate selection already groups by model association priority before calling `LoadBalancer.Sort`; lower association priority values are higher priority.
* Channel `OrderingWeight` is used operationally as channel priority/weight; higher values are preferred within a model association priority tier.
* Existing retry behavior consumes an ordered candidate list and moves to the next channel on retry.
* Existing circuit-breaker state is model-aware and can skip open candidates at outbound time for the `circuit-breaker` strategy.

## Requirements

* Add `sticky-session` as a separate load-balancer strategy value.
* Expose `sticky-session` in the existing frontend strategy selectors and localized descriptions.
* Use an in-memory TTL binding store for the first implementation. Bindings are soft state and may be lost on process restart.
* Sticky binding TTL is 5 minutes because the upstream endpoint cache this strategy targets is only useful within that window.
* Do not implement sticky cooldown/suspension in the MVP.
* A sticky key represents a cache/session context. If multiple requests produce the same key, they should route consistently to the same binding when available.
* Concurrent requests for the same sticky key are best-effort and not serialized; the latest successful binding write wins.
* Preserve priority behavior for new/unbound selection: when no binding exists, sticky-session must choose only from the best currently available model-association-priority / channel-weight tier.
* Existing 5-minute bindings may temporarily replay the last successful fallback channel even if a higher-priority or higher-weight channel later recovers; TTL bounds this cache-locality override.
* Retry fallback candidates may continue to be filled from lower-priority groups according to the existing priority order when the highest-priority group does not satisfy the retry candidate count.
* Generate sticky keys internally as cache-prefix identities from stable request context. See `info.md` for the extractor contract.
* The extractor must return `key, ok, reason`; insufficient request material returns `ok=false`.
* For `ok=false`, route through ordinary load balancing without sticky binding.
* For a valid sticky key, check the soft binding store for `stickyKey -> channelID`.
* If the bound channel is still in the current full candidate set and eligible, rank it first.
* If there is no valid binding, choose one channel randomly within the best currently available model-association-priority / channel-weight tier and bind only after a successful request.
* First binding should not apply a second deterministic score based on `stickyKey + channelID`; priority/weight has already selected the eligible tier.
* A binding that points to a channel no longer in the full legal candidate set must be ignored and may be deleted or overwritten after fallback success.
* Health and availability outrank stickiness:
  * Quota-exhausted channels filtered before load balancing remain unavailable.
  * Rate-limited or otherwise ineligible bound channels are skipped for the current request.
  * Circuit-breaker skip behavior must not be weakened by sticky routing.
* Request success should refresh or create the sticky binding for the successful channel.
* Temporary failure must respect the existing retry order: same-channel retry first when `CanRetry` allows it and `MaxSingleChannelRetries` is not exhausted, then channel switching.
* If retry escapes to another candidate and that fallback succeeds, migrate the binding to the successful fallback channel immediately.
* Lower-priority or lower-weight fallback success may become the temporary binding; the 5-minute TTL naturally returns future unbound selection to higher-priority / higher-weight channels after expiry.
* If all retry/fallback attempts fail, keep the previous binding unchanged because no new successful cache location exists.

## Acceptance Criteria

* [ ] `sticky-session` can be configured wherever load-balance strategy is currently selected.
* [ ] Unbound sticky selection never chooses a lower model-association-priority / lower channel-weight candidate while better-tier candidates are available.
* [ ] Existing successful fallback bindings can temporarily prefer the bound fallback channel until the 5-minute TTL expires.
* [ ] Requests with insufficient stable context fall back to the normal strategy path.
* [ ] Repeated requests with the same generated sticky key prefer the same bound channel when it remains available.
* [ ] First selection for an unbound key is random within the best current model-association-priority / channel-weight tier, then becomes sticky after success writes the binding.
* [ ] A binding to a removed, disabled, model-incompatible, quota-exhausted, or otherwise absent channel is ignored.
* [ ] A bound channel that fails transiently still gets existing same-channel retries before sticky-session causes or records channel escape.
* [ ] A successful fallback channel replaces the binding immediately, even if it is lower priority or lower weight, and the binding expires after 5 minutes unless refreshed.
* [ ] Unit tests cover key extraction, first-bind randomization, priority/weight isolation, fallback without key, binding refresh, stale binding, fallback migration, and best-effort duplicate-key behavior.

## Definition of Done

* Focused Go tests added/updated for sticky-session behavior.
* Relevant frontend labels/options updated if the existing strategy selector is part of the configuration surface touched by this strategy.
* Lint/typecheck/test commands run only when requested or allowed by project workflow at implementation/check stage.
* Behavior does not change for `adaptive`, `failover`, or `circuit-breaker` unless the selected strategy is `sticky-session`.

## Technical Approach

Use a new sticky-specific load-balancer path rather than adding a normal `LoadBalanceStrategy` scorer. The existing scorer interface only sees `channel`, while sticky selection needs the full candidate set, `llm.Request`, binding state, and result feedback. The clean integration point is `LoadBalancedSelector.Select`, before the normal required-count truncation, because an existing 5-minute binding may point at a previously successful fallback candidate outside the best current priority/weight tier.

The sticky strategy should:

1. Build the same priority-ordered candidate view the existing selector uses.
2. Extract `stickyKey` from the unified `llm.Request` plus server-side scope.
3. Fall back to the ordinary load balancer if extraction returns `ok=false`.
4. Prefer an existing valid binding when the bound channel is still in the full candidate set and eligible.
5. Otherwise randomly choose a candidate in the best current model-association-priority / channel-weight tier for the first binding attempt.
6. Put same-tier fallback candidates after the preferred channel first.
7. If the retry policy needs more candidates than the highest-priority group can provide, let the existing priority loop append candidates from lower-priority groups in order.
8. On retryable failure, let the existing pipeline attempt same-channel retry first when allowed.
9. On success, bind the sticky key to the actual successful channel selected by the retry chain and refresh the 5-minute TTL.
10. On complete failure, leave the previous binding unchanged.

## Decision (ADR-lite)

**Context**: The existing load balancer composes per-channel scorers, but sticky session selection is key-centric and needs request-level context plus mutable soft bindings.

**Decision**: Implement `sticky-session` as a distinct load-balancer selection mode with its own extractor, in-memory binding state, and random first binding inside the best current model-association-priority / channel-weight tier. Reuse existing candidate filtering, priority grouping, retry ordering, rate-limit/quota filtering, and circuit-breaker health checks where applicable.

**Consequences**: This keeps adaptive/failover/circuit-breaker behavior isolated. The MVP avoids introducing a durable session system. If restart-persistent sticky routing becomes necessary later, the binding store can be swapped behind a small interface.

## Open Questions

* None.

## Out of Scope

* Client-provided sticky key headers, fields, query params, or API changes.
* Unbound sticky selection that skips the best currently available model-association-priority / channel-weight tier.
* A full durable conversation/session database.
* Persisting sticky bindings across process restarts.
* Changing the semantics of existing `adaptive`, `failover`, or `circuit-breaker` strategies.
* Using only the last user message as the sticky key source.

## Technical Notes

* Main selection files inspected:
  * `internal/server/orchestrator/candidates.go`
  * `internal/server/orchestrator/load_balancer.go`
  * `internal/server/orchestrator/orchestrator.go`
  * `internal/server/orchestrator/select_candidates.go`
  * `internal/server/orchestrator/outbound.go`
  * `internal/server/orchestrator/model_circuit_breaker.go`
* Strategy constants and retry policy live in `internal/server/biz/system.go`.
* Frontend strategy selectors currently live in:
  * `frontend/src/features/system/components/retry-settings.tsx`
  * `frontend/src/features/apikeys/components/apikeys-create-template-dialog.tsx`
  * `frontend/src/features/apikeys/components/apikeys-edit-template-dialog.tsx`
  * `frontend/src/features/apikeys/components/apikeys-profiles-dialog.tsx`
  * `frontend/src/locales/en/system.json`
  * `frontend/src/locales/zh-CN/system.json`
* Unified request fields for sticky extraction are in `llm/model.go`.
* Scope decision: keep both backend support and frontend configuration entry in this task; do not add durable persistence.
* Routing decision: new/unbound sticky-session primary selection starts in the best current model-association-priority / channel-weight tier, but existing 5-minute bindings and retry fallback can temporarily use lower-priority/lower-weight candidates after a successful fallback.
* Detailed implementation design is recorded in `info.md`.
