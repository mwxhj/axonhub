# Credential Quota Reset Time and HCI Cleanup

## Goal

Make credential local quota reset timing configurable at the system level, and clean the credential/request frontend so it displays operator-facing information instead of internal fingerprints, resource scope keys, key hints, or credential ref implementation state.

This task is not a cosmetic pass. It establishes a product rule: UI must support human decisions and operations, not expose backend implementation details.

## What I Already Know

* The user wants quota refresh time to be configurable, not just displayed after the fact.
* Existing system settings already include `SystemGeneralSettings.timezone`.
* Existing credential quota scopes store `reset_policy`, `reset_at`, and `window_started_at`.
* Existing credential table displays `secret:v1:*` under the credential name and displays local quota as `status · scope name`, e.g. `available · credential default`.
* Existing credential table has a separate `keyHint` column.
* Existing request table credential column can display key hints, secret fingerprints, resource scope keys, credential source strings, and quota scope snapshots.
* The desired local quota list wording is like `2.89 / 576.94 USD · today's usage`.
* The user does not want an "advanced debug" UI that exposes `secret:v1:*`, `ref · ...`, or masked key fragments. Backend correlation can use request ID, channel ID, credential ID, logs, or database rows.
* A new frontend HCI spec now forbids displaying implementation identities in product UI.

## Requirements

### System Quota Reset Settings

* Add a system-level setting for credential local quota reset time.
* The setting must work with the existing system timezone.
* Default behavior should be easy to understand: daily local quota windows reset at `00:00` in the configured system timezone.
* Credential local quota scopes with automatic daily reset should inherit the system reset time unless a deliberate per-scope override already exists or is added.
* Updating the system reset time must not corrupt existing usage. It should affect the next automatic window calculation and new quota scopes.
* The system settings UI should let the operator set timezone and quota reset time together or in the same general area.

### Credential Quota UI

* Credential list must not show `secret:v1:*` fingerprints.
* Credential list must not show a key hint column by default.
* Local quota display should be business-facing:
  * No quota: `No local quota`
  * With quota: `<used> / <limit> <unit> · today's usage`
  * Optional second line: reset rule, e.g. `Resets daily at 00:00 Asia/Shanghai`
* Local quota display must not show `available · credential default` as the primary message.
* Provider quota remains separate from local quota and should continue to show `No provider quota observation` when empty.

### Request Log UI

* Request list credential column must not display:
  * masked raw key hints such as `sk-2...`
  * `secret:v1:*`
  * credential fingerprints
  * secret fingerprints
  * resource scope keys
  * `ref · ... · available`
  * credential source strings
* Request list should display channel name and credential name, with status/error/usage/cost/timing handled by their existing columns.
* Request detail should follow the same rule. If it needs credential context, show credential name and channel name, not key hint or resource scope identity.

### GraphQL/Data Shape Hygiene

* Frontend queries should stop fetching forbidden fields when they are only used for display.
* Backend can keep storing the fields for audit/routing/correlation.
* Do not remove backend schema fields in this task unless they are proven unused outside display.

## Acceptance Criteria

* [ ] `.trellis/spec/frontend/human-computer-interaction.md` documents the UI rule and forbidden display fields.
* [ ] System general settings expose a configurable credential local quota reset time.
* [ ] Daily credential local quota reset calculation uses the configured timezone and reset time.
* [ ] Credential create/edit quota defaults use the configured reset time for daily scopes.
* [ ] Credential list no longer renders `secret:v1:*` or key hints.
* [ ] Credential local quota cell renders used/limit/unit/today wording instead of `available · credential default`.
* [ ] Request list no longer renders key hints, resource scope keys, credential source strings, or fingerprints.
* [ ] Request detail no longer renders key hints, resource scope keys, credential source strings, or fingerprints as product fields.
* [ ] Frontend data queries remove forbidden fields where they are no longer needed.
* [ ] Focused backend tests cover reset-time calculation.
* [ ] Focused frontend/component tests or targeted UI checks cover the credential and request display changes where practical.

## Definition of Done

* Specs and task docs are updated.
* Relevant backend tests pass for quota reset calculation.
* Relevant frontend typecheck/tests pass if explicitly requested by the user.
* `git diff --check` passes.
* No lint/build command is run unless explicitly requested.

## Out of Scope

* Removing credential local quota.
* Removing provider quota.
* Dropping backend fingerprint/key-hint columns.
* Redesigning provider quota checkers.
* Building a developer-debug frontend surface for internal identifiers.
* Reworking sticky-session fallback or quota-ratio routing beyond using correct reset windows if needed.

## Implementation Notes

Likely backend files:

* `internal/server/biz/system.go`
* `internal/server/biz/system_default.go`
* `internal/server/gql/system.graphql`
* `internal/server/gql/system.resolvers.go`
* `internal/server/biz/usage_log.go`
* `internal/server/biz/upstream_credential.go`
* `internal/ent/schema/credential_quota_scope.go` only if per-scope override fields are required

Likely frontend files:

* `frontend/src/features/system/components/general-settings.tsx`
* `frontend/src/features/system/data/system.ts`
* `frontend/src/features/credentials/components/credential-quota-fields.tsx`
* `frontend/src/features/credentials/components/credentials-columns.tsx`
* `frontend/src/features/credentials/components/credential-detail-dialog.tsx`
* `frontend/src/features/credentials/data/credentials.ts`
* `frontend/src/features/requests/components/requests-columns.tsx`
* `frontend/src/features/requests/components/request-detail-content.tsx`
* `frontend/src/features/requests/data/requests.ts`
* `frontend/src/features/requests/data/usage-logs.ts`

## Open Questions

* Should the first implementation use only a system-level daily reset time, or should individual quota scopes also support an explicit override?
