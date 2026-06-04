# Legacy Surface Inventory

## Summary

The codebase has already moved toward a credential-owned model, but channel-owned credential compatibility remains spread across schema, runtime, GraphQL, and frontend UI. A large deletion pass is justified because two product models are still visible:

* Old model: channel owns API keys, OAuth JSON, disabled API-key state, and channel-key testing.
* New model: `UpstreamCredential` owns secret material, quota scope, provider quota status, and channel refs.

The cleanup should remove the old model from product surfaces first, then collapse backend compatibility once migration and runtime adapters are stable.

## Channel-Owned Credential Storage

Current surfaces:

* `internal/ent/schema/channel.go`
  * `credentials` is a sensitive JSON field of `objects.ChannelCredentials`.
  * `disabled_api_keys` is a sensitive JSON field of `[]objects.DisabledAPIKey`.
* `internal/ent/gql_mutation_input.go`
  * `CreateChannelInput` still includes inline `credentials`.
  * `UpdateChannelInput` still accepts inline `credentials`.
* `internal/objects/channel.go`
  * `DisabledAPIKey` stores raw key text as its identity.
  * `ChannelCredentials` stores `APIKey`, `OAuth`, `APIKeys`, `Azure`, and `GCP`.
  * `GetAllAPIKeys` merges legacy single key and newer key array.
  * `GetEnabledAPIKeys` filters by `disabled_api_keys`.
  * `IsOAuth` treats both `credentials.oauth` and OAuth JSON stored in `credentials.apiKey` as OAuth.
* `internal/objects/upstream_credential.go`
  * `UpstreamCredentialSecret.ToChannelCredentials` converts credential secrets back into the legacy channel credential shape for existing transformer compatibility.
* `internal/server/biz/channel.go`
  * Channel create persists `input.Credentials`.
  * Channel update persists `input.Credentials` when supplied.
* `internal/server/biz/channel_llm.go`
  * Runtime resolves credential refs first, then falls back to inline legacy credential views.
  * OAuth refresh writes to `UpstreamCredential` when a selected credential exists, but otherwise can write refreshed tokens back into `Channel.credentials`.
* `internal/server/biz/channel_apikey_provider.go`
  * API-key selection still falls back to `channel.Credentials.GetAllAPIKeys()`.
* `internal/server/biz/channel_credential_identity.go`
  * Legacy inline credentials are normalized into executable credential views for API key, OAuth, GCP, and Azure.

Deletion guidance:

* Remove new product writes to `Channel.credentials`.
* Keep a read-only migration adapter only while legacy rows exist.
* Replace disabled API-key state with `ChannelCredentialRef.enabled` or `UpstreamCredential.status`.
* Eventually drop channel credential fields from GraphQL, frontend types, and database schema after migration is proven.

## Credential-Owned Model

Current target surfaces:

* `internal/ent/schema/upstream_credential.go`
  * `secret_payload` is the durable secret owner and is skipped from GraphQL.
  * `auth_kind` and `secret_kind` already support `api_key`, `oauth`, `azure`, `gcp`, and `other`.
  * `quota_scope_id` attaches local credential quota.
  * `fingerprint` and `secret_fingerprint` are safe identities.
  * `status`, `weight`, `quota_status`, and `last_error` are credential-level product state.
* `internal/ent/schema/channel_credential_ref.go`
  * This is the binding layer between channels and credentials.
* `internal/server/biz/upstream_credential.go`
  * `MigrateLegacyChannelCredentials` creates/reuses `UpstreamCredential` rows and refs from inline channel credentials.
  * Initial scan did not find evidence that the migration clears the old inline blob after successful migration.
* `internal/server/biz/channel_bulk.go`
  * Bulk import/create already creates channels with empty inline credentials and attaches first-class credentials from supplied API keys.

Keep and strengthen:

* Credential create, rotate, archive, delete.
* Credential local quota.
* Credential/provider quota observability.
* Channel credential binding dialog.

## Frontend Channel Inline Credential Paths

Current surfaces:

* `frontend/src/features/channels/components/channels-action-dialog.tsx`
  * `showChannelInlineCredentialUI = false`, but the file still contains hidden key and OAuth code paths.
  * OAuth hook success handlers still call `form.setValue('credentials.apiKey', ...)`.
  * Codex auth JSON import still writes decoded credentials into `credentials.apiKey`.
  * Copilot device flow still serializes the token into OAuth JSON and stores it in `credentials.apiKey`.
  * `shouldShowLegacyInlineAPIKeys = false`, but dead API-key array form code remains.
* `frontend/src/features/channels/hooks/use-oauth-flow.ts`
  * Example comments still show OAuth success filling `credentials.apiKey`.
* `frontend/src/features/channels/hooks/use-device-flow.ts`
  * Example comments still show device flow success filling `credentials.apiKey`.

Deletion guidance:

* Remove hidden/dead inline credential blocks from channel create/edit.
* Move OAuth import UX to credential create/edit or a credential import dialog.
* Keep channel-side "attach credential" UX only.
* Update frontend channel queries/types to avoid requesting `credentials.apiKey`, `credentials.apiKeys`, and `disabledAPIKeys`.

## Channel Disabled API-Key Management

Current surfaces:

* `internal/server/biz/channel_apikey.go`
  * Disables, enables, bulk-enables, and deletes API keys from `disabled_api_keys` and `Channel.credentials`.
* `internal/server/gql/axonhub.graphql`
  * `disableChannelAPIKey`
  * `enableChannelAPIKey`
  * `enableAllChannelAPIKeys`
  * `enableSelectedChannelAPIKeys`
  * `deleteDisabledChannelAPIKeys`
* `frontend/src/features/channels/components/channels-disabled-api-keys-dialog.tsx`
  * Dedicated disabled key management UI.
* `frontend/src/features/channels/components/channels-test-api-keys-dialog.tsx`
  * Tests raw channel API-key arrays and disables/deletes failed raw keys.
* `frontend/src/features/channels/components/channels-columns.tsx`
  * Shows actions based on `channel.credentials.apiKeys` and `channel.disabledAPIKeys`.

Deletion guidance:

* Replace with credential/ref actions:
  * Disable a credential ref for one channel.
  * Disable/archive/delete the credential globally.
  * Test attached credentials, not raw channel key arrays.
* Do not preserve `disabled_api_keys` semantics during migration unless a user-facing reason appears.

## GraphQL Compatibility Surface

Current surfaces:

* `internal/server/gql/axonhub.graphql`
  * `ChannelCredentials`
  * `ChannelCredentialsInput`
  * `OAuthCredentials`
  * `DisabledAPIKey`
  * `Channel.credentials`
  * `Channel.disabledAPIKeys`
  * channel API-key mutations listed above
  * `migrateLegacyChannelCredentials`
* `internal/server/gql/gqlgen.yml`
  * Maps `ChannelCredentials`, `ChannelCredentialsInput`, `OAuthCredentials`, and `DisabledAPIKey` to Go objects.
* `internal/server/gql/axonhub.resolvers.go`
  * Resolves channel credentials and disabled API keys with permission checks.
* `internal/server/gql/ent.graphql`
  * Generated channel create/update inputs still expose `credentials`.

Deletion guidance:

* First remove frontend dependencies.
* Then deprecate/remove mutations that create or manage channel-owned secrets.
* Keep `migrateLegacyChannelCredentials` until legacy rows are gone.
* Avoid exposing raw OAuth token fields through GraphQL.

## OAuth Flow Migration

Current surfaces:

* Backend HTTP endpoints under `/admin/*/oauth/*` still return credentials/token strings to the frontend.
* Channel create/edit consumes those strings and stores them in `Channel.credentials.apiKey`.
* Existing specs say OAuth must become `UpstreamCredential.secret_kind=oauth`.

Deletion guidance:

* Keep OAuth start/exchange endpoints as provider flow endpoints.
* Change completion semantics so exchange creates or updates an `UpstreamCredential` and returns safe credential metadata.
* Let the user attach that credential to channels after import, or optionally auto-attach when the flow is launched from a channel context.

## Retry / Fallback Product Surface

Current surfaces:

* `internal/server/biz/system.go`
  * `RetryPolicy` includes enabled, max channel retries, max single-channel retries, retry delay, load balancer strategy, auto-disable channel, empty response detection, and upstream error policy.
* `frontend/src/features/system/components/retry-settings.tsx`
  * Presents retry as a primary settings page with strategy, retry counts, delay, empty response detection, and auto-disable channel.
* API-key profile templates also expose load balancer strategy choices.

Deletion/refactor guidance:

* Do not delete retry runtime wholesale in this task.
* Rename or reorganize the product UX around fallback/recovery.
* Keep same-target retry as a narrow transient-error mechanism.
* Move cross-target behavior under fallback semantics.
* Avoid presenting auto-disable channel/API-key as the main way to handle credential failures after credential-aware fallback exists.

## Candidate Implementation Slices

1. Frontend channel credential cleanup:
   * Remove hidden inline key/OAuth blocks.
   * Remove channel table actions for test/disabled API keys.
   * Stop frontend channel queries from requesting raw credential fields.

2. OAuth-to-credential import:
   * Change OAuth exchange responses to create/update `UpstreamCredential`.
   * Add credential import UX.
   * Optional channel-context auto-attach.

3. Channel create/update write-path cleanup:
   * Stop accepting or persisting inline `credentials` in channel create/update.
   * Convert legacy input to credentials/refs only if compatibility is still required.
   * Ensure legacy migration clears or neutralizes migrated inline blobs.

4. Channel API-key GraphQL removal:
   * Remove/disable channel key mutations.
   * Remove frontend hooks.
   * Keep migration mutation.

5. Runtime adapter cleanup:
   * Push credential-scoped execution context deeper into provider quota and outbound auth.
   * Shrink `ToChannelCredentials` usage.

6. Schema removal:
   * After migration is safe, drop or empty channel `credentials` and `disabled_api_keys`.

7. Retry/fallback UX simplification:
   * Reframe settings around fallback.
   * Hide or remove low-value retry knobs.
   * Keep config compatibility in backend normalization.
