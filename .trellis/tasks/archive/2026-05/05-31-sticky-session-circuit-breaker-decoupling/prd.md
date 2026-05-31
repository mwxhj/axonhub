# Sticky Session Circuit Breaker Decoupling

## Goal

Decouple `sticky-session` routing from model circuit-breaker execution control.

Sticky-session should preserve upstream cache locality by routing a stable sticky identity back to the last successful execution target for a short TTL. It must not open, probe, or hard-skip candidates through the model circuit-breaker raw-request middleware. Failures under sticky-session should escape through the existing retry/fallback pipeline and return real upstream or retry errors.

## Problem

The current implementation still couples sticky-session to model circuit-breaker behavior in two places:

- `withModelCircuitBreaker(..., strategy)` runs for both `circuit-breaker` and `sticky-session`.
- `StickySessionRouter` receives `modelCircuitBreaker` and filters open-circuit candidates before ordering.

This can make sticky-session requests fail locally with:

```text
failed to apply raw request middlewares: skip candidate by circuit breaker
```

That error is not an upstream failure and can be exposed as an HTTP 500. It also violates the sticky-session contract: sticky-session should not be a health-management strategy.

## What I Already Know

- The global load-balancer strategy can be `adaptive`, `failover`, `circuit-breaker`, or `sticky-session`.
- For sticky-session, `Process` currently chooses `processor.failoverLoadBalancer` as the base load balancer.
- `withModelCircuitBreaker` currently checks:
  - `strategy != circuit-breaker && strategy != sticky-session` -> no-op
  - therefore sticky-session still participates in raw-request open-circuit skip/probe behavior.
- `PersistentOutboundTransformer.CanRetry` returns `false` for `errSkipCandidateByCircuitBreaker`, so this local skip can force channel switching and can become the final error if no candidates remain.
- `StickySessionRouter.eligibleCandidates` also skips candidates when `stickyCircuitOpen(...)` returns true.
- Backend routing spec now states: sticky-session may use candidate eligibility, but must not expose circuit-breaker raw skip as HTTP 500 and must not manage model circuit state.

## Requirements

### Functional

- `sticky-session` must not install active model circuit-breaker skip/probe behavior.
- `withModelCircuitBreaker` must only hard-skip/probe candidates for `circuit-breaker` strategy.
- Sticky-session ordering must not call `ModelCircuitBreaker.GetModelCircuitBreakerStats` to exclude candidates.
- A channel/model that is open in the model circuit breaker must still be executable under sticky-session if normal eligibility allows it.
- If that execution fails, the existing retry/fallback pipeline handles same-channel retry or channel switching.
- Sticky-session binding writes still happen only after successful upstream completion.
- Existing `circuit-breaker` strategy behavior must remain unchanged.

### Error Behavior

- Under sticky-session, local `errSkipCandidateByCircuitBreaker` must not be produced by the model circuit-breaker middleware.
- Under sticky-session, users should see real upstream/retry errors when all attempts fail.
- Under circuit-breaker strategy, open-circuit skip/probe behavior remains valid and may still produce the existing skip error where currently expected.

### Observability

- Logs should make it clear when sticky-session chooses a binding target or falls back to normal load balancing.
- Do not add noisy logs for circuit-breaker state under sticky-session after decoupling.

## Acceptance Criteria

- [ ] `withModelCircuitBreaker` is inactive for `sticky-session`.
- [ ] `StickySessionRouter` no longer receives or consults `ModelCircuitBreaker`.
- [ ] Sticky-session requests do not return `failed to apply raw request middlewares: skip candidate by circuit breaker`.
- [ ] Circuit-breaker strategy tests still pass with existing open/probe behavior.
- [ ] Sticky-session tests cover an open circuit-breaker state and prove it does not remove the sticky candidate.
- [ ] Retry/fallback still handles network errors, 5xx, empty responses, retryable 429, and queue errors.

## Out of Scope

- Changing circuit-breaker strategy semantics.
- Adding a new sticky cooldown/suspension system.
- Persisting sticky bindings across process restart or across multiple pods.
- Reworking first unbound sticky candidate selection.
- Changing Responses API reconstruction/fallback behavior.
- Changing credential quota or upstream credential management UI.

## Definition of Done

- Backend code updated with a narrow diff.
- Sticky-session and circuit-breaker tests updated.
- Relevant backend tests run.
- Routing spec updated if the implemented behavior differs from the current spec.
- Commit created after implementation and verification.
