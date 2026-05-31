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
