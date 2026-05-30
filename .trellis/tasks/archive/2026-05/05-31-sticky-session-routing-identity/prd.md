# Sticky Session Routing Identity Redesign

## Goal

Redesign the `sticky-session` load-balancer identity model so sticky routing binds stable request identity to channels instead of binding a drifting transcript fingerprint to channels. The current implementation is useful for some cache-prefix cases, but it does not reliably preserve session-level stickiness for normal multi-turn chat, agent tool loops, or OpenAI Responses API chains.

## Problem

The first implementation used a sticky key built from request scope, model, tools, system/developer prompts, and an early message window. That confused two different concepts:

* **Routing identity**: the stable conversation/session/chain identity that should stay on the same channel.
* **Cache prefix identity**: the current content prefix that may grow every turn and is useful for cache locality, but is not stable enough to be the session key.

Because message history grows over time, a key derived from "messages before the latest user message" can drift every round:

```text
Round 1: [user A]
Round 2: [user A, assistant A, user B]
Round 3: [user A, assistant A, user B, assistant B, user C]
```

The current key can become different in each round. Agent flows are worse because tool calls and tool results can add protocol-specific user/tool/assistant messages that shift the extracted prefix. That means a multi-turn conversation can miss its own previous binding.

## What We Know

* `sticky-session` already exists as a separate strategy and is selected through the existing load-balancer strategy setting.
* Current binding store is in-memory with a 5 minute TTL.
* Current binding writes happen after a successful response path, including successful fallback channels.
* Axon already preserves OpenAI Responses API `previous_response_id` through request/response transformation.
* Axon already tracks response IDs in the OpenAI Responses API transformer stream/non-stream paths.
* Sticky routing currently treats `previous_response_id` as one signal inside a hashed payload, not as a first-class routing anchor.
* Current early-message-window extraction is not a valid session identity for normal multi-turn chat.

## Requirements

### Identity Priority

Sticky-session must choose routing identity in this order:

1. **Protocol/session identity** for clients that expose stable session context, especially Codex and Claude Code.
2. **OpenAI Responses chain identity** using `previous_response_id` lookup and successful response `response.id` binding.
3. **Prompt cache key identity** when the request provides an explicit stable `prompt_cache_key`.
4. **Transcript prefix index fallback** for Chat Completions / Anthropic-like requests without an explicit session or response chain.
5. **No sticky routing** when no suitable stable identity can be established.

### Protocol Session Identity

For clients with stable session headers or metadata:

* Codex-style stable session/window headers should produce a stable sticky identity.
* Claude Code-style stable user/session/project metadata should produce a stable sticky identity where available.
* Tool results and per-turn content must not be part of this identity.
* Identity must remain scoped by API key/project/profile/model/client format so unrelated users or profiles cannot collide.

### OpenAI Responses Chain Identity

For Responses API requests:

* If a request includes `previous_response_id`, sticky-session must first look up `previous_response_id -> channelID`.
* If found and the channel is a current eligible candidate, that channel must be ordered first.
* After a successful response, sticky-session must bind the returned `response.id -> current channelID`.
* If the response includes `previous_response_id`, refreshing that mapping to the current successful channel is allowed.
* Responses chain binding must be treated as stronger than transcript-prefix fallback because the upstream may require response IDs to stay on the same provider/account/channel.
* If the bound channel is unavailable, existing retry/fallback behavior may still escape according to the current retry policy, but the design must document the risk and avoid silently pretending the chain is portable.

### Transcript Prefix Index Fallback

For request formats without a durable session or response chain:

* Do not use the full current transcript as a single sticky key.
* Build exact prefix hashes over canonicalized transcript prefixes and store successful prefixes for a short TTL.
* On a new request, compute candidate prefix hashes from the current transcript and query longest prefix first.
* Prefer the longest exact prefix match; if multiple matches have the same prefix length, prefer the most recently refreshed eligible binding.
* Do not use semantic similarity or fuzzy text matching in the MVP.
* Do not include unstable assistant/tool execution artifacts in identity unless they are part of an exact prior transcript prefix needed to recognize the same conversation.
* Prefix matching must be bounded so it does not scan all stored bindings.

### Existing Routing Semantics

* Sticky routing must still respect candidate eligibility and health.
* Sticky routing must not invent channels outside the existing candidate set.
* Sticky routing must not bypass model support, disabled channels, quota exhaustion, or open circuit-breaker state.
* Priority/weight semantics for unbound selection must remain consistent with the current strategy unless explicitly changed.
* The binding TTL remains 5 minutes unless a later requirement changes it.

### Migration From Current Implementation

* Replace or downgrade the current early-message-window payload strategy so it cannot be the primary session identity.
* Keep current frontend strategy selection unchanged unless wording needs to clarify behavior.
* Preserve existing adaptive/failover/circuit-breaker behavior when sticky-session is not selected.

## Design Risks To Handle

### Response ID Portability

OpenAI Responses `response.id` may be provider/account/channel-local. If a request with `previous_response_id` escapes to another channel, that channel may reject it or continue from the wrong state. Sticky-session must treat response-chain stickiness as a correctness concern, not only a cache optimization.

### Fallback Migration Semantics

Current sticky-session refreshes binding to whichever channel eventually succeeds. For response-chain requests this is mostly the right direction after a completed fallback: if a fallback channel successfully continues the chain and returns a new response, that channel now has the fresh cache/state and should become the active owner for subsequent routing. Failed attempts must not migrate, but successful completed fallbacks should refresh active aliases to the successful channel.

### Parallel Conversations

API key + model + tools is not enough to identify a session. Two unrelated conversations can share those fields. Protocol session IDs, response chains, prompt cache keys, and exact transcript-prefix matches must stay scoped and collision-resistant.

### Prefix Match Ambiguity

Transcript prefix matching should use exact canonical prefix hashes and longest-prefix lookup. Fuzzy matching or semantic similarity is out of scope because it can bind two different but similar conversations to the same channel.

### Prompt Cache Key Semantics

`prompt_cache_key` can be a stable session signal in some clients, but it is not guaranteed to mean conversation identity for every provider or client. It should be stronger than transcript fallback but weaker than protocol session IDs and Responses chain IDs.

### Streaming And Partial Responses

Bindings should be refreshed only after a response is sufficiently successful. Streaming paths must avoid binding a channel for interrupted streams, partial outputs, or protocol errors that never reached a completed response state.

### Multi-Instance Deployment

The MVP store remains in-memory. Multiple Zeabur replicas will not share sticky bindings. This is acceptable only if deployment runs one app instance or the operator accepts weaker stickiness. A distributed store is out of scope for this task but should remain a future extension point.

## Acceptance Criteria

* [ ] A normal multi-turn chat with growing history can route round 2 and round 3 back to the channel that served prior matching transcript prefix.
* [ ] A single first-turn chat without stable context can still decline sticky routing when no useful identity exists.
* [ ] Codex/Claude Code-style stable session metadata produces the same sticky identity across tool-loop turns.
* [ ] OpenAI Responses API request with `previous_response_id` looks up the channel bound to that response ID before transcript fallback.
* [ ] Successful Responses API response binds the returned `response.id` to the successful channel.
* [ ] Successful Responses API fallback migrates the active chain/session aliases to the successful channel for the 5 minute TTL.
* [ ] Bound channels are used only if present in current candidates and eligible.
* [ ] If a binding points to a missing channel, the binding is ignored or removed according to current stale-binding behavior.
* [ ] Tests cover key drift regression for normal chat and agent/tool-like transcripts.
* [ ] Tests cover Responses API chain binding and response ID refresh.
* [ ] Tests cover longest-prefix selection and no-match fallback.
* [ ] Tests cover response-chain fallback migration after completed fallback success.
* [ ] Tests cover interrupted stream behavior so incomplete streams do not create misleading bindings.

## Out of Scope

* Distributed sticky binding store across multiple Zeabur replicas.
* Semantic similarity matching, embeddings, or fuzzy text matching.
* Changing the frontend settings flow.
* Changing non-sticky load-balancer strategies.
* Long-term persistence beyond the short in-memory TTL.

## Technical Notes

* Current sticky implementation: `internal/server/orchestrator/sticky_session.go`
* Current strategy selection: `internal/server/orchestrator/orchestrator.go`
* Current candidate ordering entry point: `internal/server/orchestrator/select_candidates.go`
* Current OpenAI Responses transformer preserves `previous_response_id`: `llm/transformer/openai/responses/outbound.go`
* Current stream transformer tracks response IDs: `llm/transformer/openai/responses/outbound_stream.go`
* Current WebSocket executor has previous-response reuse logic: `llm/transformer/openai/responses/websocket_executor.go`

## Definition of Done

* Focused Go tests added for the new identity model.
* Existing sticky-session tests updated to the new semantics.
* Relevant docs or task info updated with final behavior.
* Go tests for affected packages run when implementation is complete.
