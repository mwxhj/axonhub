# Credential key quota and OAuth credential creation

## Goal

Correct the credential/channel cleanup so quota is removed from channels but retained on upstream credentials/keys, and move key/OAuth creation paths into the credential product surface. Channels should stay routing-only; credentials should own API keys, OAuth tokens, cloud credentials, key-local quota, and provider quota identity.

## What I Already Know

- The previous cleanup made channels more credential-centered, but it also removed credential local quota UI too aggressively.
- The intended product boundary is: remove channel quota/double quota, keep key/credential quota.
- `CredentialQuotaScope` still exists in Ent, GraphQL, and backend service inputs.
- `CreateUpstreamCredentialInput` and `UpdateUpstreamCredentialInput` still support `quotaScopeID` and inline quota payloads.
- Frontend locale keys for credential quota still exist, but the quota form component was removed from credential create/edit flows.
- OAuth handlers for Codex, Claude Code, Antigravity, and GitHub Copilot still exist under admin HTTP routes, while OAuth UI remains mostly in the channel create/edit dialog.
- Existing backend specs currently say credential-local quota is not product-facing. That spec is now stale for the corrected decision and must be updated.

## Requirements

- Restore credential/key local quota as a product-facing credential feature.
- Do not restore channel-local quota or channel-level double quota.
- Credential create/edit must support:
  - No key-local quota.
  - Create or update a quota scope for this credential/key.
  - Attach an existing shared credential quota scope.
  - Clear an attached quota scope during edit.
- The credential quota UI copy must clearly label this as key/credential local quota, not channel quota and not provider quota.
- Credential detail/list views must distinguish:
  - Key-local quota (`CredentialQuotaScope`).
  - Provider quota (`ProviderQuotaStatus` / `UpstreamCredential.quotaStatus`).
  - Routing availability through channel credential refs.
- Runtime route eligibility may use key-local quota to filter the affected credential/key only. It must not disable the whole channel when other attached credentials remain usable.
- Provider quota status remains credential/key-rooted. `ProviderQuotaStatus.channel_id` remains compatibility/last-observed metadata only.
- Credential creation must support OAuth credential secrets under the credential UI, not only through channel inline credential fields.
- Codex, Claude Code, GitHub Copilot, and Antigravity OAuth flows should create/import `UpstreamCredential` records with `secretKind=oauth` and provider-appropriate `providerType`.
- Channel create/edit should not reintroduce durable key/OAuth secret fields. It may keep compatibility plumbing only where GraphQL still requires it.
- Backend and Trellis specs must be updated so future work does not remove key/credential quota again.

## Acceptance Criteria

- [x] A user can create an API-key credential with no quota.
- [x] A user can create an API-key credential with a key-local quota scope.
- [x] A user can edit a credential quota scope or attach/clear a shared scope.
- [x] Credential list/detail displays key-local quota separately from provider quota.
- [x] Exhausted/paused/disabled key-local quota removes only that credential from routing, not the whole channel if another credential is available.
- [x] Provider quota exhaustion still removes only the exhausted credential/key.
- [x] Credential creation can import/create OAuth credentials for Codex, Claude Code, GitHub Copilot, and Antigravity without putting OAuth secret material into channel fields.
- [x] Existing GraphQL compatibility fields remain usable enough to avoid breaking old callers.
- [x] Specs document the corrected boundary: no channel quota; credential/key quota is allowed.
- [x] Focused backend tests and frontend type-check cover the changed flows.

## Definition of Done

- Tests added or updated for changed backend quota eligibility and credential mutation behavior.
- Frontend type-check passes after restoring quota/OAuth credential UI.
- No raw API key, OAuth access token, refresh token, or service-account secret is exposed in read queries, logs, exports, or UI display.
- Trellis task docs and affected specs reflect the final product boundary.

## Technical Approach

Restore the existing `CredentialQuotaScope` plumbing as a credential/key feature instead of inventing a new schema. The backend already has quota-scope mutations and credential input fields; the main backend work is to make runtime filtering respect credential-local quota at the credential view level, while keeping provider quota identity credential-rooted and channel quota absent.

On the frontend, reintroduce a credential quota form component and wire it into create/edit/detail/list using the existing GraphQL schema. Reuse the existing i18n keys but tighten labels and descriptions so operators understand the three separate concepts: key-local quota, provider quota, and route availability.

For OAuth, move/import the existing channel OAuth controls into credential creation. The OAuth handlers can continue returning token payloads; the credential dialog should convert successful OAuth payloads into `CreateUpstreamCredentialInput.secret.oauth` with `authKind/secretKind=oauth`.

## Decision (ADR-lite)

**Context**: Previous cleanup correctly removed channel-owned secrets and quota semantics, but overcorrected by treating `CredentialQuotaScope` as fully legacy. The desired model keeps quota on keys/credentials.

**Decision**: Keep `CredentialQuotaScope` as credential/key-local quota. Remove channel quota semantics only. Runtime eligibility can filter by credential quota, but the filtering target must be the credential view, never the channel as a whole.

**Consequences**: The project needs a spec correction. Backend route selection must be precise enough to leave a channel eligible when at least one attached credential is still available. Frontend copy must stop using ambiguous "quota" labels without local/provider context.

## Out of Scope

- Physical database removal of legacy channel credential fields.
- A new org/project/consumer budgeting system.
- Hard deletion of provider quota history.
- Reworking OAuth backend protocols beyond wiring their results into credential creation.
- Removing GraphQL compatibility fields in this task.

## Technical Notes

- Backend spec to update: `.trellis/spec/backend/credential-routing-model.md`.
- Routing spec to update: `.trellis/spec/backend/routing-guidelines.md`.
- Backend likely affected files:
  - `internal/server/biz/upstream_credential.go`
  - `internal/server/biz/channel_credential_identity.go`
  - `internal/server/biz/provider_quota.go`
  - `internal/server/orchestrator/quota_status.go`
  - `internal/server/gql/axonhub.graphql`
- Frontend likely affected files:
  - `frontend/src/features/credentials/components/create-credential-dialog.tsx`
  - `frontend/src/features/credentials/components/edit-credential-dialog.tsx`
  - `frontend/src/features/credentials/components/credential-detail-dialog.tsx`
  - `frontend/src/features/credentials/components/credentials-columns.tsx`
  - `frontend/src/features/credentials/components/form-utils.ts`
  - `frontend/src/features/credentials/data/schema.ts`
  - `frontend/src/features/channels/components/channels-action-dialog.tsx`
  - `frontend/src/locales/*/credentials.json`

## Verification

- `go test ./internal/server/gql -run 'TestGraphQLCreateUpstreamCredentialMutation|Test.*UpstreamCredential' -count=1 -timeout 120s`
- `go test ./internal/server/biz -run 'TestCredentialViewsFromRefsFiltersExhaustedLocalQuotaScope|TestCredentialViewsFromRefsFiltersOnlyBlockedLocalQuotaScope|TestCredentialViewQuotaSelectableHonorsLocalScopeStatus|TestChannelCredentialViewsReevaluateExpiredQuotaReset|TestBuildChannelKeepsQuotaPausedCredentialLoadable|Test.*OAuth|Test.*Codex|Test.*Claudecode|Test.*Antigravity|Test.*GithubCopilot' -count=1 -timeout 120s`
- `go test ./internal/server/orchestrator -run 'TestProviderQuotaSelectorFiltersLocalQuotaScopeWithoutProviderData|TestProviderQuotaSelector_NarrowsCredentialViewsBeforeAPIKeySelection|TestProviderQuotaSelector_FiltersChannelWhenAllCredentialViewsExhausted' -count=1 -timeout 120s`
- `pnpm exec tsc --noEmit --pretty false`
- `git diff --check`
