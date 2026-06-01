# Credential-Centered Channel Cleanup Implementation Notes

## Product Decisions Locked

- Channel is routing-only.
- UpstreamCredential owns API keys, OAuth tokens, cloud credentials, and other upstream identity material.
- OAuth must migrate into UpstreamCredential; it must not remain a durable Channel credential.
- Provider quota means key/credential upstream availability.
- `CredentialQuotaScope` local quota is removed from visible product flows and runtime route availability.
- Legacy `Channel.disabled_api_keys` does not need semantic preservation; migrated credential refs default to enabled.
- Physical schema deletion can be deferred when compatibility or generated-code churn would be risky.

## Implementation Scope

### Backend

- Keep `UpstreamCredential` and `ChannelCredentialRef` as the durable credential model.
- Ensure legacy channel secret migration handles:
  - `Channel.credentials.apiKey`
  - `Channel.credentials.apiKeys`
  - `Channel.credentials.oauth`
  - OAuth JSON stored in `Channel.credentials.apiKey`
- Ensure migrated refs are enabled by default and do not copy `Channel.disabled_api_keys` state.
- Stop using `CredentialQuotaScope` local quota as a routing availability filter.
- Stop provider quota updates from treating `CredentialQuotaScope` as provider quota truth.
- Keep archive/delete semantics:
  - archive preserves refs and excludes by credential status
  - restore can make old refs routable again
  - delete removes refs, soft-deletes credential, and allows same-secret recreation

### GraphQL/API

- Preserve existing compatibility fields where removing them would require broad schema/generated churn.
- Treat channel `credentials` input as deprecated compatibility. New product flows should use credential create/import/bind APIs.
- Treat `createCredentialQuotaScope`, `updateCredentialQuotaScope`, and credential quota inputs as deprecated local-quota compatibility during this pass.

### Frontend

- Remove key/OAuth entry from channel create/edit flows.
- Move OAuth import/create expectations to credential flows.
- Remove visible `CredentialQuotaScope` local quota UI from credential creation/edit/detail.
- Keep provider quota/status display for credentials.
- Channel UI should show routing/binding state, not raw key or OAuth ownership.

### Tests

- Backend tests for legacy key/OAuth migration, especially OAuth JSON in `apiKey`.
- Backend tests that disabled legacy channel keys do not create disabled refs.
- Backend tests that route availability does not consult `CredentialQuotaScope`.
- Backend tests that provider quota only blocks the affected credential/key.
- Frontend type-check after removing channel key/OAuth and local quota UI.

## Implementation Order

1. Backend inspection and tests around current migration/runtime quota behavior.
2. Backend code changes for migration and runtime/provider quota semantics.
3. Frontend data/schema and UI cleanup for channel credentials and credential local quota.
4. Verification with focused Go tests and frontend type-check.
5. Spec/PRD update if implementation uncovers a narrower compatibility boundary.

## Compatibility Boundary

This task removes product and runtime dependence, not necessarily every database field or generated GraphQL type in the first pass. Existing rows and old clients should not break abruptly, but new UI/runtime behavior must follow the credential-centered model.

## Implementation Status

Implemented in this pass:

- Legacy `Channel.disabled_api_keys` no longer disables migrated legacy credential views or newly migrated credentials.
- Runtime credential selectability ignores `CredentialQuotaScope` local quota status and only blocks provider credential quota states such as exhausted, paused, or disabled.
- Provider quota cache and lookup no longer use quota-scope identity; provider observations update credential/key targets and leave `CredentialQuotaScope` local budget rows untouched.
- Channel reload no longer treats local quota-scope updates as routing-cache invalidation triggers.
- Channel create/edit no longer shows inline API key/OAuth/GCP credential entry in the product flow and does not send inline credentials on update; create sends the GraphQL-required empty compatibility payload.
- Credential create/edit/list/detail and channel credential binding display no longer expose local quota as a product concept; provider quota display remains.

Verification run:

- `gofmt -w` on touched Go files.
- `git diff --check`
- `go test ./internal/server/biz -count=1 -timeout 120s`
- `go test ./internal/server/orchestrator -count=1 -timeout 120s`
- `pnpm exec tsc --noEmit --pretty false` from `frontend/`
