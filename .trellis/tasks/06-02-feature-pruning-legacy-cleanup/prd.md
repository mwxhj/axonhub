# Feature Pruning and Legacy Surface Cleanup

## Goal

Remove or retire legacy product surfaces that duplicate the credential-owned routing model. The immediate product direction is: channels describe routing and transformation policy, while upstream credentials own API keys, OAuth tokens, cloud credentials, local key quota, and provider quota observation.

This task now has a locked implementation slice: complete the channel-owned credential deletion path in one coordinated pass across product surface, write paths, runtime fallback, and final dead-storage cleanup planning. The four locked work items are:

1. remove channel-side raw key management product surfaces
2. close `channel.credentials` write paths
3. hard-cut the remaining runtime legacy fallback paths
4. prepare the final schema dead-storage removal path after migration neutralization

## What I Already Know

* The user wants a large deletion pass, not more feature accretion.
* Existing backend specs already forbid reintroducing channel-owned key/OAuth storage or channel-local quota.
* `Channel` still persists sensitive `credentials` and `disabled_api_keys` JSON fields.
* `CreateChannelInput` still requires inline `credentials`, and `UpdateChannelInput` still accepts inline `credentials`.
* Channel create/update still persists `input.Credentials` when supplied.
* `UpstreamCredential` already has the durable ownership fields for upstream identity: `secret_payload`, `auth_kind`, `secret_kind`, `quota_scope_id`, `status`, `weight`, and safe fingerprints.
* `ChannelCredentialRef` is already the intended channel-to-credential binding layer.
* The channel UI has a credential binding dialog, but the channel create/edit dialog still carries hidden/dead inline credential and OAuth code paths.
* Runtime routing resolves credential refs first, but still falls back to normalized inline channel credentials.
* Legacy migration creates/reuses credentials and refs from inline channel credentials, but the initial scan did not find evidence that it clears the old inline blob after migration.
* Current retry settings still expose older retry-centric controls that partially overlap with the new sticky-session fallback direction.

## Assumptions

* We should not delete database columns in the first implementation slice unless the corresponding data has already been migrated or made irrelevant at runtime.
* Backward compatibility should be treated as a temporary migration aid, not a permanent product surface.
* Credential local quota should stay; channel-level local quota should not exist.
* Provider quota should remain credential/key-oriented, with `ProviderQuotaStatus.channel_id` treated only as compatibility or last-observed routing metadata.
* Retry should not be deleted outright yet. It should be narrowed to same-target/transient retry semantics while fallback owns target escape.

## Legacy Surface Inventory

Detailed inventory lives in [`research/legacy-surface-inventory.md`](research/legacy-surface-inventory.md).

High-confidence removal candidates:

* Channel inline secret storage and product inputs:
  * generated `CreateChannelInput.credentials`
  * generated `UpdateChannelInput.credentials`
  * `Channel.credentials.apiKey`
  * `Channel.credentials.apiKeys`
  * `Channel.credentials.oauth`
  * UI code that writes OAuth results into `credentials.apiKey`
* Channel disabled API-key management:
  * `disabled_api_keys`
  * `disableChannelAPIKey`
  * `enableChannelAPIKey`
  * `enableAllChannelAPIKeys`
  * `enableSelectedChannelAPIKeys`
  * `deleteDisabledChannelAPIKeys`
  * disabled API-key dialogs/actions
* Legacy channel API-key testing:
  * `testChannelAPIKeys`
  * frontend table/dialog actions that test raw channel key arrays
* Compatibility fields exposed through GraphQL:
  * `Channel.credentials`
  * `Channel.disabledAPIKeys`
  * `ChannelCredentialsInput`
  * raw OAuth credential GraphQL object fields if they can expose token material
* Dead frontend inline credential code:
  * `showChannelInlineCredentialUI = false`
  * `shouldShowLegacyInlineAPIKeys = false`
  * inactive UI blocks that still reference channel inline key state

Candidates to refactor rather than delete immediately:

* OAuth start/exchange endpoints should remain, but successful flows should create or rotate `UpstreamCredential` records instead of returning strings meant for `Channel.credentials.apiKey`.
* Provider quota checkers may still accept an `ent.Channel` temporarily, but should receive credential-scoped execution material or a channel clone built from selected credential context until the checker interface is refactored.
* Retry policy should remain as a compatibility config, but the UI and docs should be simplified around fallback semantics.

## Requirements

### Product Model

* Channels must not ask the operator for API keys, OAuth tokens, service account JSON, or cloud secret material.
* Channels may only attach, detach, enable, or disable credential refs.
* Credentials must be the only product surface for creating, rotating, archiving, deleting, quota-setting, and OAuth-importing upstream identity.
* OAuth providers such as Codex, Claude Code, GitHub Copilot, and Antigravity must import into credentials instead of channel inline credentials.
* Channel-local API-key disable/delete/test flows must be deleted, not re-skinned as another raw-key management surface.
* Channel list/detail actions must not expose raw channel key management once the slice lands.

### Migration Safety

* Existing channel inline credentials must be migrated to credentials and refs before the old write path is rejected.
* Migration should clear or neutralize the legacy inline blob once refs are created, unless a deliberate read-only audit window is chosen.
* Legacy `disabled_api_keys` does not need semantic preservation after migration; existing specs already allow ignoring it.
* Runtime routing must stop depending on channel-owned key arrays for new data by the end of this slice.
* Any deletion slice must keep existing enabled channels routable after migration.

### API Surface

* Remove or deprecate GraphQL mutations that manage channel-owned API keys.
* Remove channel-side test-key product mutations and related frontend calls.
* Remove raw channel credential fields from frontend queries first, then from GraphQL schema once no frontend consumer depends on them.
* Do not expose raw OAuth token fields through admin GraphQL or frontend data objects.
* Keep safe identity fields such as credential ID, credential name, key hint, fingerprint, secret fingerprint, and quota status.

### Frontend

* Channel create/edit should not render hidden inline key/OAuth controls or keep dead code paths that write into channel credentials.
* Credential binding should be the only channel-side identity management UX.
* Credential create/edit should become the entry point for API key, OAuth, Azure, GCP, local quota, and provider quota visibility.
* Retry/fallback UI should stop presenting retry as the primary reliability mechanism for sticky sessions.

### Runtime

* Outbound transformers may still consume legacy `ChannelCredentials` internally during an adapter transition, but selected credential data must be the source.
* Same-channel credential fallback must use credential refs and request-scoped credential exclusion, not channel `apiKeys` arrays.
* Provider quota and local quota filtering must remain credential-scoped.
* Request execution, usage log, copy/export, and diagnostics must continue masking raw secrets.
* OAuth refresh or exchange completion must never write refreshed tokens back into `Channel.credentials`.

### Slice Boundary

This implementation slice must land the following together:

* delete channel raw-key management UI and GraphQL mutations
* stop channel create/update from persisting inline credentials
* stop OAuth completion flows from filling `Channel.credentials.apiKey`
* remove runtime fallback readers that still prefer `Channel.credentials` when credential refs are absent
* leave physical schema-column deletion for the follow-up cut only after migrated rows are neutralized and verified

## Acceptance Criteria

* [ ] A complete deletion inventory identifies user-facing, API, schema, runtime, tests, and docs surfaces for channel-owned credentials.
* [ ] The first implementation slice removes channel create/edit inline key and OAuth product paths from the frontend.
* [ ] The first implementation slice prevents new channel inline secret writes from GraphQL/REST paths or transparently converts them to credentials.
* [ ] Existing legacy inline credentials can be migrated to credentials/refs before compatibility removal.
* [ ] Channel disabled API-key UX and GraphQL mutations are removed or replaced by credential/ref status operations.
* [ ] OAuth import creates/updates `UpstreamCredential` instead of filling `Channel.credentials.apiKey`.
* [ ] Runtime tests prove routing still works through credential refs and no longer depends on channel-owned key arrays for new data.
* [ ] Secret masking tests cover OAuth and API key copy/export/request surfaces after cleanup.
* [ ] A follow-up-safe dead-storage removal path is documented for `Channel.credentials` and `disabled_api_keys` after migration neutralization.

## Definition of Done

* Tests added or updated for migration, routing, OAuth import, GraphQL/API rejection, and frontend data shape where appropriate.
* Relevant Go package tests pass for changed backend areas.
* Relevant frontend typecheck/tests pass for changed frontend areas.
* `git diff --check` passes.
* Trellis specs updated if the cleanup discovers new permanent rules.

## Out of Scope

* Removing credential local quota.
* Removing provider quota.
* Removing sticky-session fallback.
* Removing all retry code in one pass.
* Physically dropping database columns in this same slice before a verified migration/removal sequence exists.
* Redesigning consumer/project/org budget quota.

## Technical Notes

* `internal/ent/schema/channel.go` still defines sensitive `credentials` and `disabled_api_keys`.
* `internal/objects/channel.go` still defines `ChannelCredentials`, `DisabledAPIKey`, compatibility OAuth detection from `apiKey`, and API-key array helpers.
* `internal/objects/upstream_credential.go` converts credential secrets back into legacy channel credentials for transformer compatibility.
* `frontend/src/features/channels/components/channels-action-dialog.tsx` has `showChannelInlineCredentialUI = false` and still contains inactive OAuth/key flows.
* `frontend/src/features/channels/components/channels-credentials-dialog.tsx` already exposes the intended credential binding UX and legacy migration action.
* `internal/server/biz/channel.go` persists channel create/update `Credentials` today.
* `internal/server/biz/channel_llm.go` and `internal/server/biz/channel_credential_identity.go` still keep inline credentials as runtime fallback.
* `internal/server/biz/upstream_credential.go` contains `MigrateLegacyChannelCredentials`; first scan suggests it migrates refs without clearing the old inline credential blob.
* `internal/server/gql/axonhub.graphql` still exposes channel credential and disabled API-key types/mutations.
* `internal/server/biz/system.go` still models retry as `RetryPolicy`, including max channel retries, max single-channel retries, retry delay, load balancer strategy, and auto-disable channel settings.

## Locked Decisions

* This task implements channel-owned credential cleanup first; retry/fallback UX simplification is not part of this slice.
* The four requested work items land together in one implementation slice.
* Physical schema removal of `Channel.credentials` / `disabled_api_keys` is deferred until migrated rows are neutralized and verified, but all product/runtime writes and primary reads are cut in this slice.
