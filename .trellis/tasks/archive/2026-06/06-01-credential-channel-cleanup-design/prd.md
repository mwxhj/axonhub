# Credential-Centered Channel Cleanup Design

## Goal

Make channels pure routing configuration and move all upstream identity, secret, OAuth, and provider quota semantics into credentials. This reduces duplicated concepts in channel management, makes credential reuse explicit, and prepares the product for cleaner routing availability explanations.

## What I Already Know

- The current product already has first-class `UpstreamCredential` and `ChannelCredentialRef` models.
- `UpstreamCredential` supports `auth_kind` / `secret_kind` values including `api_key`, `oauth`, `azure`, `gcp`, and `other`.
- `UpstreamCredential.secret_payload` already stores `objects.UpstreamCredentialSecret`, which supports both `APIKey` and `OAuth`.
- `Channel.credentials` still exists and stores inline `apiKey`, `apiKeys`, `oauth`, `azure`, and `gcp` payloads.
- OAuth is currently channel-owned in both backend objects and frontend channel schemas; legacy OAuth JSON can also be stored in `credentials.apiKey`.
- `ProviderQuotaStatus` already has credential-oriented fields (`credential_id`, `credential_fingerprint`, `secret_fingerprint`, `resource_scope_key`, `quota_scope_id`) but still also has `channel_id` and legacy `scope_key="channel"` compatibility.
- The previous credential quota UI work introduced `CredentialQuotaScope`, but the product direction is now to remove local quota as a product concept and keep only key/credential provider quota.

## Requirements

- Channel must become routing-only in the product model.
- Channel must not own API key, OAuth token, refresh token, service account JSON, or other upstream secret material in new product flows.
- Channel must not own provider/key quota state in new product flows.
- Channel should keep routing-related fields such as type, base URL, supported models, model mappings, endpoint settings, priority/order, tags, status, and health/error metadata.
- UpstreamCredential must own upstream identity material for API key and OAuth providers.
- OAuth credentials must be migrated to UpstreamCredential rather than left on Channel.
- ChannelCredentialRef must remain the binding between a channel and one or more credentials.
- ProviderQuotaStatus should be credential-rooted for runtime decisions and UI display.
- `CredentialQuotaScope` local quota should be removed from visible product flows and runtime availability decisions.
- Quota should mean key/credential upstream provider quota in this task, not channel-local or gateway-local budget.
- Channel-level credential/quota fields may remain as deprecated compatibility storage during migration, but runtime and new UI should stop depending on them after the migration is complete.
- Existing channels must keep working through a backfill/migration path, but legacy per-channel disabled key state does not need to be preserved.
- Existing request/usage history must remain readable through safe snapshots and must not expose raw secrets.

## Acceptance Criteria

- [x] A design decision is recorded that Channel is routing-only and Credential owns key/OAuth/provider quota semantics.
- [x] OAuth migration is explicitly included in scope.
- [x] The migration plan covers inline channel `credentials.apiKey`, `credentials.apiKeys`, `credentials.oauth`, and OAuth JSON stored in `credentials.apiKey`.
- [x] The migration plan defines how ChannelCredentialRef rows are created for migrated credentials.
- [x] The migration plan explicitly drops/ignores legacy `Channel.disabled_api_keys` compatibility state.
- [x] The runtime plan states that routing reads executable credentials from credential refs before falling back to deprecated channel inline credentials.
- [x] The provider quota plan states that new quota observations update credential/key targets rather than channel-root status.
- [x] `CredentialQuotaScope` local quota is removed from visible UI and runtime route availability semantics.
- [x] The UI plan removes key/OAuth entry from channel create/edit flows and moves those actions to credential create/bind flows.
- [x] The cleanup plan separates compatibility deprecation from later physical schema deletion.

## Technical Approach

Use a phased migration rather than direct destructive cleanup.

Phase 1 defines the product contract and compatibility boundary:

- Channel is a route.
- Credential is an upstream account/secret.
- ChannelCredentialRef binds routes to credentials.
- ProviderQuotaStatus describes upstream availability for a credential/key target.
- CredentialQuotaScope local quota is not part of the MVP product model.

Phase 2 backfills existing channel-owned secrets into credentials:

- Single API key in `Channel.credentials.apiKey` becomes an `UpstreamCredential` with `secret_kind=api_key`, unless the value is detected as OAuth JSON.
- Multiple API keys in `Channel.credentials.apiKeys` become separate `UpstreamCredential` rows, each bound to the channel.
- `Channel.credentials.oauth` becomes an `UpstreamCredential` with `secret_kind=oauth`.
- OAuth JSON stored in `Channel.credentials.apiKey` becomes an `UpstreamCredential` with `secret_kind=oauth`.
- Legacy `Channel.disabled_api_keys` state is not preserved as credential or ref state. Migrated credential refs default to enabled unless the credential itself is inactive for another reason.

Phase 3 changes new product flows:

- Channel create/edit does not ask for upstream key/OAuth.
- Credential create/import/OAuth flows create or update credentials.
- Channel credential binding UI selects which credentials a channel can use.
- OAuth flows for Codex, Claude Code, GitHub Copilot, and Antigravity should return/import credentials, not channel inline credentials.

Phase 4 changes runtime and quota ownership:

- Runtime credential resolution prefers `ChannelCredentialRef` and only uses inline channel credentials as a deprecated fallback while migration is incomplete.
- Provider quota checks update credential-rooted `ProviderQuotaStatus` rows.
- Channel-level quota/provider status is compatibility metadata only.
- Runtime route availability does not consult local quota scopes.

Phase 5 removes compatibility paths later:

- Remove channel inline secret UI and API inputs first.
- Remove runtime fallback after migrations are proven.
- Physically drop deprecated channel secret/quota fields only in a later cleanup task.

## Decision

Context: The product currently has both channel inline credentials and first-class upstream credentials. Keeping both as active models makes quota, OAuth, deletion, archive/restore, and route availability ambiguous.

Decision: Channel will be routing-only. API keys, OAuth credentials, and provider quota state will move to UpstreamCredential and credential-rooted quota status. OAuth should be migrated because it is upstream identity material, not routing configuration. Local quota is removed from this product direction; quota means key/credential upstream provider quota.

Consequences: The implementation needs compatibility/backfill work and careful frontend migration. The model becomes simpler after migration: users manage credentials once, bind them to channels, and route availability can explain whether failure comes from route status, binding status, credential status, or provider quota.

## Out Of Scope

- Consumer/user/org/project local quota product design.
- Immediate physical deletion of channel credential storage from the database.
- Wiping archived credential secret payloads.
- Replacing API key profile/user quota systems.
- Changing provider OAuth protocol details beyond moving OAuth results into credentials.
- Keeping `CredentialQuotaScope` local quota as a visible credential-management concept.

## Technical Notes

- `.trellis/spec/backend/credential-routing-model.md` records the durable project contract for channel routing-only behavior, credential-owned secrets/OAuth, provider quota ownership, and local quota removal.
- `.trellis/spec/backend/routing-guidelines.md` now defers credential ownership and local quota semantics to the credential routing model and keeps only routing/archive/delete contracts.
- `internal/objects/channel.go` defines `ChannelCredentials` with `APIKey`, `OAuth`, `APIKeys`, `Azure`, and `GCP`.
- `internal/objects/channel.go` also has compatibility detection for OAuth JSON in `APIKey`.
- `internal/ent/schema/channel.go` stores `credentials` and `disabled_api_keys` as sensitive channel JSON fields.
- `internal/ent/schema/upstream_credential.go` already supports OAuth secret kinds and credential status metadata.
- `internal/ent/schema/provider_quota_status.go` has both legacy `channel_id` and newer credential/resource/quota-scope fields; the future-facing product meaning should be credential/key provider quota.
- `frontend/src/features/channels/data/schema.ts` still validates OAuth and API key input as part of channel create/update.
- `frontend/src/features/credentials/data/schema.ts` already has credential secret inputs with OAuth support.

## Resolved Questions

- `CredentialQuotaScope` local quota should be removed from visible product flows and runtime route availability. The system should not implement channel/credential double local quotas. Quota in this cleanup means key/credential upstream provider quota.
- Legacy `Channel.disabled_api_keys` does not need semantic preservation during backfill. Per-channel disabled-key compatibility can be dropped because the operator has already replaced affected keys.
