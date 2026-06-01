# Credential create and archive recovery fixes

## Goal

Fix credential management regressions after the quota/archive work so users can create credentials, recover from archived credentials without broken routing, and permanently delete credentials when needed.

## What I already know

* Creating an upstream credential currently fails from the product flow; the user is not sure whether the failing case is the same secret, a different secret, or both.
* After archiving a credential and enabling it again, routing can fail with `model not found` for requests such as `gpt-5.5`.
* The product needs a real delete credential action in addition to archive.
* The previous task added quota reset defaults, an archive mutation, provider quota status display, and frontend quota UI changes.

## Assumptions (temporary)

* "Add credential" means the existing frontend create dialog backed by GraphQL `createUpstreamCredential`.
* "Enable after archive" means changing credential status away from `archived` via the edit/status flow; routing should recover without manually reattaching channels.
* Delete should remove or safely detach credential routing state without leaving stale channel refs.

## Open Questions

* Whether delete should be hard delete or soft delete can likely be derived from existing service patterns.

## Requirements (evolving)

* Credential creation must accept the frontend form payload for the default no-quota case and quota-enabled cases.
* Creating with a secret that matches an archived credential must reactivate/update that credential instead of silently returning a still-archived credential.
* Archive is reversible: it marks the credential archived, preserves channel refs, and relies on credential status to remove it from runtime routing.
* Re-enabling an archived credential must restore routing availability. For credentials archived by older code that disabled refs, re-enable should recover refs instead of leaving the channel without usable keys.
* Users must have an explicit delete credential action.
* UI copy must distinguish archive from delete.

## Acceptance Criteria (evolving)

* [ ] Create credential succeeds through GraphQL with frontend-like payloads.
* [ ] Creating the same key after archive returns an enabled/reactivated credential.
* [ ] Archive then re-enable restores model routing availability through preserved or recovered channel refs.
* [ ] Delete credential action exists in backend API and frontend UI and allows the same key to be added again.
* [ ] Tests cover create, archive/re-enable, and delete behavior.

## Definition of Done

* Tests added or updated around service and GraphQL behavior.
* Frontend type-check passes when frontend files change.
* Specs or notes updated if product behavior changes.
* Changes are committed separately from task bookkeeping.

## Out of Scope

* Provider-specific model discovery changes unrelated to credential status/ref lifecycle.
* Reworking quota metering semantics beyond fixing create flow regressions.

## Technical Notes

* Investigation starts from credential GraphQL mutations, frontend credential form serialization, and `UpstreamCredentialService`.
* Confirmed root cause for same-key add failure: `CreateUpstreamCredential` dedupes by fingerprint/secret fingerprint and returns an archived existing row unchanged.
* Confirmed root cause for archive->enable routing failure: archive disabled `ChannelCredentialRef` rows, while the generic status update only changed `UpstreamCredential.status`; runtime requires both ref enabled and credential status enabled.
