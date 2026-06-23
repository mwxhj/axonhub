# Journal - John Doe (Part 1)

> AI development session journal
> Started: 2026-05-30

---


## Session 1: Credential scoped routing and sticky session cleanup

**Date**: 2026-05-31
**Task**: Credential scoped routing and sticky session cleanup
**Branch**: `unstable`

### Summary

Implemented credential scoped routing/quota and request observability, documented sticky-session workflow contract, decoupled sticky-session routing from model circuit breaker, and archived completed Trellis tasks.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `ad06382` | (see git log) |
| `8faa0c0` | (see git log) |
| `6d2e106` | (see git log) |
| `0599880` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 2: Credential quota clarity and archive

**Date**: 2026-06-01
**Task**: Credential quota clarity and archive
**Branch**: `unstable`

### Summary

Clarified local/provider quota UI, added quota reset defaults and validation, and introduced explicit credential archive mutation and dialog.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `4446a30` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 3: Credential restore and delete fixes

**Date**: 2026-06-01
**Task**: Credential restore and delete fixes
**Branch**: `unstable`

### Summary

Fixed same-secret archived credential creation, made archive reversible for routing refs, added credential delete API/UI, and updated credential archive/delete contracts.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `4a62d1b` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 4: Credential-centered channel cleanup

**Date**: 2026-06-01
**Task**: Credential-centered channel cleanup
**Branch**: `unstable`

### Summary

Made channels routing-only in product flows, removed local credential quota from route availability/UI, and moved provider quota semantics to credential/key targets.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `30596c2` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 5: Credential key quota and OAuth credential creation

**Date**: 2026-06-01
**Task**: Credential key quota and OAuth credential creation
**Branch**: `unstable`

### Summary

Restored credential-local quota as a key feature, moved OAuth credential creation into credential flows, clarified provider/local quota separation, and updated routing/spec tests.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `ed86881` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 6: Sticky quota ratio balancing

**Date**: 2026-06-01
**Task**: Sticky quota ratio balancing
**Branch**: `unstable`

### Summary

Implemented sticky-session first-bind balancing by local key quota usage ratio, updated routing specs, and added focused backend tests for priority, mixed quota/no-quota, shared scope, and invalid quota data behavior.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `5a2f156` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 7: Credential quota display and sticky fallback

**Date**: 2026-06-02
**Task**: Credential quota display and sticky fallback
**Branch**: `unstable`

### Summary

Displayed local credential quota amounts in the credentials table and implemented credential-aware sticky fallback with request-scoped key exclusions, fallback-first retry behavior, priority-tier preservation, docs, and focused tests.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `52a39a0` | (see git log) |
| `04d4078` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 8: Credential quota reset time and HCI cleanup

**Date**: 2026-06-04
**Task**: Credential quota reset time and HCI cleanup
**Branch**: `unstable`

### Summary

Added configurable credential local quota daily reset time, cleaned credential/request UI to remove internal identifiers, and documented frontend HCI rules.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `24618c2` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 9: Feature pruning hard-cut rollout

**Date**: 2026-06-04
**Task**: Feature pruning hard-cut rollout
**Branch**: `unstable`

### Summary

Hard-cut legacy quota and routing surfaces, removed provider quota subsystem, narrowed GraphQL/UI exposure, and synced Trellis specs.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `7f6ff5c` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 10: Sticky session fallback routing redesign

**Date**: 2026-06-05
**Task**: Sticky session fallback routing redesign
**Branch**: `unstable`

### Summary

Defined and implemented the A-G sticky fallback routing lifecycle. Sticky first-bind now uses normal primary selection without quota-ratio balancing, load balancing keeps full candidate sets, same-channel model alternatives are explicit fallback candidates, and orchestrator routing tests were updated for the credential/ref model.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `93df0a1` | (see git log) |
| `aa358b4` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 11: Upstream beta4 safe patch port wrap-up

**Date**: 2026-06-23
**Task**: Upstream beta4 safe patch port wrap-up
**Branch**: `unstable`

### Summary

Completed upstream beta4 safe patch port, added request observability and LLM transformer media helpers, verified with targeted backend/llm tests, archived all active tasks.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `0d0131cb` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete
