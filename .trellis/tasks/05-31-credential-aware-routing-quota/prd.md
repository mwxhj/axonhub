# Credential-Aware Routing And Quota

## Goal

Make upstream credentials a first-class routing, sticky-session, quota, and observability resource. Axon currently treats `channel` as the main upstream unit, but a channel can contain multiple API keys and the same API key can be duplicated across multiple channels to control model scope. This makes cache affinity, quota status, failure handling, and metrics inaccurate.

The target design is:

```text
channel = routing/model policy view
credential = real upstream identity/cache/quota/limit resource
execution target = channel + credential + model
```

## Problem

The sticky-session work exposed a deeper abstraction issue:

* Sticky binding to `channelID` is not enough when one channel contains multiple upstream API keys.
* Sticky binding to `channelID + key` is also not enough when the same key appears in multiple channels.
* Provider quota is currently mostly channel-scoped, but quota/limits/cache often belong to the upstream credential/account.
* Multi-key channels can look like extra capacity even when several channels or keys point to the same real upstream account.
* A single exhausted or rate-limited credential should not disable an unrelated credential, but it should affect every channel that references the same credential.

This is especially important for OpenAI-style prompt caching:

* Prompt cache locality depends on the real upstream account/cache pool.
* A session routed to the same Axon channel can still miss cache if it rotates across credentials.
* A session routed to a different Axon channel may still hit cache if both channels use the same upstream credential.

## What We Know

* Current channel credentials are stored inline in `objects.ChannelCredentials`.
* `ChannelCredentials.APIKeys` can hold multiple keys, and older data may still use `APIKey`.
* `getAPIKeyProvider` already returns a sticky provider for multi-key channels when more than one enabled key exists.
* Current multi-key provider uses trace ID for stable selection and random selection when no trace exists.
* The selected upstream key can be stored in context through `contexts.WithChannelAPIKey`, but that path is not yet a strong execution contract.
* Current provider quota checkers take `*ent.Channel`, so provider quota state is naturally channel-scoped today.
* Channel auto-disable already has some API-key-specific failure counting and disabled-key behavior, but the data model is still nested under a channel.
* Operators duplicate the same upstream key across channels to control model support, model mapping, priorities, tags, and endpoint behavior.

## Core Principles

1. A channel is not necessarily a real upstream resource.
2. Credentials are deployment-global resources, matching the current global channel/upstream-key configuration model.
3. A credential is the real upstream identity for cache, quota, account-level limits, authentication failures, and many rate limits.
4. Same credential across multiple channels must be recognized as one upstream resource.
5. One channel can reference multiple credentials.
6. Sticky-session should bind to the credential identity when cache locality matters.
7. Channel priority/model eligibility still decides the candidate channel set.
8. Credential selection happens inside eligible channels and must not bypass channel eligibility.
9. Credential failures should degrade at credential scope first, then channel scope only if no usable credential remains.
10. Raw secret values must not be exposed in logs, UI, metrics, or sticky keys.

## Target Data Model

Introduce an upstream credential concept. Exact schema names can change during implementation, but the model should represent:

```text
UpstreamCredential
  id
  deployment-global scope
  provider type
  normalized base URL / endpoint scope
  auth kind: api_key | oauth | gcp | azure | other
  encrypted secret payload
  fingerprint
  display name / remark
  enabled status
  weight
  created_at / updated_at

ChannelCredentialRef
  channel_id
  credential_id
  enabled
  weight override
  model/include constraints if needed later
```

Credential fingerprint should identify the same real upstream account without storing or exposing the raw secret:

```text
fingerprint = hash(provider + normalized_base_url + auth_kind + stable_auth_identity)
```

For API keys, `stable_auth_identity` can be a keyed hash of the secret. For OAuth, it should use the best stable account/project identity available, falling back to a secure hash of the credential payload if necessary.

## Routing Requirements

### Channel Selection

Channel selection remains responsible for:

* API format and endpoint support.
* Model support and model mapping.
* Priority and ordering weight.
* Tags and profile restrictions.
* Channel-level health, circuit breaker, and queue constraints.

Sticky-session must not pick a credential from a channel that is not in the eligible channel set.

Channel priority remains stronger than credential stickiness. Credential affinity may reorder or select within the current eligible priority group, but it must not cross to a lower-priority channel only to preserve the same credential. Lower-priority channels are used only when existing retry/fallback behavior has already allowed moving to that priority level.

### Credential Selection

After channel candidates are determined, each candidate channel exposes eligible credential refs:

* enabled
* not deleted
* not exhausted for relevant quota type
* not in credential cooldown
* not disabled by repeated credential-specific auth/rate-limit failures

When sticky-session has a stable identity, credential selection should use that identity to choose or reuse a credential:

```text
sticky identity -> credential fingerprint
sticky identity -> channel + credential
```

The selected execution target becomes:

```text
channelID + credentialID/fingerprint + actualModelID
```

### Same Credential Across Channels

If channel A and channel B reference the same credential:

* Quota status is shared.
* Credential cooldown is shared.
* Credential-specific disabled state is shared.
* Cache affinity can remain valid when routing crosses A/B for model-policy reasons.
* Metrics should be able to aggregate by credential independent of channel.
* Sticky-session may keep the same credential across A/B only when both channels are in the same currently eligible priority group, or when retry/fallback has already moved routing to B's priority group.

### Multi-Key Channel

If one channel references multiple credentials:

* sticky-session should not randomly move a conversation between credentials.
* unbound requests can distribute across credentials by weight.
* bound requests should prefer the bound credential if it is eligible within the selected channel.
* if the bound credential is unavailable, request may escape to another credential in the same eligible channel or to another channel according to retry policy.

## Quota Requirements

Provider quota status should support credential-level state:

```text
credential_id/fingerprint -> quota status
credential_id/fingerprint + model scope -> optional quota status
channel_id -> derived aggregate status
```

Channel status is derived from its referenced credentials:

* available if at least one eligible credential remains for the model/request.
* warning if usable credentials remain but at least one relevant credential is warning.
* exhausted if no eligible credential remains because all referenced credentials are exhausted/unavailable for the request.

Provider quota checkers should be able to check a credential directly. If a provider only supports channel-level checking today, the design should adapt it through a channel-to-credential wrapper rather than keeping credential state invisible.

## Failure Handling Requirements

Credential-scoped failures:

* 401/403 authentication failures.
* account quota exhausted.
* per-key or per-account long rate limits.
* provider says billing/account unavailable.

These should affect the credential across all channels that reference it.

Channel-scoped failures:

* base URL unreachable.
* endpoint format mismatch.
* transformer/protocol failure tied to channel configuration.
* model mapping error.
* channel queue/circuit breaker state.

These should not automatically disable a shared credential globally unless the response clearly identifies the credential/account as the failing resource.

Temporary network/5xx failures should follow existing retry/circuit-breaker semantics and should not immediately migrate sticky bindings unless the existing sticky-session migration rules say so.

## Observability Requirements

Request execution records should capture the selected credential identity without leaking secret values:

```text
channel_id
credential_id or credential_fingerprint
credential_display_name
credential_key_prefix/suffix if already allowed and safe
actual_model_id
cache read/write indicators where available
```

Request list/detail and metrics should eventually show:

* channel used
* credential used
* cached tokens
* per-credential request count
* per-credential cost
* per-credential error counts
* per-credential quota status

## Frontend Requirements

The final UX should make credentials manageable as first-class resources. A complete version likely needs:

* A Credentials page for global upstream credentials.
* Channel edit UI that attaches/detaches credentials instead of only editing inline key arrays.
* Channel detail showing referenced credentials and status.
* Credential detail showing channels that reference it.
* Per-credential status, quota, recent errors, request count, cached token totals, and cost.
* Migration UI or compatibility behavior for existing inline credentials.

The UI must not expose raw secrets after creation/update.

## Migration Requirements

Existing data must continue to work:

* `credentials.apiKey` becomes one credential.
* `credentials.apiKeys[]` becomes multiple credentials.
* existing disabled API keys should migrate to credential disabled/cooldown state where possible.
* duplicated same raw key across channels should map to the same fingerprinted credential if provider/baseURL/auth scope matches.
* old backup/restore data must remain importable.

Migration should avoid duplicating capacity:

* if the same key appears in three channels, it should become one credential referenced by three channels.

## Implementation Scope

The first implementation already landed as the derived credential identity phase. It keeps existing inline channel credentials and derives a safe `credential_fingerprint` at runtime and persistence boundaries.

Implemented scope:

* runtime credential fingerprinting for API-key credentials.
* sticky-session binding to `channel + credential_fingerprint`.
* same-priority credential preservation across eligible channels.
* selected credential propagation into request execution, usage logs, performance records, and sticky binding refresh.
* credential-aware quota cache and channel availability derivation.
* credential-scoped auto-disable only for clear credential/account failures (`401`, `402`, `403`).

The next implementation should now complete the full credential model instead of leaving it deferred:

* first-class `UpstreamCredential` and `ChannelCredentialRef` tables.
* credential management frontend.
* physical migration of inline secrets into a credential table.
* provider-specific OAuth/GCP/Azure credential identity support beyond current API-key derived identity.
* channel edit UI based on attaching/detaching credentials.
* request and quota UI visibility by credential.

Building/pushing a Docker image is still out of scope unless explicitly requested.

## Full Implementation Requirements

### Backend Model

Add first-class credential storage:

```text
UpstreamCredential
  id
  name
  provider_type
  base_url
  auth_kind
  secret_payload
  fingerprint
  status
  weight
  remark
  created_at
  updated_at

ChannelCredentialRef
  id
  channel_id
  credential_id
  enabled
  weight_override
  created_at
  updated_at
```

Credential ownership remains deployment-global. Projects, API keys, and profiles continue to restrict access through channels; they do not need duplicated upstream credentials per project.

### Migration

Existing inline channel credentials must be migrated into credential rows:

* `credentials.apiKey` becomes one `UpstreamCredential`.
* `credentials.apiKeys[]` becomes multiple `UpstreamCredential` rows.
* same provider/baseURL/auth kind/key must dedupe to one credential row.
* channels get `ChannelCredentialRef` rows pointing to deduped credentials.
* existing disabled API keys should become credential disabled/cooldown state where the failure is credential-scoped.
* inline credentials remain readable as legacy import/export fallback during compatibility period.

Runtime must prefer credential refs and fall back to legacy inline credentials only when refs are absent.

### Routing

Channel remains the outer policy unit:

* model support
* API format
* priority
* tags/profile access
* endpoint/transform options
* channel-level health/circuit breaker

Credential becomes the upstream execution identity:

* selected API key/OAuth/GCP/Azure secret
* prompt cache/account locality
* provider quota
* credential-specific auth/billing failures
* request execution and usage aggregation

Execution target must be:

```text
channelID + credentialID + credentialFingerprint + actualModelID
```

Sticky-session can preserve a credential across eligible same-priority channels, but must not cross channel priority only to keep credential affinity.

### Quota And Failure

Provider quota should be credential-first:

* check credential quota when provider supports it.
* derive channel status from attached credentials.
* aggregate credential quota into channel list/detail UI.
* unknown/unsupported quota must not falsely exhaust channels.

Failure scope:

* `401`, `402`, `403`, account disabled, billing unavailable, and account quota exhausted are credential-scoped.
* network errors, 5xx, transformer/config errors, endpoint incompatibility, and circuit breaker state are channel-scoped unless provider response clearly identifies a credential/account failure.

### Frontend

Add a global Credentials page:

* list credentials with name, provider, base URL, auth kind, status, fingerprint prefix, referenced channels count, quota status, latest error, usage/cost summary.
* create credential.
* edit display fields and weight.
* rotate secret without revealing the old secret.
* enable/disable credential.
* view referencing channels.

Update Channel create/edit:

* attach existing credential.
* create credential inline and attach it.
* detach credential.
* enable/disable a channel credential ref.
* set per-channel weight override.
* preserve legacy inline credential import path where needed.

Update Request/Usage/Quota visibility:

* show credential name/fingerprint in request execution detail.
* allow usage filtering/grouping by credential fingerprint.
* show per-credential quota/status inside channel quota UI.

## Acceptance Criteria

Derived identity acceptance:

* [x] Design documents define channel, credential, and execution target responsibilities.
* [x] Same upstream key across multiple channels is represented as one credential identity.
* [x] One channel can reference multiple credentials without randomizing sticky sessions across credentials.
* [x] Sticky-session can bind to credential identity as well as channel eligibility.
* [x] Sticky-session can keep the same credential across same-priority eligible channels.
* [x] Sticky-session does not cross channel priority only to keep the same credential.
* [x] Provider quota can be tracked at credential scope and reflected into channel availability.
* [x] Credential-specific failures affect all referencing channels.
* [x] Channel-specific failures do not globally disable a credential by accident.
* [x] Request execution records can store selected credential identity without exposing secrets.
* [x] Frontend UX direction is documented for credential management and channel references.
* [x] Migration from existing inline `apiKey` / `apiKeys` is documented.

Full model acceptance:

* [ ] `UpstreamCredential` schema exists with safe fingerprint and secret payload storage.
* [ ] `ChannelCredentialRef` schema exists and supports enabled state plus weight override.
* [ ] Inline channel credentials are migrated/deduped into credential rows.
* [ ] Runtime channel construction prefers credential refs and only falls back to inline legacy credentials when refs are missing.
* [ ] Sticky binding persists `channelID + credentialID + credentialFingerprint`.
* [ ] Provider quota can be checked and cached by credential row.
* [ ] Channel quota status is derived from attached credential rows.
* [ ] Credential-scoped failures disable/cool the credential across all channel refs.
* [ ] Channel-scoped failures do not globally disable shared credentials.
* [ ] GraphQL exposes credential CRUD and channel attach/detach operations without returning raw secrets.
* [ ] Credentials page supports list/create/edit/rotate/enable/disable and referencing channel visibility.
* [ ] Channel create/edit UI supports attach/detach credentials and legacy import compatibility.
* [ ] Request detail and usage views show selected credential identity.
* [ ] Backup/restore remains compatible with legacy inline credentials.

## Resolved Decisions

* Project/profile credential restrictions are not part of the first full implementation. Channel restrictions remain the access-control boundary.
* Quota should be credential-level first, with optional per-limit/per-model detail when providers expose it.
* The next implementation should physically introduce credential tables and migrate inline secrets, while keeping legacy fallback for import/export compatibility.

## Definition Of Done

* `prd.md` captures the product and acceptance scope.
* `info.md` captures the technical design and migration strategy.
* Backend schema, runtime, GraphQL, frontend, migration, and tests are updated together.
* No raw upstream secret is returned through GraphQL, logs, request execution records, or frontend display.
