# Credential-Aware Routing And Quota Technical Design

## Design Thesis

The correct abstraction is not:

```text
stickyKey -> channel
provider quota -> channel
failure state -> channel
```

The correct abstraction is:

```text
sticky identity -> credential/resource identity, constrained by eligible channels
provider quota -> credential/resource identity
failure state -> credential or channel depending on cause
request execution -> channel + credential + model
```

Channels remain important, but they are policy views. Credentials represent real upstream account/cache/quota resources.

Refined full-model boundary:

```text
Credential = upstream key/account asset, quota/budget state, operator metadata
Channel = endpoint/API format/model/routing policy
ExecutionTarget = selected channel + selected credential + actual model
```

Credential should not become a second place to configure channel behavior. In the full model, baseURL, API format, transformer settings, model support, priority, retry policy, and project/profile access restrictions all remain channel responsibilities.

## Current Implementation Clues

Relevant current files:

* `internal/objects/channel.go`
  * `ChannelCredentials` stores `APIKey`, `APIKeys`, `OAuth`, Azure, and GCP credential payloads inline.
  * Comments still describe multi-key usage as round-robin, but runtime implementation has evolved.
* `internal/server/biz/channel_llm.go`
  * `getAPIKeyProvider` returns `NewTraceStickyKeyProvider` when a channel has multiple enabled API keys.
  * Transformer construction passes only an `auth.APIKeyProvider`, not a selected credential identity.
* `internal/server/biz/channel_apikey_provider.go`
  * `TraceStickyKeyProvider` selects a key by trace ID when available, otherwise random.
  * It tries to store the selected key in context through `contexts.WithChannelAPIKey`.
* `internal/contexts/context.go`
  * `WithChannelAPIKey` returns a new context but current provider call sites do not clearly make selected credential identity part of the execution contract.
* `internal/server/biz/provider_quota.go`
  * Provider quota cache and check flow are channel-oriented.
* `internal/server/biz/provider_quota/types.go`
  * Quota checker interface accepts `*ent.Channel`.
* `internal/server/biz/channel_auto_disable.go`
  * Failure handling already has both channel-level and API-key-level concepts, but API-key state remains nested under a channel.
* `internal/server/biz/request.go` and `internal/server/biz/usage_log.go`
  * Request/execution/usage paths store channel IDs, but not a first-class upstream credential ID.
* `internal/server/orchestrator/sticky_session.go`
  * Sticky-session currently binds channel aliases, not credential aliases.

## Proposed Domain Model

### UpstreamCredential

Represents a real upstream key/account asset. It is intentionally smaller than a channel.

Suggested fields:

```go
type UpstreamCredential struct {
    ID int
    Name string
    SecretPayload encrypted JSON
    Fingerprint string
    Status string
    Weight int
    Remark string
    SecretKind string
    IssuerScope string
    KeyHint string
    QuotaScopeID *int
    QuotaStatus string
    LastError string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

The secret payload should preserve provider-specific shapes:

```json
{
  "apiKey": "...",
  "oauth": {...},
  "azure": {...},
  "gcp": {...}
}
```

The stored secret can be reused by transformer construction. The fingerprint is the safe identity used in logs, sticky keys, quotas, and metrics.

Recommended Ent schema fields:

```go
field.String("name").Default("")
field.JSON("secret_payload", objects.UpstreamCredentialSecret{}).Sensitive()
field.String("fingerprint").Unique().MaxLen(128)
field.Enum("status").Values("enabled", "disabled", "archived").Default("enabled")
field.Int("weight").Default(100)
field.String("remark").Optional()
field.Enum("secret_kind").Values("api_key", "oauth", "azure", "gcp", "other")
field.String("issuer_scope").Default("")
field.String("key_hint").Default("")
field.Int("quota_scope_id").Optional().Nillable()
field.String("quota_status").Default("unknown")
field.String("last_error").Default("")
```

Indexes:

* unique `fingerprint`.
* non-unique `status`.
* non-unique `issuer_scope, status`.
* non-unique `quota_scope_id`.

Credential list/detail should not expose `secret_payload`.

Fields that should not be on `UpstreamCredential` in the full model:

* channel baseURL / endpoint URL.
* API format such as OpenAI, Anthropic, Responses, Claude Code, or Codex.
* supported model list or model mapping.
* channel priority/order.
* transformer or pass-through settings.
* project/profile/API-key access restrictions.

### ChannelCredentialRef

Represents a channel's use of a credential.

Suggested fields:

```go
type ChannelCredentialRef struct {
    ID int
    ChannelID int
    CredentialID int
    Enabled bool
    Weight int
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

Future fields could include model-specific credential constraints, but the first full implementation can keep model control at channel level.

Recommended Ent schema fields:

```go
field.Int("channel_id").Immutable()
field.Int("credential_id").Immutable()
field.Bool("enabled").Default(true)
field.Int("weight_override").Optional()
```

Indexes:

* unique `channel_id, credential_id`.
* non-unique `credential_id`.
* non-unique `channel_id, enabled`.

Edges:

* `Channel` has many `credential_refs`.
* `UpstreamCredential` has many `channel_refs`.
* `ChannelCredentialRef` belongs to one channel and one credential.

### ExecutionTarget

Runtime-only or persisted on request execution:

```go
type ExecutionTarget struct {
    ChannelID int
    CredentialID int
    CredentialFingerprint string
    ModelID string
    ActualModelID string
}
```

This should be available to:

* transformer auth provider
* request execution persistence
* quota accounting
* failure classifier
* sticky binding refresh
* request logs and metrics

## Credential Fingerprint

Fingerprint goals:

* deduplicate the same upstream credential across channels.
* avoid exposing secrets.
* be stable across restarts.
* include enough scope to avoid false merges.

Suggested API key fingerprint input:

```text
v1
issuer_scope
secret_kind = api_key
secret HMAC
```

Suggested OAuth fingerprint input:

```text
v1
issuer_scope
secret_kind = oauth
account identity if available
project/account id if available
fallback secret HMAC
```

Use an application secret/HMAC if available instead of plain hash so fingerprints cannot be used for offline key guessing.

Do not include channel ID in the fingerprint, otherwise duplicated credentials across channels will not deduplicate.

Do not include full channel baseURL in the fingerprint by default. The same upstream key may be attached to multiple channels with different base URLs, model scopes, or endpoint routes in order to control routing. Splitting those into multiple credentials breaks quota, cache, and request observability.

Use `issuer_scope` only as a coarse namespace to avoid false merges:

* official OpenAI key: `openai`
* official Anthropic key: `anthropic`
* Azure/GCP/project-backed credential: account/resource/project identity where available
* unknown OpenAI-compatible provider: inferred provider host or explicit issuer scope, not the whole endpoint URL

If the issuer cannot be determined, prefer conservative dedupe within the same channel family but document the ambiguity in the UI. Operators can rename credentials or split/merge through management operations later.

## Locked Decisions

### Credential Ownership

Credentials are deployment-global resources. This matches the current behavior where channels and upstream keys are configured globally, while projects/API keys/profiles constrain which channels are available to a request. Switching projects must not require re-entering the same upstream keys for every channel.

### Credential Stickiness Across Channels

Credential affinity can preserve the same credential across multiple eligible channels only inside the same currently selected priority group. It must not cross to a lower-priority channel only to preserve the credential. Lower-priority channels are considered only when the existing retry/fallback flow has already moved routing to that priority level.

## Routing Flow

### High-Level Flow

```text
request
-> build channel candidates by current eligibility rules
-> group/select channels by current priority semantics
-> resolve sticky/session identity
-> for each eligible channel, resolve eligible credential refs
-> prefer credential binding if it is usable in an eligible channel
-> otherwise select credential by sticky identity + credential weights
-> execute channel + credential + model
-> on success, refresh sticky aliases to selected credential/execution target
```

### Important Constraint

Credential affinity must not bypass channel policy. If a sticky credential is referenced only by a channel that does not support the requested model, the router must not use that channel.

### Channel And Credential Priority

Chosen behavior:

1. Keep existing channel priority and strategy as the outer selection layer.
2. Within the selected channel tier, credential affinity can choose among credentials referenced by eligible channels.
3. If the same sticky credential is referenced by multiple eligible same-priority channels, prefer the normal channel order or the previous bound channel if still eligible.
4. Do not cross channel priority only to keep the same credential unless the existing retry/fallback policy has already moved to lower priority.

This preserves current routing intent while fixing cache/quota identity.

## Sticky-Session Model

Current sticky-session should evolve from:

```text
identity -> channelID
```

to:

```text
identity -> execution target alias:
  channelID
  credentialID/fingerprint
  actualModelID
  expiresAt
```

Lookup behavior:

* If both channel and credential remain eligible, prefer that execution target.
* If channel is no longer eligible but the credential is referenced by another eligible same-priority channel, the router may use that channel with the same credential.
* If credential is exhausted/disabled/cooling, skip it and select another credential according to retry/fallback rules.
* On successful fallback with a new credential, refresh the active sticky aliases to the successful execution target.

This is more accurate for prompt cache than channel-only binding.

## Credential Selection Algorithm

For unbound sticky identity:

```text
score = rendezvousHash(stickyIdentity + credentialFingerprint) * credentialWeight
```

For normal non-sticky distribution:

* use weighted random or current provider behavior.
* avoid per-request round-robin for sticky-enabled strategies because it fragments cache.

For no sticky identity:

* selected credential can be random/weighted random.
* request should still record the credential used.

For multiple credentials in one channel:

* select by credential eligibility first.
* do not choose disabled/exhausted/cooling credentials.
* if all credentials are unavailable, mark channel unavailable for that request.

## Provider Quota Model

### Store

Current channel quota cache:

```text
channelID -> QuotaChannelStatus
```

New cache:

```text
credentialID/fingerprint -> QuotaCredentialStatus
quotaScopeID -> shared QuotaCredentialStatus when multiple credentials share one account/billing pool
channelID -> derived QuotaChannelStatus
```

Optional model dimension:

```text
credentialID/fingerprint + modelID -> QuotaLimitStatus
```

### Checker Interface

Current:

```go
type QuotaChecker interface {
    CheckQuota(ctx context.Context, channel *ent.Channel) (QuotaData, error)
}
```

Target direction:

```go
type CredentialQuotaChecker interface {
    CheckCredentialQuota(ctx context.Context, credential *ent.UpstreamCredential, scope QuotaScope) (QuotaData, error)
}
```

Compatibility adapter:

* For providers whose quota API still needs channel config, pass a resolved channel+credential view.
* For providers where quota is inherently account-level, check credential directly and update all referencing channels through derived status.

Provider quota result should persist on credential or quota scope, not on channel:

```text
CredentialQuotaState
  credential_id
  quota_scope_id nullable
  scope: global | model | operation
  status: available | warning | exhausted | cooldown | disabled | unknown
  remaining_amount
  limit_amount
  unit: usd | token | request | credit | unknown
  reset_at
  exhausted_until
  last_checked_at
  last_error
  source: provider_api | response_error | local_budget | manual | inferred
```

Local operator budgets should use the same credential-first shape. Example: a credential can be stopped after daily cost exceeds a configured budget even if the provider still has quota.

Some providers expose account-level quota shared by multiple keys. The first full implementation may treat `credential_id` as the quota scope, but the schema should not block a later `QuotaScope` table:

```text
QuotaScope
  id
  name
  issuer_scope
  status
```

Multiple credentials can point to one `quota_scope_id` when evidence shows they share an upstream billing/account pool.

### Derived Channel Status

For a request model, channel availability should be computed from referenced credentials:

```text
available = any credential is usable for request
exhausted = no credential usable because all are exhausted/disabled/cooling
warning = at least one usable credential remains but quota warning exists
unknown = no fresh credential quota data
```

This avoids disabling a whole channel because one key is exhausted, while still preventing the same exhausted key from being used through another channel.

## Failure Classification

Classify failures into credential-scope and channel-scope.

Credential-scope examples:

* invalid API key
* account disabled
* billing hard limit
* quota exhausted
* per-account long rate limit
* provider returns account/project-level quota status

Channel-scope examples:

* base URL unreachable
* endpoint does not support API format
* model mapping invalid
* request transformation bug tied to channel type
* channel queue timeout
* channel circuit-breaker failures from network/5xx

Ambiguous failures:

* 5xx
* network timeout
* empty response
* transient 429 without account/key-specific signal

Ambiguous failures should not immediately disable a credential globally. They should follow existing channel retry/circuit behavior and possibly short credential cooldown only after repeated evidence.

## Request Execution Persistence

Add or derive fields for execution records:

```text
credential_id nullable
credential_name_snapshot nullable
credential_fingerprint nullable
credential_key_hint nullable
credential_source nullable
credential_quota_status_snapshot nullable
```

Usage logs should be able to aggregate:

* by channel
* by credential
* by channel + credential
* by model
* by sticky identity kind where useful

Do not store raw API key.

Request list should expose the selected credential name and safe key hint as first-class columns alongside channel. This is required for diagnosing cache misses, cost spikes, and quota/rate-limit behavior.

## Frontend Design

### Credentials Page

Purpose: manage upstream key/account assets.

Primary table columns:

* name
* remark
* key hint
* status
* weight
* referenced channels count
* quota status
* quota remaining/reset when available
* request count
* cached tokens
* cost
* latest error
* updated time

Actions:

* create credential
* edit name/remark/weight/status
* rotate secret
* disable/enable
* test quota
* view referencing channels

Do not put baseURL, API format, model list, model mapping, priority, or transformer fields on this page except as read-only context through referenced channels.

### Channel Edit

Replace or augment inline key arrays with credential references:

* attach existing credential
* create credential inline
* detach credential
* set per-channel credential weight override
* show per-credential availability within this channel

Channel edit remains responsible for:

* provider/channel type
* baseURL / endpoint
* API format / transformer options
* supported models and model mapping
* priority, weight, retry, sticky policy
* project/profile/API-key access restrictions

### Request Detail

Show:

* channel name
* credential name
* safe key hint
* credential fingerprint prefix only if useful for debugging
* actual model
* cached tokens
* sticky identity kind/reason where available

## Migration Strategy

### Phase 1: Derived Credential Identity

Before full schema migration, derive credential fingerprints from existing inline channel credentials:

* compute credential identity for each key/oauth payload.
* use fingerprint for sticky credential selection and metrics.
* keep secrets stored inline.

Pros:

* lower migration risk.
* can prove cache and quota behavior before schema change.

Cons:

* cannot fully manage credentials globally.
* duplicate secrets still exist in channel config.

### Phase 2: First-Class Credential Table

Create upstream credential entities and channel references:

* migrate inline credentials to credential rows.
* deduplicate same fingerprint across compatible scope.
* replace channel inline APIKeys with refs in runtime.
* preserve old fields for backup/restore compatibility during transition.

### Phase 3: UI And Cleanup

Add Credentials page and channel reference UI. After compatibility period, inline credential arrays become legacy import/export fields rather than the primary editing model.

Channel cleanup target:

* `credentials.apiKey`, `credentials.apiKeys`, inline GCP/Azure/OAuth secrets become import/legacy fallback only.
* `disabled_api_keys` becomes compatibility-only or is replaced by credential/channel-ref scoped failure state.
* channel-local API key round-robin is removed from normal execution.
* provider quota and request observability stop treating channel as the key/account resource.

## Recommended Implementation Order

When implementation begins, do not start with UI. Start with the execution contract:

1. Introduce runtime `CredentialIdentity` / `ExecutionTarget`.
2. Make API key provider return selected credential identity, not only raw key.
3. Persist selected credential fingerprint on request execution.
4. Update sticky-session binding to include credential identity.
5. Update provider quota/failure state to aggregate by credential fingerprint.
6. Add derived channel status from credential states.
7. Add frontend visibility and management.
8. Add full schema migration if derived-identity phase proves insufficient.

If the team wants to do it "once and for all", steps 1-7 belong in the same task family, but the implementation can still land in coherent commits.

## Implemented Derived-Identity Phase

The code implementation follows Phase 1 and intentionally avoids a full credential table migration.

Runtime identity:

* `ChannelCredentialFingerprintForAPIKey(provider, baseURL, apiKey)` derives a deployment-global API-key fingerprint.
* Current code fingerprint scope still includes provider/channel type, normalized base URL, auth kind, and the API key secret. This is now considered an interim implementation detail, not the full-model target.
* Full-model target should migrate fingerprint identity toward `issuer_scope + secret_kind + stable_secret_identity`, so channel baseURL does not split the same key into multiple credentials.
* Fingerprint output is versioned (`cred:v1`) and does not expose raw secret text.
* The current implementation uses SHA-256 truncation because credential identity is derived without injecting `SystemService` or config into channel runtime helpers. If the full credential table is added later, prefer HMAC with the system secret.

Routing:

* `TraceStickyKeyProvider` now stores selected raw key and `credential_fingerprint` in the request context.
* Sticky routing can prefer a bound credential in the current eligible priority tier.
* If the bound channel is no longer eligible but another same-priority channel references the same credential, sticky may switch channels while preserving the credential.
* Sticky does not move to a lower-priority channel solely to keep the same credential.
* Empty enabled-key fallback handles legacy `credentials.apiKey` and does not panic when no key exists.

Persistence and observability:

* `request_executions`, `usage_logs`, and `provider_quota_statuses` have optional `credential_fingerprint` fields.
* Request execution creation stores the selected credential fingerprint.
* Usage logs copy the fingerprint from request execution.
* Performance records carry the selected credential fingerprint for failure handling.

Quota:

* Provider quota service maintains an in-memory credential quota cache in addition to the existing channel cache.
* Multi-key API-key quota-capable providers are checked per deduped credential where supported.
* Channel quota status is derived from credential statuses when credential data is available.
* Candidate filtering, quota scoring, and all-exhausted checks use the same credential-aware channel status path.

Failure handling:

* Credential-scoped auto-disable can disable the same fingerprint across all referencing channels.
* Global credential disable is restricted to clear credential/account statuses: `401`, `402`, `403`.
* Ambiguous failures such as `5xx`, network failures, and generic transient errors remain channel/API-key scoped and do not globally disable shared credentials.

## Full Credential Table Implementation Plan

The next implementation should upgrade derived identity into first-class credential storage while preserving the current derived behavior as the compatibility adapter.

### Step 1: Objects And Ent Schemas

Add objects:

```go
type UpstreamCredentialSecret struct {
    APIKey string
    OAuth *OAuthCredentials
    Azure *AzureCredential
    GCP *GCPCredential
    Extra map[string]any
}
```

Add Ent schemas:

* `internal/ent/schema/upstream_credential.go`
* `internal/ent/schema/channel_credential_ref.go`

Update related schemas:

* `Channel` edge to credential refs.
* `RequestExecution` optional `credential_id` plus existing `credential_fingerprint`.
* `UsageLog` optional `credential_id` plus existing `credential_fingerprint`.
* `ProviderQuotaStatus` optional `credential_id` plus existing `credential_fingerprint`.

Keep `credential_fingerprint` even after adding `credential_id` so historical rows remain queryable if a credential row is deleted or imported later.

If current schemas already contain `provider_type`, `base_url`, or `auth_kind`, migrate them toward:

```text
secret_kind
issuer_scope
key_hint
quota_scope_id
```

`provider_type/base_url/auth_kind` may remain as temporary compatibility fields during migration, but they should not drive the public credential concept or default fingerprint identity.

### Step 2: Migration And Legacy Adapter

Migration algorithm:

```text
for each channel:
  collect legacy credential entries from credentials.apiKey and credentials.apiKeys
  normalize and dedupe apiKey + apiKeys[] entries inside the channel
  skip OAuth JSON legacy apiKey when it belongs to OAuth-only provider
  for each entry:
    compute fingerprint(issuer_scope, secret_kind, stable secret identity)
    find or create UpstreamCredential by fingerprint
    create ChannelCredentialRef(channel_id, credential_id)
```

Important migration correction:

* `credentials.apiKey` and `credentials.apiKeys[]` are compatibility fields that may both contain real API keys.
* If a channel has three real keys plus one stale legacy `apiKey`, migration can produce four credentials unless the collector dedupes by normalized key material.
* Same raw key across multiple channels should become one credential unless `issuer_scope` proves they are different upstream resources.
* Different channel baseURLs alone should not split credentials.

Disabled legacy API keys:

* if disabled because of clear credential failure (`401`, `402`, `403`, billing/account unavailable), mark the credential disabled or cooling.
* if disabled due ambiguous/channel failures, keep channel-local disabled legacy metadata until a dedicated channel-ref status is introduced.

Runtime adapter:

```text
if channel has credential refs:
  use credential refs
else:
  derive temporary credential views from legacy inline credentials
```

This keeps old backups and partially migrated databases working.

### Step 3: Runtime Credential View

Introduce a runtime type:

```go
type ChannelCredentialView struct {
    CredentialID int
    Fingerprint string
    AuthKind string
    Secret objects.UpstreamCredentialSecret
    Enabled bool
    Weight int
    Source string // "ref" or "legacy"
}
```

`biz.Channel` should cache credential views similarly to `cachedEnabledAPIKeys`.

Current API-key helpers become compatibility helpers:

* `EnabledCredentialFingerprints()` reads credential views first.
* `HasEnabledCredentialFingerprint()` reads credential views first.
* API-key provider chooses from credential views, not raw strings.
* Channel-local round-robin over raw `APIKeys` becomes legacy-only. Sticky-enabled execution must select one credential before transformer construction.

### Step 4: Routing And Execution Target

Update execution state:

```go
CurrentCredentialID int
CurrentCredentialFingerprint string
CurrentCredentialAPIKey string
CurrentCredentialName string
CurrentCredentialKeyHint string
PreferredCredentialID int
PreferredCredentialFingerprint string
```

Sticky target:

```go
type StickySessionTarget struct {
    ChannelID int
    CredentialID int
    CredentialFingerprint string
}
```

Selection behavior:

* Bound `channel + credential` wins when both are eligible.
* Bound credential may move to another same-priority eligible channel that references it.
* If credential is unavailable, router may escape within the current priority tier.
* Router never crosses to lower priority only to preserve credential.
* Successful fallback refreshes sticky binding to the successful execution target.

### Step 5: Provider Quota

Add credential-aware quota methods:

```go
CheckCredentialQuota(ctx, credentialView, channelView) (QuotaData, error)
GetCredentialQuotaStatus(credentialID, fingerprint)
```

Providers that need channel config can receive a synthetic channel view that contains:

* channel endpoint/base URL/transform config.
* one selected credential secret.

Channel quota status derivation:

```text
available = any attached enabled credential is available
warning = at least one usable credential is warning
exhausted = all attached credentials are exhausted/disabled/unavailable for request
unknown = no fresh credential data but not known exhausted
```

### Step 6: Failure Handling

Credential state should live on `UpstreamCredential` where the cause is credential-scoped:

* invalid API key
* account disabled
* billing unavailable
* account quota exhausted
* long account/key-level rate limit

Channel/channel-ref state should remain separate:

* base URL unreachable
* API format mismatch
* model mapping/config error
* transformer failure
* network/5xx/circuit breaker

The existing `disabled_api_keys` field remains compatibility-only after migration.

### Step 7: GraphQL/API Contract

Add GraphQL operations:

* query credentials list/detail.
* create credential.
* update credential display fields/status/weight.
* rotate credential secret.
* delete/archive credential.
* attach credential to channel.
* detach credential from channel.
* update channel credential ref enabled/weight override.

Secret rules:

* create/rotate accepts raw secret.
* read/list never returns raw secret.
* frontend can show safe metadata only: name, remark, key hint, status, quota state, referenced channel count, and fingerprint prefix when debugging needs it.

Update channel mutations:

* accept `credentialRefs`.
* keep legacy `credentials` input for import/backward compatibility.
* when both are provided, `credentialRefs` wins and legacy credentials should not be used for runtime.

### Step 8: Frontend Pages

Credentials page:

* dense operations table, not a marketing view.
* columns: name, remark, key hint, status, weight, referenced channels, quota status, remaining/reset, latest error, request count, cached tokens, cost, updated time.
* actions: create, edit, rotate secret, enable/disable, archive, test quota, view channels.

Channel create/edit:

* replace primary key-array editing with credential refs.
* allow attach existing credential.
* allow create credential inline and attach.
* allow detach and per-ref weight override.
* show whether a credential is globally disabled or channel-ref disabled.

Request and usage views:

* show credential name and safe key hint on request list/detail.
* allow filter by credential id/fingerprint.
* keep channel and credential columns separate.

### Step 9: Backup/Restore

Backup should include:

* upstream credentials, with secret payload only when channel secrets are included.
* channel credential refs.
* legacy channel credentials for compatibility if still present.

Restore should:

* import credential rows by fingerprint.
* reattach channel refs.
* fall back to legacy inline credential migration if credential rows are absent.

### Step 10: Test Plan

Backend:

* migration dedupes same issuer scope/secret kind/key into one credential.
* migration dedupes legacy `apiKey` plus `apiKeys[]` duplicates inside the same channel.
* migration does not split the same key only because channel baseURL differs.
* migration does not merge same key across explicitly different issuer scopes.
* channel runtime prefers credential refs over legacy inline credentials.
* legacy-only channel still works.
* sticky target stores credential ID and fingerprint.
* same-priority channel switch can preserve credential.
* lower-priority channel does not win only because it has the credential.
* credential quota status derives channel quota status.
* credential auth failure disables all refs.
* channel 5xx does not disable credential globally.
* request execution and usage log store credential ID/fingerprint and safe snapshot fields.
* GraphQL list/detail never returns raw secret.
* create/rotate accepts secret and then redacts it on read.

Frontend:

* credentials list renders without secrets.
* create credential form submits secret plus display metadata, not channel routing fields.
* rotate secret form does not display existing secret.
* channel edit attaches/detaches credentials.
* request list/detail shows credential name and safe key hint.

Migration/compat:

* existing inline `apiKey` channel migrates.
* existing inline `apiKeys[]` channel migrates.
* backups without credential table still restore and migrate.
* backups with credential table restore without duplicating credentials.

## Testing Strategy

Backend tests:

* same key in two channels deduplicates to one credential fingerprint.
* different keys in one channel produce distinct credential fingerprints.
* sticky identity selects the same credential across repeated requests.
* sticky identity can use the same credential through another eligible same-priority channel.
* credential exhausted in one channel prevents the same credential through another channel.
* credential auth failure disables/cools only the credential, not unrelated credentials in the same channel.
* channel network failure does not globally disable the credential.
* request execution records selected credential fingerprint.
* raw secret never appears in logs or serialized public response.

Frontend tests later:

* credential list does not display raw secret.
* channel edit attaches/detaches credentials.
* request detail shows credential fingerprint/name.

## Risks

### Fingerprint False Merge

If fingerprint scope is too broad, two different upstream resources could merge. Use `issuer_scope`, `secret_kind`, and stable auth identity. For unknown compatible providers, infer or require a coarse issuer scope when needed.

### Fingerprint False Split

If fingerprint includes channel ID, full baseURL, model scope, endpoint path, or mutable label fields, the same key across channels will not deduplicate. Avoid mutable fields and channel routing fields.

### Secret Leakage

Credential identity must be safe to display and log. Use HMAC/fingerprint, never raw key.

### Migration Complexity

Moving secrets out of channel config affects backup/restore, GraphQL types, channel creation, update flows, and transformer construction. A derived-fingerprint phase can reduce risk, but a full first-class resource requires careful migration.

### Provider-Specific Quota Semantics

Some providers expose account quota, some expose project quota, some expose model quota, and some expose no reliable quota. The model should support unknown/partial state rather than forcing all providers into one exact quota shape.

## Open Decisions

### Project/Profile Credential Restrictions

Open question: should project/profile settings be able to restrict specific credentials, or is channel-level restriction enough for the first implementation?

Recommended default: channel-level restrictions are enough for the first implementation. Add credential allowlist/denylist only if operators need to expose the same channel to a project while hiding some credentials attached to that channel.

### Full Table Now Or Derived First

Options:

1. Derived fingerprint first, table later.
2. Full credential table immediately.

Recommended default for "do it once": define the full table model now, but implement with a compatibility adapter that can read legacy inline credentials during migration.

### UI Scope

Options:

1. Only add request-detail visibility first.
2. Add channel credential refs UI.
3. Add full Credentials page plus channel ref UI.

Recommended target: full Credentials page plus channel ref UI, but backend execution identity should land first so the UI is not decorative.
