# Sticky Session Circuit Breaker Decoupling - Technical Design

## Current Routing Model

The orchestrator currently derives one runtime strategy:

```go
strategy := deriveLoadBalancerStrategy(retryPolicy, apiKey)
```

Then it selects a base load balancer:

- `adaptive` -> `adaptiveLoadBalancer`
- `failover` -> `failoverLoadBalancer`
- `circuit-breaker` -> `circuitBreakerLoadBalancer`
- `sticky-session` -> `failoverLoadBalancer`

Sticky-session then reorders candidates in `orderCandidates(...)` through `StickySessionRouter`.

## Current Coupling To Remove

### 1. Raw-request circuit-breaker middleware

Current behavior:

```go
withModelCircuitBreaker(outbound, processor.modelCircuitBreaker, strategy)
```

`modelCircuitBreakerTracker.OnOutboundRawRequest` is active when strategy is either:

- `circuit-breaker`
- `sticky-session`

When the current channel/model is open and probe is not allowed, it returns:

```go
errSkipCandidateByCircuitBreaker
```

The pipeline wraps that as:

```text
failed to apply raw request middlewares: skip candidate by circuit breaker
```

Required behavior:

- The middleware may still be installed globally, but it must no-op for `sticky-session`.
- Prefer changing the guard to only activate for `LoadBalancerStrategyCircuitBreaker`.
- Do not change circuit-breaker strategy behavior in this task.

### 2. Sticky router open-circuit filtering

Current behavior:

```go
stickySessionRouter := NewStickySessionRouter(
    stickySessionStore,
    NewDefaultStickyKeyExtractor(),
    modelCircuitBreaker,
)
```

`StickySessionRouter.eligibleCandidates` calls `stickyCircuitOpen(...)` and excludes open-circuit candidates.

Required behavior:

- `StickySessionRouter` should not own a `modelCircuitBreaker` field.
- `NewStickySessionRouter` should only receive the binding store and sticky key extractor.
- `eligibleCandidates` should depend on base load-balancer sticky eligibility only:

```go
req.LoadBalancer.IsStickyPrimaryEligible(...)
```

- Delete or stop using `stickyCircuitOpen` and `stickyRequestedModel` if they become dead code.

## Desired Runtime Flow

### Sticky-session strategy

```text
inbound request
-> select eligible candidates
-> orderCandidates invokes StickySessionRouter
-> StickySessionRouter extracts stickyKey
-> if no key: use base load-balancer ordering
-> if binding exists and candidate is eligible in current tier: place it first
-> send request normally
-> upstream failure: existing retry/fallback decides
-> upstream success: sticky binding refreshes to successful target
```

There is no circuit-breaker hard skip or probe lease in this flow.

### Circuit-breaker strategy

```text
inbound request
-> circuit-breaker load-balancer strategy participates in candidate ordering
-> raw-request middleware can skip open candidates unless probe is allowed
-> success/error updates model circuit-breaker state
```

This flow remains owned by `LoadBalancerStrategyCircuitBreaker`.

## Implementation Plan

1. Update `internal/server/orchestrator/model_circuit_breaker.go`.
   - Change `OnOutboundRawRequest` strategy guard so only `circuit-breaker` can skip/probe.
   - Consider whether success/error recording should also be gated to `circuit-breaker`.
   - Recommendation: record only for `circuit-breaker` in this task, because sticky-session should not mutate model circuit state.

2. Update `internal/server/orchestrator/orchestrator.go`.
   - Instantiate sticky router without passing `modelCircuitBreaker`.

3. Update `internal/server/orchestrator/sticky_session.go`.
   - Remove `modelCircuitBreaker` field from `StickySessionRouter`.
   - Remove variadic constructor argument.
   - Remove open-circuit filtering from `eligibleCandidates`.
   - Remove dead helpers if unused.

4. Update tests.
   - Existing `TestStickySessionRouter_SkipsBoundPrimaryWhenCircuitOpen` should be replaced or inverted.
   - New expected behavior: even if the shared model circuit breaker is open, sticky-session ordering does not skip a bound eligible candidate.
   - Add middleware-level test proving `withModelCircuitBreaker(..., sticky-session)` does not return `errSkipCandidateByCircuitBreaker`.
   - Keep circuit-breaker middleware tests proving `circuit-breaker` still skips when open and probe is not allowed.

## Test Plan

Targeted tests:

- `go test ./internal/server/orchestrator -run 'StickySession|CircuitBreaker'`

Broader backend tests if the targeted tests pass:

- `go test ./internal/server/orchestrator ./internal/server/biz`

Do not run frontend tests for this task unless UI text is changed.

## Risk Notes

- If success/error recording remains active for sticky-session while hard skip is disabled, sticky traffic can still mutate the model circuit-breaker state used later by explicit `circuit-breaker` strategy. That is still a coupling, just less visible. Prefer disabling both skip/probe and state mutation for sticky-session.
- Disabling sticky-session circuit filtering means a previously open candidate can be tried. This is intentional: sticky-session delegates failure handling to retry/fallback.
- If a deployment relies on sticky-session to avoid obviously broken channels via circuit-breaker state, that behavior should move to normal eligibility, retry, quota, channel disabled state, or explicit circuit-breaker strategy.

## Code Pointers

- `internal/server/orchestrator/orchestrator.go`
  - Sticky router construction.
  - Middleware list includes `withModelCircuitBreaker(...)`.
- `internal/server/orchestrator/model_circuit_breaker.go`
  - Raw request skip/probe.
  - Success/error recording.
- `internal/server/orchestrator/sticky_session.go`
  - Sticky router constructor and candidate eligibility filtering.
- `internal/server/orchestrator/sticky_session_test.go`
  - Existing sticky-router behavior tests.
- `internal/server/orchestrator/outbound.go`
  - `errSkipCandidateByCircuitBreaker`.
  - Retry behavior for local skip.
