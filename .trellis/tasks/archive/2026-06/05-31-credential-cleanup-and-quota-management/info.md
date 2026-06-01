# Credential Cleanup And Quota Management Technical Design

## Design Thesis

Credentials should be boring secret assets. Channels should explain how to use them. Quota scopes should describe which selected execution targets share a budget pool.

The cleanup is necessary because the current implementation made credentials first-class, but still exposed too much channel/provider detail on the credential surface.

Correct runtime shape:

```text
request
-> eligible channels
-> eligible credentials attached to those channels
-> resource scope for each channel+credential pair
-> quota scope availability
-> execution target
-> usage/quota/sticky updates after upstream result
```

## Current State Summary

Current relevant structures:

* `internal/ent/schema/upstream_credential.go`
  * Contains `provider_type`, `base_url`, `auth_kind`, `secret_kind`, `issuer_scope`, `weight`, `quota_scope_id`, `quota_status`.
  * Some of these are now product-model mistakes if exposed as normal credential fields.
* `internal/ent/schema/channel_credential_ref.go`
  * Connects channel and credential.
  * Has `weight_override`, which should not be normal UI in this cleanup.
* `internal/ent/schema/provider_quota_status.go`
  * Still has a unique channel-centric index.
  * Has credential fields but the primary model is not yet quota-scope first.
* `internal/objects/channel.go`
  * `ChannelCredentials` still stores legacy inline API keys and special credential payloads.
* `frontend/src/features/credentials/data/schema.ts`
  * Exposes provider/baseURL/auth-kind/secret-kind/issuer-scope/weight and special secret fields.
* `frontend/src/features/credentials/components/*`
  * Create/edit/rotate dialogs still show implementation details.

## Target Domain Model

### Credential Secret Asset

The existing `UpstreamCredential` Ent type can remain as the persisted table name for compatibility, but the product semantics should become:

```text
Credential
  id
  name
  encrypted secret payload
  secret_fingerprint
  key_hint
  status
  remark
  default quota_scope_id nullable
  latest quota/status summary
  latest safe error
  created_at / updated_at / deleted_at
```

Fields to deprecate from normal behavior:

```text
provider_type
base_url
auth_kind
issuer_scope
weight
```

`secret_kind` remains internal. For the normal create flow it defaults to `api_key`.

#### Fingerprint

Use a secret-only fingerprint for API key dedupe:

```text
secret_fingerprint = HMAC(raw_secret)
```

This differs from the previous provider/issuer-scoped fingerprint. If the existing `fingerprint` field is reused, migration must merge duplicate rows safely before enforcing uniqueness. A safer staged path is:

1. Add `secret_fingerprint`.
2. Backfill it from decryptable secret payloads where available.
3. Use `secret_fingerprint` for duplicate detection and routing identity.
4. Keep existing `fingerprint` as legacy identity until all call sites move.
5. Optionally rename/retire legacy `fingerprint` later.

### Secret Revisions

Full target:

```text
CredentialSecretRevision
  id
  credential_id
  secret_payload
  secret_fingerprint
  key_hint
  secret_kind
  status: active | replaced | archived
  created_at
```

MVP alternative:

* API-key replacement creates a new credential row or rewrites the credential only through an explicit "replace secret" flow.
* If rewriting in place, record enough audit fields to avoid confusing request history.

Recommended implementation for this task: add revision support if migration cost is reasonable; otherwise create a new credential and provide a ref-migration action when replacing the secret.

### ChannelCredentialRef

Target semantics:

```text
ChannelCredentialRef
  channel_id
  credential_id
  enabled
```

`weight_override` may remain in schema for compatibility but should be hidden from normal UI and ignored by new selection logic unless an explicit future weighted-credential feature is designed.

### Resource Scope

Resource scope is runtime identity for "where this secret is being used."

```text
resource_scope_key = normalize(channel_resource_scope) + ":" + secret_fingerprint
```

Channel resource scope rules:

* Official provider channels use stable provider namespaces where known.
* Third-party OpenAI-compatible channels default to normalized base URL host.
* Custom channel configuration may expose an advanced `resource_scope_override` later.
* Explicit quota scope can group budgets even when resource scopes differ.

Persist snapshots on request execution:

```text
credential_id
credential_name_snapshot
credential_key_hint
credential_source
secret_fingerprint
resource_scope_key
quota_scope_id
quota_scope_name_snapshot
actual_model_id
```

Current code already has a partial request-observability path through `RequestExecution` and `UsageLog` fields such as `credentialNameSnapshot`, `credentialKeyHint`, `credentialSource`, and `credentialFingerprint`, and the requests frontend already reads those fields. The cleanup must keep that path, make it more reliable, and extend it to resource/quota scope snapshots.

### Quota Scope

Add a first-class quota scope model.

```text
CredentialQuotaScope
  id
  name
  status: available | warning | exhausted | paused | disabled | unknown
  unit: usd | token | request | credit | custom | unknown
  limit_amount decimal/string
  used_amount decimal/string
  warning_threshold_percent nullable
  reset_policy: none | manual | daily | monthly | custom
  reset_at nullable
  window_started_at nullable
  over_limit_action: warn | pause | disable
  pause_until nullable
  source: local_budget | provider_api | response_error | manual | inferred
  last_error
  remark
  created_at / updated_at / deleted_at
```

Relationship options:

* `UpstreamCredential.quota_scope_id` points to the default quota scope for that credential.
* A later `CredentialResourceScope` table can allow per-resource-scope budget assignment.
* First implementation can use credential default quota scope plus runtime resource scope snapshots.

### ProviderQuotaStatus

Provider-observed quota status should stop being channel-only.

Recommended migration:

* Remove or relax unique `channel_id` as the only identity.
* Add indexes for:
  * `credential_id`
  * `credential_fingerprint` or `secret_fingerprint`
  * `quota_scope_id`
  * `resource_scope_key`
  * `channel_id, credential_id, resource_scope_key`
* Persist provider observations to the most specific known scope.
* Derive channel availability from attached credential/quota states.

## Frontend Design

### Credentials Page

Normal create dialog:

```text
Name
Secret
Status
Remark
Quota section
```

Do not show:

```text
provider type
base URL
issuer scope
auth kind
secret kind
weight
manual OAuth token fields
manual GCP fields
Azure API version
```

Credential table should show:

* name
* key hint
* status
* quota status / used / limit / reset
* attached channel count
* latest safe error
* created/updated

Credential detail should include tabs or sections:

* Overview
* Attached channels
* Quota
* Usage/request history
* Replace secret

### Quota UI

Quota form fields:

```text
Quota scope: default own scope | existing shared scope | create shared scope
Unit
Limit amount
Reset policy
Next reset time when applicable
Warning threshold
Over-limit action
Remark
```

Read-only state:

```text
Used amount
Remaining amount
Status
Last reset
Last error
Source
```

### Request Log UI

Request list/detail should expose the upstream credential used by each execution attempt:

```text
credential display name
safe key hint
credential source: ref | legacy | unknown
resource scope key when available
quota scope name/status when available
```

Required behavior:

* Do not show raw upstream keys.
* In the request list, show the latest execution credential and indicate when retries used multiple credentials.
* In request detail, show the credential for every execution attempt.
* For legacy inline channel credentials, show a safe key hint plus `legacy` source so operators can diagnose pre-migration key rotation.
* Prefer persisted snapshots over live credential joins so old records remain readable after rename/archive/replacement.

### Channel UI

Channel create/edit:

* Remove inline API key/API keys as primary fields.
* Keep attached credentials as the auth configuration path.
* Show legacy inline credential banner if present:

```text
This channel has legacy inline credentials. New configuration should use credential refs.
[Migrate legacy credentials]
```

Channel credentials dialog:

* Attach/detach credentials.
* Enable/disable refs.
* Do not expose weight override by default.
* Show each credential quota status and key hint.

### Special Auth Flows

OAuth:

* Do not expose manual OAuth token paste in normal UI.
* Future UI should be provider-specific "Connect account" / "Reauthorize".

GCP:

* Service account JSON should be introduced only by a channel/provider auth flow that requires it.

Azure:

* Endpoint, deployment, model mapping, and API version belong to channel.
* Secret key remains a credential secret.

## Runtime Design

### Candidate Resolution

High-level flow:

```text
build eligible channels
for each channel:
  resolve credential refs
  if no refs, resolve legacy inline credentials through adapter
  filter disabled/exhausted/paused credentials
  compute resource scope for channel + credential
  compute quota scope
select execution target according to routing policy
execute request
on success/failure:
  update request execution snapshot
  update usage/quota accounting
  update sticky binding after success only
```

### Legacy Adapter

Legacy channel credentials should produce ephemeral credential views:

```text
LegacyCredentialView
  source = legacy_channel_credentials
  channel_id
  secret_payload
  secret_fingerprint
  key_hint
  resource_scope_key
```

Migration action should create real `UpstreamCredential` rows and `ChannelCredentialRef` rows from these views.

### Credential Selection

New selection should not use global credential weight.

For sticky-session:

* Prefer prior execution target when channel + credential + quota scope remain eligible.
* If channel is not eligible but the same credential is attached to another same-priority eligible channel, use that only within the current priority tier.
* If credential/quota is exhausted, escape according to retry/fallback rules.

For non-sticky distribution:

* Keep channel selection as the main load-balancing layer.
* If one selected channel has multiple eligible credentials, choose among them evenly or by future explicit channel-ref policy.
* Do not treat multiple credentials with the same quota scope as independent capacity for quota decisions.

### Quota Accounting

Usage accounting should update:

```text
credential_id when known
secret_fingerprint when known
resource_scope_key when known
quota_scope_id when known
model/operation
tokens/request/money based on available usage data
```

Routing skip rules:

* `disabled` quota scope: skip.
* `paused` with `pause_until` in the future: skip.
* `exhausted` with over-limit action `pause` or `disable`: skip.
* `warning` or over-limit action `warn`: allow but annotate logs/status.
* unknown quota: allow unless provider checker marked the credential unavailable.

## Backend/API Work Areas

Likely backend files:

* `internal/ent/schema/upstream_credential.go`
* `internal/ent/schema/channel_credential_ref.go`
* `internal/ent/schema/provider_quota_status.go`
* new quota scope Ent schema
* `internal/objects/channel.go`
* `internal/server/biz/upstream_credential.go`
* `internal/server/biz/channel_credential_identity.go`
* `internal/server/biz/channel_apikey_provider.go`
* `internal/server/biz/usage_log.go`
* `internal/server/biz/request.go`
* `internal/server/orchestrator/*`
* `internal/server/backup/restore.go`
* `internal/server/gql/*`

Likely frontend files:

* `frontend/src/features/credentials/*`
* `frontend/src/features/channels/components/*credentials*`
* `frontend/src/features/requests/*`
* `frontend/src/locales/*/credentials.json`
* `frontend/src/locales/*/channels.json`
* `frontend/src/locales/*/requests.json`

## Migration Plan

1. Add quota scope schema/API without deleting existing fields.
2. Add secret-only fingerprint/backfill support.
3. Update credential create/edit APIs to accept the simplified product input.
4. Hide deprecated fields from frontend normal flows.
5. Add quota UI and routing/accounting enforcement.
6. Add legacy migration action for inline channel credentials.
7. Update request execution/usage snapshots.
8. Keep legacy read paths and backup/restore compatibility.

## Testing Plan

Backend tests:

* creating a credential stores secret-only fingerprint and key hint safely
* duplicate raw key reuses/detects the same secret identity
* channel refs attach credentials without inline channel key writes
* legacy channel credentials resolve through runtime adapter
* legacy migration creates credentials/refs idempotently
* quota scope creation/update/reset behavior
* quota exhaustion skips only affected credential/resource/quota scope
* sticky-session does not cross channel priority to preserve a credential
* provider quota status can be associated with credential/resource/quota scope
* backup/restore round-trips quota and credential data

Frontend checks:

* credential create/edit does not render deprecated technical fields
* quota controls are visible and submit expected GraphQL input
* channel edit no longer exposes inline key as primary path
* legacy inline credential banner/migration action appears when relevant
* request list/detail safely displays credential, key hint, source, resource scope, and quota snapshots without raw key exposure

## Implementation Notes

* Avoid hard-deleting schema fields during the first cleanup. Hide/deprecate first, then migrate.
* Do not expose raw secret values in logs, GraphQL responses, request snapshots, or frontend state beyond the current form input.
* If a field must remain in GraphQL for compatibility, mark it as advanced/deprecated in frontend usage and avoid using it in new forms.
* Be careful with generated Ent/gqlgen files. Schema changes require regeneration and focused tests.
* The channel priority/sticky-session contract from `.trellis/spec/backend/routing-guidelines.md` still applies.
