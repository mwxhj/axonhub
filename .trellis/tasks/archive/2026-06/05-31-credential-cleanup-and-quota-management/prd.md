# Credential Cleanup And Quota Management

## Goal

Clean up the credential model so credentials are no longer treated as mini channels, then add first-class quota management where operators actually configure and observe budget limits.

The target product boundary is:

```text
Credential = secret asset + display metadata + quota participation
Channel = endpoint/API format/model/routing policy
ChannelCredentialRef = which credentials a channel may use
ResourceScope = where this credential is being used as a real upstream resource
QuotaScope = which credentials/resources share one budget pool
ExecutionTarget = channel + credential + resource scope + model
```

This task continues the credential-aware routing work, but corrects the overexposed provider/baseURL/auth-kind fields that leaked channel concerns into the credential UI.

## Problem

The current credential implementation is useful for routing, but the product model is still too technical and partially wrong:

* The credential create/edit UI still exposes provider-like fields, issuer scope, secret kind, and weight.
* Users can still think of a credential as an OpenAI/Anthropic/provider object, when normal API-key credentials are just secret strings.
* OAuth/GCP/Azure secret shapes are exposed as generic "credential type" choices even when Axon does not have a proper OAuth connection flow for that provider.
* Channel key configuration still exists as a visible path, even though new configuration should attach credentials to channels.
* Credential weight should not exist as a global property. If weighting ever exists, it belongs to a channel/ref policy, not the secret asset itself.
* Quota has status fields and reserved IDs, but there is no clear UI or data model for operators to set budgets, reset windows, over-limit behavior, or shared quota pools.
* Same raw key should dedupe as the same secret asset, but quota/cache/sticky behavior also depends on where that key is used.

## Decisions

### Credential Is A Secret Asset

For the normal product path, credential creation should ask only for:

* name
* secret string
* remark
* enabled/disabled status
* optional quota configuration or quota scope assignment

Credential creation must not ask users to choose:

* provider type
* OpenAI vs Anthropic vs compatible source
* base URL
* API format
* issuer scope
* secret kind
* credential weight

Those are channel/runtime concerns, not credential concerns.

### Channel Explains How To Use A Credential

Channel remains responsible for:

* base URL / endpoint
* API format
* provider/channel type
* model support and model mapping
* priority and retry/fallback policy
* sticky-session policy
* access restrictions
* auth placement rules such as Authorization header vs provider-specific auth

New channel writes should no longer put API keys into `channel.credentials.apiKey` or `channel.credentials.apiKeys`.

Legacy channel credentials remain readable for compatibility and migration, but should not remain the normal UI path.

### Secret Identity And Resource Identity Are Different

Same raw key globally means the same secret asset:

```text
secret_fingerprint = HMAC(raw_secret)
```

This fingerprint is only the answer to "is this the same secret string?"

Quota/cache/sticky behavior depends on the resource scope where the secret is used:

```text
resource_scope = channel_resource_scope + secret_fingerprint
```

Default channel resource scopes:

* official OpenAI channel: `openai`
* official Anthropic channel: `anthropic`
* official Google/Azure channel: provider/account/resource namespace when available
* OpenAI-compatible or unknown third-party channel: normalized channel base URL host
* manually shared pool: explicit quota scope overrides the default budget grouping

This avoids forcing two different third-party endpoints into one quota/cache identity just because the operator reused the same local token string.

### OAuth/GCP/Azure Are Not Generic User Choices

The normal global credential creation flow is API-key first.

Special credential shapes should appear only when there is a real product flow:

* OAuth should be "Connect account" / "Reauthorize", backed by a provider-specific OAuth flow. Users should not paste tokens captured from browser/devtools.
* GCP service account JSON should appear only when a channel/provider requires that auth style.
* Azure endpoint/API version/model deployment remain channel fields. Azure key material can still be a credential secret.
* "Other" is not a normal user-facing option.

The internal `secret_kind` can continue to exist for backend serialization and legacy support, but it should not be a main create-form choice.

### Quota Is First-Class

Operators must be able to configure quota/budget policy on credentials or shared quota scopes.

Quota requirements:

* quota unit: money, token, request, credit, or custom/unknown
* limit amount
* used amount or usage rollup source
* reset policy: none/manual/daily/monthly/custom window
* next reset time
* warning threshold
* over-limit action: warn only, pause/cooldown, disable
* quota scope assignment so multiple credentials/resources can share one budget pool
* display current status on credential list/detail
* derived channel availability from attached credential quota states

Quota must be able to represent both:

* local operator budgets calculated from Axon usage logs
* provider-observed quota status when a provider checker exists

## Requirements

### Credential UI

1. Credential create/edit should focus on name, secret, remark, status, and quota.
2. Provider type, base URL, issuer scope, auth kind/secret kind, and weight should not be exposed in the normal create/edit path.
3. Credential list/detail should still show safe operational hints:
   * name
   * key hint
   * enabled/disabled status
   * attached channels
   * quota status
   * quota scope/budget summary
   * latest safe error
4. Secret rotation should be renamed by user-facing behavior:
   * API key: replace secret
   * OAuth: reauthorize
   * GCP: replace service account JSON
   * Azure: replace key
5. Replacing an API key must not silently mutate identity semantics. If the raw secret changes, the backend must either create a new secret revision or a new credential identity and preserve/migrate refs explicitly.

### Channel UI

1. Channel create/edit should no longer present API key/API keys as normal fields.
2. Channel auth configuration should attach existing credentials through channel credential refs.
3. Existing channels with legacy inline credentials should show a legacy warning and a migration action.
4. New writes should not create fresh inline channel credentials except where required for backward compatibility.
5. The attached-credentials UI should not expose credential weight by default.

### Runtime Routing

1. Runtime execution must select an execution target as `channel + credential + resource scope + model`.
2. Candidate channel selection remains the outer routing layer.
3. Credential selection must not bypass channel eligibility, priority, model support, profile restrictions, or retry/fallback policy.
4. Sticky-session affinity may prefer the same credential/resource scope inside the currently eligible channel tier.
5. Sticky-session must not cross to lower-priority channels only to preserve a credential/resource scope.
6. Resource scope should be recorded on request execution/usage logs for observability and future cache/quota analysis.

### Request Observability

1. Request list and request detail must show which upstream credential was used for each execution attempt.
2. The UI must show operator-safe identity only:
   * credential display name
   * key hint
   * credential/source marker such as `ref` or `legacy`
   * resource scope / quota scope when available
3. The UI must never expose the raw upstream key in request records, GraphQL responses, logs, tooltips, copied text, or exports.
4. When a request retries across multiple executions, the retry detail must show the credential used by each attempt, not only the final request-level channel.
5. Legacy inline channel credentials must still show a safe key hint and `legacy` source so operators can diagnose multi-key rotation before migration.
6. Request records should keep snapshots so old records remain readable after a credential is renamed, replaced, archived, or migrated.

### Quota

1. Credential detail must include a quota management surface.
2. Operators must be able to set local budgets and over-limit behavior.
3. Operators must be able to put multiple credentials/resources into a shared quota scope.
4. Requests must account usage toward the selected credential/resource/quota scope when a credential is known.
5. Routing must treat exhausted/paused/disabled quota scopes as unavailable for the relevant execution target.
6. Channel availability must be derived from attached credential availability where possible.
7. Provider quota checker output should update credential/resource/quota status rather than only channel status.

### Compatibility

1. Existing `channel.credentials.apiKey/apiKeys/oauth/azure/gcp` data remains readable.
2. Backup/restore must preserve both legacy credentials and new credential/quota structures.
3. Legacy inline channel credentials should be mapped into runtime credential views until migrated.
4. Existing request records should remain readable even if they lack credential/resource/quota fields.
5. Public API compatibility should be preserved where practical, but new UI should stop encouraging legacy writes.

## Acceptance Criteria

* [ ] Credential create/edit no longer asks users for provider type, base URL, issuer scope, secret kind, or weight.
* [ ] Credential create/edit includes quota configuration or quota scope assignment.
* [ ] Credential list/detail shows quota status and attached channel information without exposing raw secrets.
* [ ] Channel create/edit no longer uses inline API key fields as the primary path.
* [ ] Legacy inline channel credentials are still supported and can be migrated into credential refs.
* [ ] Same raw API key dedupes to one secret identity without relying on channel/provider fields.
* [ ] Runtime computes resource scope from selected channel + credential secret identity.
* [ ] Sticky-session and routing continue to respect channel priority and eligibility.
* [ ] Quota exhaustion can prevent selection of the affected credential/quota scope without disabling unrelated credentials.
* [ ] Request list/detail identify the selected credential/key hint/source for every execution attempt without exposing raw secrets.
* [ ] Request execution and usage logs identify the selected credential/resource/quota scope safely.
* [ ] Backup/restore covers new quota and credential fields.
* [ ] Tests cover credential creation, duplicate key handling, channel credential attachment, legacy migration, quota accounting, and routing skip behavior.

## Out Of Scope

* Implementing a full provider OAuth authorization flow for every upstream provider.
* Removing legacy channel credential fields from persisted data immediately.
* Guaranteeing provider-side prompt-cache hit detection when providers do not return cache telemetry.
* Solving provider account identity discovery for every OpenAI-compatible gateway.
* Building complex quota billing invoices. This task needs routing/budget controls, not accounting-grade finance.

## Technical Notes

* Current `UpstreamCredential` schema still contains provider/baseURL/auth-kind/secret-kind/issuer-scope/weight fields. These should be treated as compatibility/internal fields, not normal UI fields.
* Current `ChannelCredentialRef` contains `weight_override`; this should be hidden and not used as a normal UI control for the cleanup scope.
* Current `ProviderQuotaStatus` is still uniquely keyed by `channel_id`; quota design needs a credential/resource/quota-scope path.
* Current credential frontend schema and form utilities still expose provider type, auth kind, issuer scope, OAuth fields, GCP fields, Azure fields, and weight.
* Current request execution schema and frontend already contain a partial credential display path (`credentialNameSnapshot`, `credentialKeyHint`, `credentialSource`, `credentialFingerprint`). This task must preserve it, make the behavior explicit, and extend it with resource/quota scope instead of raw key exposure.
* Previous task reference: `.trellis/tasks/archive/2026-05/05-31-credential-aware-routing-quota/`.
