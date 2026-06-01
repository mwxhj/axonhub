# Credential Quota Clarity And Deletion Design

## Goal

Clarify the credential quota product model before implementation, then use that model to guide three follow-up enhancements: predictable quota reset time behavior, safe credential deletion, and clearer quota presentation across backend APIs and the credentials UI.

The intended product boundary is:

```text
Credential = secret asset + safe display metadata + status
Credential quota scope = local budget and enforcement window
Provider quota status = upstream/provider-observed availability snapshot
Routing availability = whether a channel+credential target may be selected
Deletion = archival/removal policy with history and secret-safety rules
```

## What I Already Know

* The previous credential cleanup task moved the system toward first-class credentials, channel credential refs, quota scopes, and request execution snapshots.
* `CredentialQuotaScope` already stores quota unit, limit amount, used amount, reset policy, reset time, window start, over-limit action, pause time, source, and status.
* `UsageLogService` already increments `CredentialQuotaScope.used_amount` and lazily resets the quota window when `reset_at` has passed.
* `ProviderQuotaStatus` also has `next_reset_at`, but that field represents provider checker observations, not local budget resets.
* `UpstreamCredential` has `SoftDeleteMixin` and a user-facing `archived` status, but there is no product-level delete mutation or frontend delete dialog.
* The frontend currently exposes raw quota fields, but it merges local quota scope status and provider-observed credential quota status into one quota label/badge.

## Problem

The current quota implementation is functional but ambiguous:

* Credential creation currently fails in the product flow and must be fixed before deeper quota/delete enhancements can be safely built.
* "Quota" can mean local budget enforcement, provider-observed quota status, or derived routing availability.
* Credential detail falls back from `quotaScope.status` to `credential.quotaStatus`, which hides whether a status came from Axon local usage accounting or a provider checker.
* Reset policy and reset time are user-editable, but there is no clear default/reset-time rule for daily or monthly scopes.
* Units such as `credit`, `custom`, and `unknown` are selectable even though automatic usage increments currently apply only to tokens, requests, and USD cost.
* There is no first-class credential deletion path. Operators can archive a credential, but not delete it through an explicit action with clear consequences.
* If deletion is added without a clear model, it could break request history, route cache behavior, quota scope references, provider quota rows, and secret-safety expectations.

## Decisions

### Separate The Three Quota Meanings

The product and API should treat these as separate concepts:

```text
Local quota scope
  Axon-controlled budget, usage, reset window, and over-limit behavior.

Provider quota status
  Provider/API-checker observation such as available, warning, exhausted,
  provider next reset time, and provider-specific quota_data.

Routing availability
  Derived answer used by runtime selection:
  can this channel+credential+resource/quota scope be selected now?
```

The UI should not collapse these into a single unlabeled "quota status".

### Local Quota Reset Is A Window Contract

For local quota scopes:

* `reset_policy=none` means no automatic reset.
* `reset_policy=manual` means only explicit operator reset clears usage.
* `reset_policy=daily` means a repeating daily window.
* `reset_policy=monthly` means a repeating monthly window.
* `reset_policy=custom` means a repeating interval derived from `window_started_at -> reset_at`.

`reset_at` should be displayed as "Next local reset", not just "Reset time".

Recommended default behavior:

* When creating a daily scope and `reset_at` is empty, default to the next local midnight converted to UTC.
* When creating a monthly scope and `reset_at` is empty, default to the first day of the next month at local midnight converted to UTC.
* When creating a custom scope, require both `window_started_at` and `reset_at`, with `reset_at > window_started_at`.
* When editing a scope from `none/manual` to `daily/monthly`, fill a default `reset_at` if it is empty.

The backend should still enforce reset semantics server-side, because frontend defaults are not a trust boundary.

### Credential Delete Means Archive First

The first productized deletion path should be "Archive credential", not hard physical deletion.

Archive behavior:

* Set credential status to `archived`.
* Disable or remove active channel credential refs from routing use.
* Reload channel/routing caches.
* Keep request execution and usage log history readable.
* Keep quota scope history and provider quota status rows safe for audit.
* Keep enough safe metadata for historical display: name snapshot, key hint, secret/resource fingerprints, quota scope snapshots.

Hard delete is out of scope for the first implementation unless it is guarded as an owner-only purge action with strict reference checks.

### Secret Wipe Is A Separate Option

Archiving a credential and wiping secret material are different actions.

Recommended MVP:

* Archive keeps the encrypted secret payload by default so restore/re-enable can work.
* A future "Archive and wipe secret" option can clear or replace `secret_payload` only after checking that it will not corrupt runtime assumptions.

This avoids silently destroying recoverability while still removing archived credentials from selection.

### Unit Choices Must Match Accounting Behavior

Automatic local quota accounting should be explicit:

* `token` increments by response total tokens.
* `request` increments by one per accounted usage log.
* `usd` increments by computed total cost when available.
* `credit`, `custom`, and `unknown` do not automatically increment unless a provider-specific mapper exists.

The UI should either hide non-automatic units behind an advanced path or label them as manual/provider-managed.

## Requirements

### Quota Clarity

1. Rename or relabel local budget fields in the UI:
   * "Local quota scope"
   * "Local status"
   * "Used this window"
   * "Limit"
   * "Remaining"
   * "Current window"
   * "Next local reset"
2. Show provider-observed status separately when available:
   * provider status
   * provider next reset
   * provider last checked or next check if available
   * safe provider-specific summary if supported
3. Show routing availability separately:
   * selectable
   * blocked by local quota
   * blocked by provider quota
   * disabled
   * archived
   * no data, treated as selectable
4. Do not show one generic quota badge that hides the source of the status.
5. Display remaining amount as a derived value when both limit and used amount are valid decimals.
6. Display unit behavior clearly, especially for units that do not auto-increment.

### Quota Reset Time

1. Daily and monthly reset policies must have a predictable `reset_at`.
2. The backend must validate or default missing reset times for automatic reset policies.
3. Custom reset policy must require a valid window.
4. Lazy reset on usage log creation remains acceptable, but stale list/detail views should indicate when a reset is due.
5. Runtime selection may treat an expired paused/exhausted scope as selectable if auto-reset is due, but persisted scope state should be reconciled promptly.

### Credential Archive/Delete

1. Add an explicit credential archive/delete product action.
2. The default action should archive, not physically delete.
3. Archived credentials must be excluded from runtime credential selection.
4. Channel credential refs for archived credentials must not remain enabled in runtime views.
5. Request execution and usage logs must continue to resolve safe snapshots after archive.
6. The UI must explain the effect before confirming:
   * credential leaves routing
   * attached channels stop using it
   * history remains visible
   * secret is not shown
7. Hard delete/purge is out of scope unless separately approved.

### Credential Creation Bug

1. Creating a normal API-key credential from the credentials UI must succeed.
2. Creating a credential with no quota scope must succeed.
3. Creating a credential with a new inline quota scope must succeed.
4. Creating a duplicate raw secret must not crash the UI; the backend may return the existing credential, but the frontend should still refresh list/detail state.
5. Any backend error must be specific enough to diagnose whether the failure came from secret validation, quota input, authorization, uniqueness, or GraphQL response shaping.

### GraphQL/API

1. Keep compatibility for existing generated Ent fields.
2. Add product-level mutations instead of relying only on generic status updates:
   * `archiveUpstreamCredential(id: ID!): UpstreamCredential!`
   * optional later: `restoreUpstreamCredential(id: ID!): UpstreamCredential!`
   * optional later: `purgeUpstreamCredential(id: ID!): Boolean!`
3. Prefer explicit computed display fields or frontend selectors for:
   * local quota summary
   * provider quota summary
   * routing availability summary
4. Do not expose raw secret payloads in any new field, copy action, export, tooltip, or log.

### Frontend

1. Credentials table should distinguish:
   * credential status
   * local quota summary
   * provider quota summary
   * attached channels
2. Credential detail quota tab should show a local quota section and provider quota section separately.
3. Create/edit quota form should guide reset policy defaults.
4. Delete/archive action should use a confirmation dialog with precise copy.
5. Existing status dialog may remain, but archive/delete should not be hidden behind a generic status dropdown.

## Acceptance Criteria

* [ ] The documented quota model clearly separates local quota, provider quota, and routing availability.
* [ ] Daily/monthly local quota scopes get a predictable next reset time.
* [ ] Custom reset windows require enough information to compute the next reset.
* [ ] Credentials UI no longer collapses local quota scope status and provider-observed quota status into one ambiguous label.
* [ ] Local quota detail shows limit, used, remaining, unit, current window, next local reset, threshold, and over-limit action.
* [ ] Provider quota detail, when available, is displayed separately from local quota.
* [ ] Normal API-key credential creation succeeds from the frontend flow.
* [ ] Credential creation with no quota and with inline quota are both covered by tests.
* [ ] Archive/delete action is explicit and does not rely only on generic status editing.
* [ ] Archived credentials are excluded from runtime selection and cache state is refreshed.
* [ ] Request execution and usage log history remain readable after credential archive.
* [ ] No raw upstream secret is exposed in GraphQL, logs, UI, copy/export actions, or history views.
* [ ] Tests cover reset defaults, lazy reset, quota status display source, archive routing exclusion, and history preservation.

## Out Of Scope

* Accounting-grade billing or invoices.
* Provider-specific quota accounting for every third-party gateway.
* Hard physical deletion by default.
* Full OAuth account revocation flows.
* Removing legacy channel credential fields.
* Reworking all provider quota checkers unless needed to expose status source clearly.

## Technical Notes

* `CredentialQuotaScope` fields are defined in `internal/ent/schema/credential_quota_scope.go`.
* Local quota accounting and lazy reset live in `internal/server/biz/usage_log.go`.
* Runtime quota selection and aggregation live in `internal/server/orchestrator/quota_status.go`.
* Credential runtime views and quota-selectability checks live in `internal/server/biz/channel_credential_identity.go`.
* Provider quota checking, cache, and persistence live in `internal/server/biz/provider_quota.go`.
* Credential GraphQL mutations are in `internal/server/gql/axonhub.graphql` and `internal/server/gql/axonhub.resolvers.go`.
* Frontend credential quota fields are in `frontend/src/features/credentials/components/credential-quota-fields.tsx`.
* Frontend credential detail currently merges quota statuses in `frontend/src/features/credentials/components/credential-detail-dialog.tsx`.
* Frontend credential actions currently do not include a delete/archive confirmation dialog.
