# Credential Quota Clarity And Deletion Technical Notes

## Current Code Facts

### Local Quota Scope

`CredentialQuotaScope` is already the local budget/enforcement model.

Relevant fields:

```text
status
unit
limit_amount
used_amount
warning_threshold_percent
reset_policy
reset_at
window_started_at
over_limit_action
pause_until
source
last_error
remark
```

Important behavior:

* `UsageLogService.CreateUsageLog` increments the selected quota scope after a usage log is saved.
* Increment depends on unit:
  * `token` -> `usage.TotalTokens`
  * `request` -> `1`
  * `usd` -> `totalCost`
  * other units -> no automatic increment
* Reset is lazy. It runs when usage is accounted and `reset_at <= now`.
* Daily and monthly reset policies advance `reset_at` by calendar days/months.
* Custom reset policy derives the next interval from `window_started_at` to `reset_at`.

Implication:

The backend already has a reasonable reset engine, but it needs better defaults, validation, and UI language.

### Provider Quota Status

`ProviderQuotaStatus` stores provider checker observations.

Relevant fields:

```text
channel_id
scope_key
credential_id
credential_fingerprint
secret_fingerprint
resource_scope_key
quota_scope_id
provider_type
status
quota_data
next_reset_at
ready
next_check_at
```

Important behavior:

* `ProviderQuotaService` periodically checks enabled channels.
* It stores quota observations by a `scope_key` that can represent channel, credential, secret, resource, or quota scope identity.
* It also updates `UpstreamCredential.quota_status` and `CredentialQuotaScope.status` from provider observations when the target is known.
* `next_reset_at` means provider-observed reset, not local budget reset.

Implication:

Provider quota and local quota can share a status vocabulary but must not share one unlabeled UI badge.

### Runtime Quota Selection

Runtime selection uses layered lookup:

```text
quota scope status
-> resource scope status
-> credential id status
-> credential fingerprint status
-> channel status fallback
```

When aggregating multiple credential statuses for a channel, the code currently prefers warning, then available, then unknown, then exhausted.

Local quota selectability also checks `CredentialQuotaScope` snapshots in `ChannelCredentialView`:

* `disabled` quota scope blocks selection.
* `paused` blocks selection until `pause_until` has passed.
* `exhausted` blocks selection when over-limit action is `pause` or `disable`.
* `exhausted` with action `warn` remains selectable.
* If auto-reset is due, the view is treated as selectable.

Implication:

Routing behavior is already more advanced than the UI explains. The implementation work should focus on making this visible and less ambiguous.

### Credential Archive/Delete

`UpstreamCredential` already has:

```text
SoftDeleteMixin
status: enabled | disabled | archived
secret_payload
fingerprint
secret_fingerprint
quota_scope_id
quota_status
```

Current product/API surface:

* There is create/update/status/rotate.
* There is attach/detach channel credential ref.
* There is no explicit delete/archive mutation.
* The frontend action menu opens a generic status dialog rather than a deletion confirmation.

Important behavior:

* Runtime credential views are enabled only when both the ref is enabled and the credential status is `enabled`.
* Rotation can create a replacement credential, migrate refs, disable old refs, and archive the old credential.
* Backfill can merge duplicate credentials by secret fingerprint and archive duplicates.

Implication:

Archive is already a supported internal state. The missing part is a productized archive/delete action with explicit side effects and tests.

## Proposed Design

### Terminology

Use these labels consistently:

```text
Credential status
  enabled, disabled, archived

Local quota scope
  Axon-managed local budget.

Local quota status
  available, warning, exhausted, paused, disabled, unknown.

Provider quota status
  Provider/API checker observation.

Routing availability
  Derived selection state for runtime routing.

Next local reset
  CredentialQuotaScope.reset_at.

Provider next reset
  ProviderQuotaStatus.next_reset_at.
```

Avoid using plain "quota status" when the source is not obvious.

### Local Quota Summary

The UI should derive a summary object from `CredentialQuotaScope`:

```text
unit
limit
used
remaining
used_percent
status
source
window_started_at
next_local_reset_at
reset_policy
warning_threshold_percent
over_limit_action
pause_until
auto_increment_mode
```

`remaining` and `used_percent` should be computed only when `limit_amount` and `used_amount` parse as valid positive decimals.

`auto_increment_mode`:

```text
token -> automatic from usage total tokens
request -> automatic per usage log
usd -> automatic when cost is available
credit/custom/unknown -> manual/provider-managed
```

### Provider Quota Summary

The UI should derive a separate provider summary from `ProviderQuotaStatus` or the credential-level cached observation:

```text
provider_type
status
ready
next_provider_reset_at
next_check_at
last_observed_at
scope_kind
scope_key
limits
safe_message
```

If this data is not available, show "No provider quota check data" instead of replacing it with local quota status.

### Routing Availability Summary

Routing availability should be displayed as a derived state:

```text
selectable
blocked_by_credential_status
blocked_by_local_quota
blocked_by_provider_quota
blocked_by_disabled_ref
no_quota_data
```

This summary can initially be frontend-derived from fields already loaded in credential detail. A backend resolver can be added later if the logic becomes too complex.

### Reset Defaults

Backend helper should normalize quota inputs:

```text
reset_policy=none
  reset_at may be null.

reset_policy=manual
  reset_at may be null.

reset_policy=daily
  if reset_at is null, set next midnight in the configured operator timezone or UTC.

reset_policy=monthly
  if reset_at is null, set first day of next month at midnight in the configured operator timezone or UTC.

reset_policy=custom
  require window_started_at and reset_at.
  require reset_at > window_started_at.
```

Timezone note:

The existing code stores `time.Time` and scheduled provider checks use UTC. The MVP should store UTC. Frontend may display local time.

### Credential Archive Action

Recommended mutation:

```graphql
archiveUpstreamCredential(id: ID!, wipeSecret: Boolean = false): UpstreamCredential!
```

MVP behavior with `wipeSecret=false`:

1. Load credential with refs.
2. Set credential status to `archived`.
3. Disable all enabled `ChannelCredentialRef` rows pointing to it.
4. Clear live quota/provider caches for that credential if needed, or reload channels and provider quota cache.
5. Return the archived credential.

Potential future behavior with `wipeSecret=true`:

1. Archive credential.
2. Replace `secret_payload` with an empty internal payload.
3. Preserve `key_hint`, `secret_fingerprint`, and historical snapshots.

Do not implement hard physical delete in the MVP.

### Frontend Archive Dialog

Dialog copy should state:

```text
Archive this credential?

This removes the credential from routing and disables its channel attachments.
Historical request and usage records remain visible. The secret is not exposed.
```

Actions:

```text
Cancel
Archive
```

An optional checkbox can be added later:

```text
Also wipe stored secret material
```

Do not put archive behind only the generic status dropdown. Keep the status dialog if useful, but archive should be an explicit action.

## Implementation Order

1. Add local quota summary helpers in frontend or backend.
2. Rename/restructure credential quota UI labels to distinguish local quota and provider quota.
3. Add reset defaulting/validation in backend quota input application.
4. Add frontend reset policy guidance and default reset time filling.
5. Add explicit credential archive mutation and service method.
6. Add archive confirmation dialog and action menu entry.
7. Add tests for reset defaults, lazy reset, archive behavior, routing exclusion, and history preservation.

## Testing Plan

Backend tests:

* GraphQL `createUpstreamCredential` mutation succeeds with the frontend field selection, both without quota and with inline quota.
* Creating a daily quota without reset time sets a next reset.
* Creating a monthly quota without reset time sets a next reset.
* Creating custom quota without a valid window fails.
* Usage after an expired reset window resets used amount before incrementing.
* Archiving a credential changes status to archived.
* Archiving disables enabled channel refs or makes them runtime-unselectable.
* Archived credential request history still resolves safe snapshots.

Frontend tests or component-level checks:

* Credential quota detail shows local quota and provider quota separately.
* Remaining amount is displayed when limit/used are valid.
* Non-automatic units are labeled as manual/provider-managed.
* Archive action opens a confirmation dialog.
* Archive success invalidates credentials, channels, and quota queries.

## Risks

* Local quota status and provider quota status may fight if both write `CredentialQuotaScope.status`. The UI split helps, but backend ownership may need a future refinement.
* Treating auto-reset-due scopes as selectable before persistence can make list state briefly stale.
* Wiping secret material could break restore/re-enable behavior if done too early.
* Hard delete would require careful handling of request execution, usage logs, provider quota status rows, and channel refs.

## Debug Note: Credential Creation Failure

The observed credential creation failure was in GraphQL response shaping, not in `UpstreamCredentialService.CreateUpstreamCredential`.

The frontend create mutation requests `quotaScope` and `channelRefs` on the returned `UpstreamCredential`. The GraphQL server wraps mutations with `entgql.Transactioner`. Nullable association resolvers that query `Channel`, `UpstreamCredential`, or `CredentialQuotaScope` must use the Ent client from `ent.FromContext(ctx)` when present, because that is the transaction client. Using the root resolver client inside the same mutation response can block under SQLite test/runtime connection constraints while the transaction is still open.

Regression coverage should exercise the full GraphQL mutation handler with the frontend-like selection set, not only the service method.

## Open Decision

Should the first archive mutation preserve encrypted secret material, or should it include an owner-only `wipeSecret` option from the start?

Recommendation: preserve by default and do not expose wipe in the first UI iteration.

## Implementation Note: Reset Defaults, Archive, And Quota UI Split

Implemented behavior:

* Backend quota input normalization now defaults missing daily/monthly reset times and validates custom windows.
* Daily/monthly defaults use the next local midnight / next local month start, stored as UTC.
* Custom local quota reset requires `window_started_at` and `reset_at`, with `reset_at > window_started_at`.
* `archiveUpstreamCredential(id: ID!): UpstreamCredential!` archives the credential and disables enabled `ChannelCredentialRef` rows in the same logical transaction.
* Archive preserves encrypted secret material and history snapshots; hard delete / wipe secret remains out of scope.
* Frontend credential queries now fetch `providerQuotaStatuses` separately from `quotaScope`.
* Credential detail now splits:
  * local quota scope
  * provider quota status
  * derived routing availability
* Credential list now has separate local quota and provider quota columns.
* Archive is an explicit confirmation dialog/action, no longer only a generic status edit.

Verification run:

```text
go test ./internal/server/biz -run 'TestNormalizeCreateCredentialQuotaScopeInputDefaultsDailyAndMonthlyResetAt|TestUpstreamCredentialService_CreateCredentialQuotaScopeValidatesCustomResetWindow|TestUpstreamCredentialService_UpdateCredentialQuotaScopeDefaultsResetAtWhenPolicyBecomesAutomatic|TestUpstreamCredentialService_ArchiveDisablesRefsAndRuntimeSelection|TestUpstreamCredentialService_CreateAndUpdateInlineQuotaScope' -timeout 60s
go test ./internal/server/gql -run 'TestGraphQLCreateUpstreamCredentialMutation|TestGraphQLArchiveUpstreamCredentialMutation' -timeout 60s
pnpm exec tsc --noEmit --pretty false
```
