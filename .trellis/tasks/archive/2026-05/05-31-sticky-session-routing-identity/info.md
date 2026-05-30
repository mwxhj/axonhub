# Sticky Session Routing Identity Technical Design

## Root Cause

The current sticky-session implementation hashes a canonical request payload that includes request scope and a stable-prefix message window. That made the implementation easy to integrate with the existing load-balancer, but it selected the wrong abstraction.

The sticky binding should represent:

```text
stable routing identity -> channel
```

The current design often represents:

```text
current content prefix fingerprint -> channel
```

Those are not equivalent. A transcript prefix grows every turn, so a direct hash of the current prefix can drift. Sticky-session needs a resolver that can recognize the same conversation or response chain even when the current request contains more context than the last one.

## Proposed Routing Model

Introduce a sticky identity resolver that returns ordered lookup keys, not one fixed key:

```go
type StickyLookup struct {
    Key      string
    Kind     string
    Strength int
    Reason   string
}

type StickyExtraction struct {
    Lookups []StickyLookup
    Bindings []StickyLookup
    OK bool
    Reason string
}
```

The selection path should try lookup keys in priority order:

1. Protocol session keys.
2. Responses `previous_response_id` key.
3. Explicit prompt cache key.
4. Transcript prefix keys from longest to shortest.

The successful-response binding path should write binding keys appropriate for the completed request:

* Protocol session key, if present.
* Returned Responses `response.id`, if present.
* Previous response ID refresh, if present.
* Explicit prompt cache key, if present.
* Completed transcript prefix hash, if available and bounded.

The exact structs can differ during implementation, but the important design change is that routing can perform multiple ordered lookups and successful responses can bind multiple aliases to the same channel.

## Identity Strength

Sticky identities should not be treated as equal. Suggested strength order:

```text
protocol session id > responses chain id > explicit prompt_cache_key > exact transcript prefix
```

The router should prefer the strongest available eligible hit. Transcript-prefix hits should never override a valid protocol session or Responses chain hit.

## Store Model

Current store:

```text
stickyKey -> {channelID, expiresAt}
```

Next store can remain a map, but keys should encode the identity type:

```text
session:v1:<scopeHash>:<sessionHash> -> channel
responses:v1:<scopeHash>:<responseIDHash> -> channel
prompt-cache:v1:<scopeHash>:<promptCacheKeyHash> -> channel
prefix:v1:<scopeHash>:<prefixHash> -> channel
```

Each binding still has:

```text
channelID
expiresAt
updatedAt
kind
prefixLength/messageCount if relevant
```

TTL remains 5 minutes for MVP.

## Responses API Chain

Request lookup:

```text
if previous_response_id exists:
  lookup responses:<scope>:hash(previous_response_id)
```

Successful response binding:

```text
if response.id exists:
  bind responses:<scope>:hash(response.id) -> currentChannel

if response.previous_response_id exists:
  refresh responses:<scope>:hash(response.previous_response_id) -> currentChannel
```

Important behavior:

* This lookup must run before transcript-prefix fallback.
* If the bound channel is eligible, it should be primary.
* If the bound channel is not eligible, existing retry/fallback can continue, but logs should make the escape visible.
* A fallback success may bind the new response ID to the fallback channel, but this is a semantic migration of the response chain and should be tested carefully.

Potential migration policies:

1. **Immediate migration**: fallback success moves all relevant aliases to the fallback channel. Simple, but can hide transient failure and fragment an upstream response chain.
2. **New-ID-only migration**: fallback success binds only the new response ID to the fallback channel, leaving previous IDs untouched until TTL expiry. More precise, but requires careful alias handling.
3. **Conservative migration**: fallback success does not migrate chain aliases unless the original channel is ineligible, circuit-open, quota-exhausted, or repeatedly failing. Best preserves chain locality, but needs failure state.

Chosen MVP: active-success migration.

If a fallback channel successfully completes a Responses request, bind the returned `response.id` to that fallback channel and refresh active session/prompt/prefix aliases to that fallback channel. If the request used `previous_response_id` and the fallback channel accepted it, refreshing that previous response ID alias to the fallback channel is acceptable because the channel proved it can continue from that ID and now has the freshest cache. Do not migrate aliases on failed attempts or incomplete streams.

This keeps the active conversation on the channel that most recently created useful cache/state. Older response IDs that were not involved in the successful request do not need to be rewritten.

## Transcript Prefix Index

The prefix index should solve the drift problem without scanning all stored bindings.

On successful response, store hashes for bounded completed prefixes. A practical MVP:

* Canonicalize the transcript after response transformation when enough data is available.
* Store only a small set of suffix-completed prefixes, such as:
  * full completed transcript
  * transcript without volatile trailing tool artifacts if applicable
* Or store a bounded rolling set per request, capped by max message count and max total bytes.

On request lookup:

* Canonicalize the incoming transcript.
* Generate prefix hashes for the current request.
* Query the map from longest to shortest.
* Stop at the first eligible binding.

Avoid:

* Fuzzy matching.
* Embedding similarity.
* Comparing against every stored key.
* Hashing the latest user message as the only identity.

The prefix index should be considered a best-effort cache locality feature, not a correctness guarantee. If a protocol/session/chain key exists, it should win.

## Canonicalization Rules

Scope should include:

* API key ID / project ID / active profile
* model
* API format and client format
* tools schema hash
* response format hash
* tool choice hash

Message canonicalization should:

* Normalize roles.
* Trim insignificant surrounding whitespace.
* Include message names where meaningful.
* Hash large binary/URL/data fields instead of storing raw data.
* Bound content size per message.
* Treat protocol-specific tool result structures carefully so they do not become the primary session identity.

Canonicalized keys must avoid raw sensitive payload storage. Store hashes and bounded normalized fragments only where necessary for deterministic hashing.

## Current Files To Revisit

* `internal/server/orchestrator/sticky_session.go`
  * Replace single-key extractor with multi-lookup/multi-bind resolver.
  * Add Responses API response ID binding support.
  * Replace early-window primary strategy with protocol/session/chain/prefix lookup hierarchy.
  * Move or extend binding logic if `response.id` is only available at `OnOutboundLlmResponse`, not `OnInboundRawResponse`.
* `internal/server/orchestrator/state.go`
  * May need to store lookup/binding aliases and response IDs for binding middleware.
* `internal/server/orchestrator/select_candidates.go`
  * Keep strategy gate and candidate ordering path.
* `llm/model.go`
  * Confirm unified request/response fields expose `PreviousResponseID` and response `ID`.
* `llm/transformer/openai/responses/*`
  * Confirm response IDs are available before sticky binding runs for stream and non-stream.

## Pipeline Hook Concern

Current sticky binding happens in `OnInboundRawResponse`, which runs after unified response conversion back to the client-facing HTTP response. That hook can refresh the current channel, but it may not have a typed `llm.Response.ID` available without reparsing the response body.

For Responses chain binding, prefer one of these:

* Capture response IDs at `OnOutboundLlmResponse` for non-stream responses.
* Capture final response IDs from `OnOutboundLlmStream` or stream state for streaming responses.
* Store extracted response IDs in `PersistenceState` before the final raw response middleware runs.

Do not rely on brittle string extraction from final client JSON unless no structured hook is available.

## Implementation Notes

The implementation must preserve the routing-time sticky payload before outbound channel transformation mutates fields such as `llmRequest.Model` to the channel's actual model. Response ID and completed-prefix aliases should be computed from this routing-time payload so lookup and bind keys match.

Each outbound attempt must clear captured sticky response fields before sending the request. Otherwise a failed attempt can leave a stale response ID or assistant message in shared request state, and a later successful retry could bind the wrong alias to the final channel.

Successful fallback migration is implemented by binding all active aliases to the current successful channel after response completion. Failed attempts and incomplete streams do not refresh bindings.

## Test Plan

Add or update tests under `internal/server/orchestrator/`:

* Existing unbound selection still chooses within current eligible tier.
* Existing bound channel is still preferred when eligible.
* Chat transcript drift regression:
  * Round 1 successful completed prefix binds channel A.
  * Round 2 request includes round 1 transcript plus new user message.
  * Longest prefix lookup finds A.
* Agent/tool transcript regression:
  * Tool-call/tool-result additions do not create a different session identity when a protocol session key exists.
* Responses chain:
  * `previous_response_id=resp_1` routes to channel A when `resp_1 -> A` exists.
  * Successful response with `ID=resp_2` binds `resp_2 -> A`.
  * If A fails and fallback B completes the request, `resp_2 -> B` and active aliases refresh to B.
* Missing/ineligible bound channel:
  * Binding is skipped or deleted according to current stale/ineligible rules.
* No stable identity:
  * Single short user message returns no sticky lookup and falls back to normal load balancing.
* Additional regressions:
  * Two unrelated conversations with the same API key/model/tools do not collide when their transcript prefixes differ.
  * `prompt_cache_key` lookup is used only when no stronger session/Responses key exists.
  * Interrupted stream does not bind or refresh a sticky alias.
  * Multiple aliases for the same channel expire after TTL.

## Open Decisions

* Whether fallback success should immediately migrate a Responses chain binding, or only bind the new response ID while preserving old IDs until TTL expiry.
* How many transcript prefix hashes to write on successful chat responses.
* Whether to expose debug traces for sticky identity kind/reason in request logs or traces.
