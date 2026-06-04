# Sticky Rebind Quota Ratio Bug

## Goal

Fix the sticky-session rebind path so that when a sticky binding has expired and the request re-enters first-bind, the selected credential follows the intended local key-quota ratio rule instead of silently rebinding to a heavily used key such as `input(lite)`.

## What I already know

* The user observed a sticky-session request rebinding to `input(lite)` after the previous successful request was more than 5 minutes earlier.
* The relevant production timestamps were:
  * `#23402` at `2026-06-04 12:45:15`
  * `#23412` at `2026-06-04 12:51:06`
  * The gap is `5m51s`, so the in-memory sticky binding TTL of 5 minutes had elapsed.
* Current sticky-session routing lives in `internal/server/orchestrator/sticky_session.go`.
* Current sticky-session uses `failoverLoadBalancer` during sticky ordering, not the adaptive load balancer.
* The intended local key-quota rebind rule is documented in `.trellis/spec/backend/routing-guidelines.md`.
* Existing sticky-session tests cover simplified candidate layouts, but they do not clearly model the reported production shape where one channel owns multiple credentials and competes with another same-priority channel.

## Assumptions (temporary)

* The reported requests are using the `sticky-session` load balancing strategy.
* The bug is in the current orchestrator sticky-session rebind path, not in an older deleted routing implementation.
* The right first step is to add a regression test that models the real multi-key channel layout before changing routing logic.

## Open Questions

* Does the failure come from `quota-ratio-first-bind` not entering at all, or from it entering with stale / incomplete credential quota views?

## Requirements

* Add a regression test that matches the reported routing shape:
  * expired sticky binding
  * same-priority candidates
  * one channel with multiple credential views
  * another candidate channel with its own credential view
  * local daily quota data that should make a lower-ratio credential win
* The regression must fail on the buggy path and pass after the fix.
* The implementation must preserve existing sticky-session contracts:
  * bind only after upstream success
  * do not cross lower priority tiers during first-bind reordering
  * only compare comparable local quota-managed credential views
* Keep the fix narrowly scoped to sticky-session rebind / first-bind behavior.

## Acceptance Criteria

* [ ] A focused test reproduces the expired-binding rebind scenario with multi-key channels.
* [ ] After the fix, the sticky-session rebind path selects the lowest comparable `used_amount / limit_amount` credential instead of the overused key.
* [ ] Existing sticky-session focused tests still pass.

## Definition of Done

* Focused backend tests pass for the sticky-session routing area.
* No unrelated routing semantics are changed.
* The bug explanation and fix are clear enough to support a later spec update if needed.

## Out of Scope

* Frontend changes
* Provider quota UI changes
* Broad routing redesign beyond the minimal fix required for this bug
* Feature additions unrelated to sticky-session expired-binding rebind behavior

## Technical Notes

* Relevant files:
  * `internal/server/orchestrator/sticky_session.go`
  * `internal/server/orchestrator/sticky_session_test.go`
  * `internal/server/orchestrator/outbound.go`
  * `internal/server/biz/channel_apikey_provider.go`
  * `internal/server/biz/channel_credential_identity.go`
* Relevant specs:
  * `.trellis/spec/backend/index.md`
  * `.trellis/spec/backend/routing-guidelines.md`
  * `.trellis/spec/backend/credential-routing-model.md`
