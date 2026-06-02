# show credential local quota amounts

## Goal

Update the credentials list so the `Local Quota` column shows the configured local quota limit and current usage for credentials that have a quota scope. Operators should be able to scan the list and know both how much quota was set and how much has been consumed without opening each credential detail dialog.

## What I already know

* The user showed the credentials table where `Local Quota` currently displays only `无本地配额` or `可用 · 凭据默认`.
* The requested first change is to make this column show both the configured quota and the used amount.
* The credentials GraphQL query already fetches `quotaScope.limitAmount`, `quotaScope.usedAmount`, and `quotaScope.unit`.
* The frontend schema already validates those fields.
* The credential detail dialog already displays quota limit and usage, so this is primarily a list/table presentation gap.

## Assumptions

* Credentials without a local quota should continue to show `无本地配额`.
* Credentials with a quota scope should keep the current status/scope label, then add a compact second line with `used / limit` and unit.
* If either used or limit is absent, the UI should degrade gracefully instead of showing misleading math.
* This task does not change quota accounting, routing, reset behavior, or provider quota display.

## Requirements

* Show local quota status and scope name as today for credentials with a quota scope.
* Show current usage and configured limit in the local quota column when data exists.
* Include the quota unit label using existing i18n keys.
* Keep the table compact and readable in the existing list layout.
* Preserve the `无本地配额` display for credentials that have no local quota scope.

## Acceptance Criteria

* [x] A credential with `usedAmount=1200`, `limitAmount=5000`, and `unit=token` displays a compact `1,200 / 5,000 Token`-style value in the local quota column.
* [x] A credential with a local quota scope still displays its status and scope name.
* [x] A credential without a local quota scope still displays `无本地配额`.
* [x] Missing quota amount fields do not crash rendering and do not show `undefined`.
* [x] No backend schema change is required for this UI-only improvement.

## Definition of Done

* [x] Frontend table rendering updated.
* [x] Existing i18n labels reused where possible.
* [x] Lightweight validation completed without running broad lint/build unless explicitly requested.

## Verification

* Reviewed the focused table rendering diff.
* Did not run broad frontend lint/build per project command constraints.

## Out of Scope

* Provider quota UI changes.
* Quota reset-time UI changes.
* Quota accounting or routing behavior changes.
* Backend GraphQL/Ent schema changes.
* New quota edit actions or delete/archive actions.

## Technical Notes

* Likely UI file: `frontend/src/features/credentials/components/credentials-columns.tsx`.
* Data source: `frontend/src/features/credentials/data/credentials.ts` already includes `quotaScope { unit limitAmount usedAmount ... }`.
* Type schema: `frontend/src/features/credentials/data/schema.ts` already includes quota amount fields.
