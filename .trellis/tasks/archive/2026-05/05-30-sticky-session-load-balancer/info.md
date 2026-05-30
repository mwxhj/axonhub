# Sticky Session Design

## Intent

`sticky-session` is a model-cache locality strategy. It should route requests that share the same cacheable prompt prefix back to the channel that last served that prefix successfully within a short cache window.

The sticky key is not a client-provided session ID. It is an internal cache-prefix identity derived from server scope and stable request material.

## Routing Model

Candidate discovery remains unchanged:

1. Resolve model/channel candidates through the existing selector chain.
2. Apply project/API-key profile filters, channel tags, native-tools filters, stream policy, and quota filters.
3. Produce `ChannelModelsCandidate` values with `Channel`, `Priority`, `Models`, and `APIFormat`.

Priority semantics:

* `ChannelModelsCandidate.Priority` comes from model associations. Lower value is higher priority.
* `Channel.OrderingWeight` is used operationally as channel priority/weight. Higher value is preferred within an association-priority tier.

Sticky routing rules:

1. If no valid sticky key can be extracted, use the ordinary non-sticky ordering for this strategy without writing a binding.
2. If a valid binding exists and the bound channel is still in the full legal candidate set, prefer it even if a higher-priority or higher-weight channel has recovered.
3. If no valid binding exists, choose randomly only within the best current tier:
   * lowest model association priority value
   * highest `OrderingWeight` inside that association priority
4. Fill retry candidates using the existing priority order after the preferred candidate.
5. A successful retry/fallback channel immediately becomes the binding, even if it is lower priority or lower weight.
6. The binding TTL is 5 minutes. After expiry, the next unbound request returns to the best current tier and random first selection.

This means a fallback channel can be sticky for up to 5 minutes after it successfully served the cache context. That is intentional: it preserves the actual cache location instead of pulling the next request back to a recovered higher-priority channel too early.

## Binding Store

MVP store:

```text
stickyKey -> {
  channelID,
  expiresAt
}
```

Rules:

* In-memory only.
* TTL: 5 minutes.
* Success refreshes TTL and writes the successful channel.
* Complete failure leaves the previous binding unchanged.
* No cooldown/suspension state.
* No per-key lock or queue.
* Concurrent requests with the same sticky key are best-effort; latest successful write wins.

Rationale for no cooldown:

The sticky key should already represent one cache/session context. If duplicate-key requests happen concurrently, they should prefer the same channel when a binding exists. Adding per-key cooldown/serialization would turn the feature into a session coordinator, which is out of scope for a load-balancing strategy.

## Failure Behavior

Existing pipeline retry order remains authoritative:

1. Same-channel retry first when `CanRetry` allows it and `MaxSingleChannelRetries` is not exhausted.
2. Channel switch only after same-channel retry is not possible or exhausted.
3. When a switched fallback channel succeeds, write `stickyKey -> fallbackChannel`.
4. When all attempts fail, keep the previous binding unchanged.

The strategy should not force an early channel switch.

## Sticky Key Extractor

Extractor API:

```go
type StickyKeyExtraction struct {
    Key    string
    OK     bool
    Reason string
}
```

There is no confidence score. A key is either suitable for cache locality or not.

### Key Meaning

The key should identify a reusable model-cache prefix, not an arbitrary chat turn. If two requests produce the same sticky key, the design assumes they share the same cache context and should route consistently.

### Strong Inputs

Use these when present:

* Server scope:
  * API key ID or project/profile scope.
  * Active API-key profile / project profile identity when available.
* Model/request scope:
  * requested model after mapping if available; otherwise request model
  * request type
  * inbound API format
* Explicit cache/session hints:
  * `PreviousResponseID`
  * `PromptCacheKey`
* Stable prompt prefix:
  * system messages
  * developer messages
  * tools schema and tool names
  * response format
  * tool choice / parallel tool call shape when it changes prompt/tool behavior
  * early stable conversation history before the latest user turn

Hash the canonicalized payload; do not store raw prompts in the binding map.

### Inputs To Avoid

Do not use these as primary key material:

* Only the last user message.
* The full request body, because every turn would produce a new key.
* Only API key plus model, because it is too broad and will merge unrelated cache contexts.
* Sampling-only parameters such as temperature, top_p, max_tokens, or max_completion_tokens.
* Assistant/tool output from late conversation turns unless it is part of an explicit early prefix window.

### `ok=false` Cases

Return `OK=false` when there is not enough stable cache-prefix material, for example:

* a single short user message with no system/developer prompt
* no tools
* no previous response ID
* no prompt cache key
* no meaningful prior context

In these cases sticky-session should not create or refresh a binding.

### Canonicalization

Canonicalization should be deterministic:

* Include field names and version marker in the payload.
* Normalize role names.
* Preserve message order.
* For JSON schemas and response formats, use stable JSON encoding or normalized raw JSON.
* For tools, sort only where the API semantics are order-insensitive; otherwise preserve order.
* Hash with a stable hash suitable for internal keys, for example SHA-256 truncated to a practical string length.

## Placement Concern

The sticky key should reflect the final prompt prefix that reaches the provider as much as practical. The existing pipeline currently selects candidates before prompt injection/protection. Implementation should avoid extracting the key before server-injected system/developer prompt material is visible.

Acceptable implementation directions:

* Move sticky ordering later, after prompt injection/protection, while keeping candidate discovery before it.
* Or store the candidate set first, then apply sticky ordering once `llm.Request` contains injected prompt material.

The implementation should not base the sticky key only on the raw client request if server prompt injection affects provider cache locality.

## Out Of Scope

* Client-provided sticky keys.
* Persistent binding storage.
* Per-key locking or request serialization.
* Cross-process coordination.
* A full session system.
