# Credential Routing Model

> Product and implementation contract for channel-owned routing, credential-owned upstream identity, OAuth migration, and credential-local quota ownership.

---

## 1. Scope / Trigger

Read this spec before changing any of:

- Channel create/update inputs, channel credential fields, channel import/export, or channel credential UI.
- `UpstreamCredential`, `ChannelCredentialRef`, or credential archive/delete/restore behavior.
- OAuth flows for Codex, Claude Code, GitHub Copilot, Antigravity, or future provider-auth flows.
- Credential local quota, route availability, or quota UI.
- Legacy channel credential migration/backfill code.

The durable model is:

```text
Channel = routing configuration
UpstreamCredential = upstream identity and secret material
ChannelCredentialRef = channel-to-credential binding
CredentialQuotaScope = key/credential local budget quota
```

Do not reintroduce channel-local budget quota or channel-level double quota. `CredentialQuotaScope` is the only quota product concept in this model, and it may affect only that credential view. Provider quota is not a product or domain model; provider quota-like upstream responses are runtime attempt errors. If consumer/org/project budgeting is added later, design it outside this channel/credential contract.

---

## 2. Signatures

Database/schema fields with durable meaning:

```text
Channel.type
Channel.base_url
Channel.supported_models
Channel.manual_models
Channel.settings
Channel.endpoints
Channel.status
Channel.ordering_weight
Channel.error_message
```

Channel fields that are compatibility-only after the migration:

```text
Channel.credentials
Channel.disabled_api_keys
```

Credential-owned secret model:

```text
UpstreamCredential.provider_type
UpstreamCredential.base_url
UpstreamCredential.auth_kind
UpstreamCredential.secret_kind
UpstreamCredential.secret_payload
UpstreamCredential.fingerprint
UpstreamCredential.secret_fingerprint
UpstreamCredential.key_hint
UpstreamCredential.status
```

Supported secret payload shapes:

```go
type UpstreamCredentialSecret struct {
    APIKey string
    OAuth  *OAuthCredentials
    Azure  *AzureCredential
    GCP    *GCPCredential
    Extra  map[string]any
}
```

Binding model:

```text
ChannelCredentialRef.channel_id
ChannelCredentialRef.credential_id
ChannelCredentialRef.enabled
ChannelCredentialRef.weight_override
```

GraphQL mutations that remain product-facing:

```graphql
createUpstreamCredential(input: CreateUpstreamCredentialInput!): UpstreamCredential!
updateUpstreamCredential(id: ID!, input: UpdateUpstreamCredentialInput!): UpstreamCredential!
rotateUpstreamCredentialSecret(id: ID!, input: RotateUpstreamCredentialSecretInput!): UpstreamCredential!
archiveUpstreamCredential(id: ID!): UpstreamCredential!
deleteUpstreamCredential(id: ID!): Boolean!
attachCredentialToChannel(input: AttachCredentialToChannelInput!): ChannelCredentialRef!
updateChannelCredentialRef(id: ID!, input: UpdateChannelCredentialRefInput!): ChannelCredentialRef!
detachCredentialFromChannel(channelID: ID!, credentialID: ID!): Boolean!
migrateLegacyChannelCredentials: MigrateLegacyCredentialsPayload!
```

GraphQL inputs that are compatibility-only after the migration:

```graphql
CreateChannelInput.credentials
UpdateChannelInput.credentials
ChannelCredentialsInput.apiKey
ChannelCredentialsInput.apiKeys
ChannelCredentialsInput.oauth
```

Credential/key-local quota APIs are product-facing for credential management:

```graphql
createCredentialQuotaScope
updateCredentialQuotaScope
CreateCredentialQuotaScopeInput
UpdateCredentialQuotaScopeInput
```

These APIs must stay attached to `UpstreamCredential` / key management. They must not become channel quota APIs.

---

## 3. Contracts

- Channel owns route shape only: provider/channel type, base URL, models, endpoint mapping, transform settings, ordering, status, health, and operator notes.
- Channel must not be the durable owner of API keys, OAuth access tokens, refresh tokens, service account JSON, or cloud credentials.
- `UpstreamCredential.secret_payload` is the durable owner of upstream secret material. Raw secret material must stay in sensitive storage and must never be returned by read APIs, logs, tooltips, exports, traces, request records, or UI tables.
- OAuth is a credential secret kind. OAuth data for Codex, Claude Code, GitHub Copilot, Antigravity, and future OAuth providers must be imported into `UpstreamCredential`, not stored as channel inline credentials.
- `ChannelCredentialRef` is the only durable binding layer between a route and upstream credentials.
- Runtime route availability is derived from `channel.status`, `ref.enabled`, `credential.status`, model/route eligibility, and credential/key-local quota state.
- Runtime route availability must not consult channel-local quota. `CredentialQuotaScope` local budget state may filter only the affected credential view. A channel with another eligible credential remains routable.
- `CredentialQuotaScope` local quota is a visible credential/key product concept.
- Provider quota must not exist as a durable product/domain model. Do not expose provider quota UI, route on provider quota state, or treat provider quota observation as local truth.
- Provider quota-like upstream responses such as rate limits, insufficient quota, billing errors, or quota exceeded are runtime attempt errors. Classify them by scope for the current request and record them as execution diagnostics, not as provider quota state.
- Legacy `Channel.credentials` may remain as migration compatibility storage. New product flows must create credentials and refs instead.
- Legacy `Channel.disabled_api_keys` does not need semantic preservation. Backfill may ignore it; migrated refs default to enabled unless the credential itself is inactive for another reason.
- Creating a credential with the same secret as an archived credential should reactivate/update the archived credential instead of returning a still-archived row.
- Archive is reversible: set `UpstreamCredential.status=archived`, preserve refs, and rely on runtime eligibility to exclude the credential.
- Delete is irreversible at the product level: remove refs, soft-delete the credential, reload routing state, and allow the same secret to be recreated.

---

## 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| Channel create/update includes inline credentials during compatibility window | Accept only as compatibility input; migrate/create `UpstreamCredential` + `ChannelCredentialRef` where the flow supports it. Do not make new runtime features depend on inline storage. |
| Channel create/update includes OAuth credentials | Store/import as `UpstreamCredential.secret_kind=oauth`; do not keep OAuth as durable channel-owned identity. |
| `Channel.credentials.apiKey` contains OAuth JSON | Treat it as OAuth secret material during migration. |
| `Channel.credentials.apiKeys` contains multiple keys | Create/reuse one credential per key and bind each credential to the channel. |
| Legacy `Channel.disabled_api_keys` is present | Ignore/drop this compatibility state during credential migration; do not create disabled refs from it. |
| Credential is archived but refs remain enabled | Route eligibility excludes the credential because `credential.status != enabled`. |
| Archived credential is re-enabled | Existing refs can make the route available again without recreating bindings. |
| Credential is deleted | Delete channel refs, soft-delete the credential, and reload channel routing state. |
| Local quota scope is exhausted with action `warn` | Keep the credential routable and surface warning state. |
| Local quota scope is exhausted with action `pause` or `disable` | Filter only that credential view from routing. Keep the channel eligible if another bound credential remains available. |
| Local quota scope is paused and `pause_until` is in the future or absent | Filter only that credential view from routing. |
| Local quota scope is paused and `pause_until` is in the past | Treat the credential view as eligible pending reset/update. |
| Local quota scope is disabled | Filter only that credential view from routing. |
| Local quota reset is due for automatic reset policies | Do not keep the credential blocked only because the stored status has not been refreshed yet. |
| Upstream returns quota/rate-limit/billing style error | Treat it as a runtime attempt error for retry/fallback/error reporting. Do not persist it as provider quota state. |

---

## 5. Good / Base / Bad Cases

- Good: an OAuth flow completes for Codex and creates/updates an OAuth `UpstreamCredential`, then binds it to a Codex channel through `ChannelCredentialRef`.
- Good: a channel has three credential refs; key-local quota pauses one key, and routing still uses the other two available credentials.
- Good: archiving a credential stops routing without deleting refs or request history.
- Good: deleting a credential removes refs and lets the same secret be added again later.
- Base: a legacy channel still has inline `credentials.apiKey`; migration creates a credential/ref and future routing uses the ref.
- Base: a channel has no credential refs and no inline compatibility credentials; routing reports no executable credential.
- Bad: adding new frontend fields that ask for API key or OAuth token inside the channel create/edit dialog.
- Bad: using `CredentialQuotaScope.status` to mark an entire channel unavailable when another bound credential is still usable.
- Bad: storing OAuth JSON in `Channel.credentials.apiKey` after the credential model is available.
- Bad: introducing a second visible quota concept alongside `CredentialQuotaScope`.
- Bad: adding `ProviderQuotaStatus`, provider quota UI, or provider quota routing rules as if upstream quota were local truth.

---

## 6. Tests Required

When changing this contract, add or update tests for:

- Legacy single API key backfill creates/reuses one credential and one channel ref.
- Legacy multi-key backfill creates/reuses one credential per key and one ref per credential.
- Legacy OAuth field backfill creates/reuses an OAuth credential and ref.
- Legacy OAuth JSON stored in `credentials.apiKey` is detected and migrated as OAuth.
- Legacy `disabled_api_keys` is ignored/dropped during migration and does not create disabled refs.
- Runtime credential resolution prefers enabled refs and treats inline channel credentials only as compatibility fallback.
- Archived credential refs are preserved but excluded from runtime routing.
- Deleting a credential removes refs, soft-deletes the credential, and permits same-secret recreation.
- Credential local quota exhaustion/paused/disabled filters only the affected credential view and keeps the channel eligible when another credential remains selectable.
- Credential local quota `warn` action does not filter the credential view.
- Automatic local quota reset due dates allow the credential to be reconsidered instead of staying permanently blocked.
- Provider quota-like upstream responses are stored and displayed as attempt errors, not as provider quota product state.
- Frontend type checks ensure channel create/edit no longer requires or displays key/OAuth fields after migration.
- Request/export/log tests verify raw secret and OAuth token material is masked or absent.

---

## 7. Wrong vs Correct

#### Wrong

```text
Channel.credentials.apiKey / oauth
-> runtime provider chooses key from channel
-> channel UI shows a channel/quota/key state mashup
```

#### Correct

```text
UpstreamCredential.secret_payload
-> ChannelCredentialRef binds credential to channel
-> runtime chooses an eligible credential ref
-> CredentialQuotaScope filters only the affected credential view
-> channel UI explains route status through refs and credential-local quota
```

#### Wrong

```text
CredentialQuotaScope.status = exhausted
-> channel unavailable
```

#### Correct

```text
CredentialQuotaScope.status = exhausted for credential K1 with action pause
-> K1 unavailable
-> channel remains available if another bound credential is ready
```
