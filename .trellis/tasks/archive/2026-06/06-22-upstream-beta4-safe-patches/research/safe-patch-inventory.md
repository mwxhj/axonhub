# Safe Patch Inventory

## Purpose

Record the initial audit for porting low-risk upstream `v1.0.0-beta4` fixes
without adopting upstream routing, credential, quota, retry, or fallback
semantics.

This file is the initial inventory. The complete execution gate is
`complete-safe-patch-evaluation.md`, which classifies every commit in
`HEAD..v1.0.0-beta4` plus selected post-beta4 follow-ups.

## Source Points

* Local branch: `unstable`.
* Local head during audit: `c9e778d refactor(routing): route requests through api key tiers`.
* Upstream tag: `v1.0.0-beta4 = 7122f329`.
* Merge base: `88d2a59d9b84e81ba33ef1368f05f27e20cbe904`.
* Divergence count from audit: `HEAD...v1.0.0-beta4 = 51 local / 73 upstream`.

## Hard Reject Areas

### Provider Quota

Reject direct port.

Evidence from upstream:

* `internal/server/orchestrator/candidates_quota.go` adds `ProviderQuotaSelector`
  that filters candidates based on provider quota status.
* `internal/server/orchestrator/lb_strategy_quota.go` adds `QuotaAwareStrategy`
  that scores channels by provider quota.
* `internal/server/orchestrator/provider_quota_provider.go` defines a channel-ID
  provider quota interface.
* `internal/server/biz/provider_quota.go` and `internal/server/biz/provider_quota/*`
  create a durable provider quota service and normalized quota status model.
* `internal/ent/schema/provider_quota_status.go` persists provider quota status.
* `frontend/src/components/quota-badges.tsx` and
  `frontend/src/features/system/data/quotas.ts` expose provider quota UI/data.

Reason:

Provider quota is explicitly not a local product/domain model. Upstream provider
quota participates in candidate filtering and load-balancer scoring, which
conflicts with local routing and quota contracts.

### Channel-Owned API Key Management

Reject direct port.

Evidence from upstream:

* `internal/server/biz/channel_apikey.go` manages `Channel.credentials.apiKeys`
  and `Channel.disabled_api_keys`.
* `frontend/src/features/channels/components/channels-test-api-keys-dialog.tsx`
  tests and mutates raw channel API keys.
* `frontend/src/features/channels/components/channels-disabled-api-keys-dialog.tsx`
  manages disabled raw channel keys.
* `internal/server/biz/channel_apikey_provider.go` selects raw channel keys
  from channel credentials.

Reason:

The local product model moved secret ownership to `UpstreamCredential` and
binding ownership to `ChannelCredentialRef`. Reintroducing channel raw key
management would create two competing credential systems.

### Global Route / Retry Semantic Restorations

Reject direct port unless redesigned under local contracts.

Evidence from upstream:

* `RetryPolicy.LoadBalancerStrategy` remains a primary route strategy input in
  upstream code.
* `0c749008` adds response timeout retry settings and pipeline behavior.
* `e260e0b4` adds channel retryable error pattern matching.
* Provider quota selector/strategy reintroduces quota-based routing decisions.

Reason:

Local runtime ordering belongs to API key route tiers. Retry/fallback must not
silently hide failures or restore pre-route-template behavior.

## Safe Candidates

### LLM Protocol / Transformer Fixes

These are good first candidates because they mostly modify `llm/` transformer
and protocol conversion behavior.

* `54ffbdf7` Anthropic server-side tool blocks.
* `28adad0a` OpenAI stream aggregation nil panic.
* `c3bb25b3` Responses function call namespace.
* `ad096f57` OpenAI audio APIs.
* `7891a8d8` TTS streaming/storage/memory fixes.
* `68672716` Codex image generation/edit.
* `12e6082d` Codex image generation fix.
* `d9fb1555` image generation bypass fix.
* `31d43681` Cerebras transformer.
* `94394eb0` OpenAI video schema.
* `d1d9779b` OpenAI Responses schema.

Review notes:

* `ad096f57` touches backend API/orchestrator request handling in addition to
  `llm/`; it must be adapted to local `AttemptTarget` and credential semantics.
* `d9fb1555` touches request cURL generation and pass-through behavior; frontend
  masking rules apply.
* Any GraphQL/Ent generated diff must be regenerated from local schema if needed.

### Infrastructure / Stability Fixes

Good candidates after reviewing config/default impact:

* `23b062cf` disabling DB auto migration.
* `7122f329` SQLite busy timeout.
* `4a6ade51` Redis multi mode.
* `cd1157ce` data storage OOM fix.
* `f41f6de8` IP blocklist.
* `4562ac2b` GC and retry operational optimization, only if retry semantics are not changed.
* `be5523c6` backup/restore usage log split, only after schema reconciliation.

Review notes:

* Config additions must not silently break existing installs.
* Backup/restore must preserve local credential/quota/request schema.

### Request Observability / Export

Candidate after HCI review:

* `98c15184` request URL and pass-through flag.
* cURL generator improvements from image/audio patches.
* Request detail performance/navigation fixes that preserve operator-facing
  state without adding backend identity exposure.

Review notes:

* Normal UI/export must not display secret material, fingerprints, key hints,
  `resourceScopeKey`, or credential source strings.
* Request detail should show operator-useful facts, not backend implementation
  identities.

## Design-First Later Candidates

These are not part of the safe-patch task:

* Channel retryable status/error patterns.
* Media condition routing.
* Strict local channel RPM admission.
* Model circuit breaker changes.
* Response timeout retry.
* API key quota usage OpenAPI query, if it crosses into provider quota or global quota semantics.

They may be useful, but each needs its own local semantic design before code.

## Safe Correctness Candidates Added After Complete Review

The complete commit-level review also selects safe permission/account/runtime
correctness fixes that do not alter routing ownership:

* Generic NOT_FOUND fallback text.
* Request execution channel field permission guard.
* Own-profile/language update path.
* Model fetcher scope check and stored-credential endpoint boundary, adapted to
  local `CredentialViews()`.
* OIDC pre-auth privacy-bypass fix for the user's own identity check.
* API key creator column/filter permission fixes.
* API key token usage project-scope fix.
* Async channel cache/performance initialization fixes.
* Hide mapped model aliases across direct, prefixed, and auto-trimmed entries.

## Recommended Execution Order

1. Protocol/transformer fixes that do not touch schema/UI.
2. Endpoint additions that require backend API adaptation.
3. Security, permission, account, and runtime correctness fixes.
4. Infrastructure fixes.
5. Request observability/export fixes under HCI masking.
6. Stop and reassess before any design-first routing/retry/quota feature.
