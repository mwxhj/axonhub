# Feature Pruning Roadmap

## Goal

Record one implementation-driving roadmap for the six pruning topics already locked in `prd.md`:

1. sticky-session decision order and binding semantics
2. provider quota strong deletion
3. legacy compatibility one-shot hard cut
4. copy/export/masking surface collapse
5. retry/fallback and health-score pruning
6. user-visible routing strategy naming simplification

This document is the technical companion to `prd.md`. It ties each roadmap decision to the current code/document surface so later deletion tasks can execute without reopening the product direction.

## Current State Snapshot

The repo already points toward the credential-centered model, but several old and overlapping surfaces are still live:

- `Channel.credentials` and `Channel.disabled_api_keys` still exist in the schema and GraphQL (`internal/ent/schema/channel.go:108`, `internal/ent/schema/channel.go:109`, `internal/server/gql/axonhub.graphql:720`, `internal/server/gql/axonhub.graphql:725`).
- Ent-generated channel create/update inputs still require or accept inline credentials (`internal/server/gql/ent.graphql:1720`, `internal/server/gql/ent.graphql:7093`).
- Channel runtime still falls back to legacy inline credentials when no `ChannelCredentialRef` exists (`internal/server/biz/channel_llm.go:438`, `internal/server/biz/channel_llm.go:440`, `internal/server/biz/channel_credential_identity.go:580`).
- API-key selection still has a legacy fallback path that directly reads `channel.Credentials.GetAllAPIKeys()` when no credential views remain (`internal/server/biz/channel_apikey_provider.go:48`, `internal/server/biz/channel_apikey_provider.go:52`).
- OAuth/channel create UI still carries hidden inline-secret code paths even though the visible inline UI is off (`frontend/src/features/channels/components/channels-action-dialog.tsx:358`, `frontend/src/features/channels/components/channels-action-dialog.tsx:363`, `frontend/src/features/channels/components/channels-action-dialog.tsx:1013`, `frontend/src/features/channels/components/channels-action-dialog.tsx:1054`).
- Reliability is still presented as a retry-centric admin settings surface with strategy names and several knobs (`frontend/src/features/system/components/retry-settings.tsx:16`, `frontend/src/features/system/components/retry-settings.tsx:188`, `internal/server/biz/system.go:291`).
- Some docs still teach the removed model directly, including channel-owned multiple API keys and OAuth credentials filled back into channel forms (`docs/zh/guides/channel-management.md:225`, `docs/en/guides/antigravity.md:24`).

## Topic 1: Sticky-Session Semantic Simplification

### Current Code Facts

- Sticky binding already stores `channel + credential` identity, so the target execution identity is not channel-only anymore (`.trellis/spec/backend/routing-guidelines.md`, `internal/server/orchestrator` work summarized in archived tasks).
- First-bind credential choice still happens late, in the API-key provider, after channel candidate selection stays channel-scoped (`internal/server/biz/channel_apikey_provider.go:18`, `internal/server/biz/channel_apikey_provider.go:42`, `.trellis/tasks/archive/2026-06/06-02-credential-aware-sticky-fallback/prd.md`).
- `TraceStickyKeyProvider` persists selected credential metadata into request context, including credential fingerprint, secret fingerprint, resource scope key, source, and quota metadata (`internal/server/biz/channel_apikey_provider.go:298`, `internal/server/biz/channel_apikey_provider.go:300`, `internal/server/biz/channel_apikey_provider.go:301`).
- Legacy inline views are normalized into credential-like runtime views, which keeps the old model alive inside the same sticky/fallback path (`internal/server/biz/channel_credential_identity.go:580`).
- The routing spec still explicitly describes first unbound selection as starting from the existing channel load-balancer order and only then doing local quota-ratio balancing inside that result (`.trellis/spec/backend/routing-guidelines.md`).

### Roadmap Decision

Keep:

- sticky-session as cache-locality state
- priority tiers as service-level routing policy
- final execution materialization as concrete `channel + credential`

Delete:

- long-term channel-first first-bind architecture
- silent semantic fallback between "binding hit", "rebind", and plain load balancing
- any user-facing interpretation that sticky means a separate global routing mode with its own opaque distribution semantics

Demote:

- branch-level internal sticky reasons to debug and request observability only

### Required Execution Order

1. Make sticky semantic states explicit in logs, request execution state, and debug output.
2. Finish removing silent degrade semantics from sticky behavior.
3. Refactor first-bind so selected tier comes first, then credential/resource-scope affinity is resolved inside that tier.
4. Only after that, revisit persistence shape or alias binding details.

### Why This Waits Until Late

The current routing stack is still carrying compatibility readers and extra quota/reliability vocabulary. Refactoring sticky before those deletions would force the new routing order to preserve old semantics that the roadmap already rejects.

## Topic 2: Provider Quota Strong Deletion

### Current Code Facts

- Provider quota has first-class backend schema and cache identity rooted in credential fingerprint, secret fingerprint, resource scope, and also compatibility channel metadata (`internal/ent/schema/provider_quota_status.go`, `.trellis/spec/backend/credential-routing-model.md`).
- The credential detail page still presents provider quota as a primary product section, which is acceptable for current state but incompatible with the locked "strong delete" target (`frontend/src/features/credentials/components/credential-detail-dialog.tsx:253`).
- Current docs still market provider- or endpoint-level fallback/quota pool behavior as a product capability, especially for Antigravity (`docs/en/guides/antigravity.md:7`, `docs/en/guides/antigravity.md:132`).
- Archived design notes confirm that the existing provider quota subsystem still behaves like a second quota model with checker/cache/status aggregation and UI semantics (`.trellis/tasks/archive/2026-06/06-01-credential-quota-clarity-and-deletion/info.md`).

### Roadmap Decision

Keep:

- raw request failure observation
- request execution history
- ordinary retry/fallback driven by actual upstream outcomes

Delete:

- provider quota as a primary product capability
- checker/cache/status aggregation whose purpose is to expose a second quota truth
- channel/credential UI whose main value is quota-badge semantics rather than ordinary failure observation
- docs that teach dual quota pools, quota-driven endpoint selection, or quota-product language

Do not demote:

- this is a strong delete, not "hide under advanced debug"

### Required Execution Order

1. Remove provider quota from product docs and user-facing explanations.
2. Remove provider quota from routing/product semantics.
3. Remove backend checker/cache/aggregation code and remaining schema/API/UI dependencies.
4. Keep only ordinary error observation needed for request explainability.

### Main Migration Constraint

Anything still using provider quota rows to gate routing must be removed in the same slice. A half-delete would leave hidden routing dependencies behind.

## Topic 3: Legacy Compatibility One-Shot Hard Cut

### Current Code Facts

Backend/schema/API:

- Channel schema still owns sensitive credential JSON and disabled API-key state (`internal/ent/schema/channel.go:108`, `internal/ent/schema/channel.go:109`).
- GraphQL still exposes `ChannelCredentialsInput`, `Channel.credentials`, `Channel.disabledAPIKeys`, channel API-key mutations, and `migrateLegacyChannelCredentials` (`internal/server/gql/axonhub.graphql:200`, `internal/server/gql/axonhub.graphql:720`, `internal/server/gql/axonhub.graphql:725`, `internal/server/gql/axonhub.graphql:891`, `internal/server/gql/axonhub.graphql:923`).
- Ent-generated create/update inputs still preserve inline `credentials` (`internal/server/gql/ent.graphql:1720`, `internal/server/gql/ent.graphql:7093`).

Writers/readers/runtime fallback:

- Channel create/update still persists compatibility credentials on the backend path when sent (`internal/server/biz/channel.go`; current product UI avoids it, but backend compat remains live).
- Runtime channel construction still falls back from `credentialRefs` to inline legacy credentials (`internal/server/biz/channel_llm.go:438`, `internal/server/biz/channel_llm.go:440`).
- Legacy credentials are still turned into executable credential views with fingerprints, key hints, and resource scope keys (`internal/server/biz/channel_credential_identity.go:580`).
- API-key provider still falls back to raw inline keys if no credential views are available (`internal/server/biz/channel_apikey_provider.go:48`, `internal/server/biz/channel_apikey_provider.go:52`).
- `UpstreamCredentialSecret.ToChannelCredentials()` still exists as a compat adapter back to legacy shape (`internal/objects/upstream_credential.go:14`).

Frontend/UI:

- Hidden channel OAuth/API-key code still writes OAuth exchange output into `credentials.apiKey` (`frontend/src/features/channels/components/channels-action-dialog.tsx:358`, `frontend/src/features/channels/components/channels-action-dialog.tsx:363`, `frontend/src/features/channels/components/channels-action-dialog.tsx:372`, `frontend/src/features/channels/components/channels-action-dialog.tsx:381`, `frontend/src/features/channels/components/channels-action-dialog.tsx:1013`).
- Channel dialogs and channel columns still have logic keyed off `channel.credentials` or `channel.disabledAPIKeys` (`frontend/src/features/channels/components/channels-columns.tsx`, `frontend/src/features/channels/components/channels-credentials-dialog.tsx`, `frontend/src/features/channels/components/channels-test-api-keys-dialog.tsx`).

Docs:

- Chinese channel management doc still tells users to list multiple API keys inside `credentials.api_keys` and recover disabled keys from a channel-specific disabled list (`docs/zh/guides/channel-management.md:225`).
- Antigravity doc still teaches OAuth exchange directly into channel configuration (`docs/en/guides/antigravity.md:24`).

### Roadmap Decision

Keep:

- one-shot migration/backfill logic only for the cut
- first-class credential and credential-ref APIs

Delete:

- compat writers
- compat readers in main runtime paths
- GraphQL aliases that preserve channel-owned secret semantics
- channel API-key management mutations and UI
- docs teaching channel-owned API keys/OAuth

Do not demote:

- the user already chose same-slice hard cut instead of a coexistence window

### Required Execution Order

1. Inventory every remaining writer/reader/alias.
2. Backfill existing rows through `migrateLegacyChannelCredentials` or a successor cutover path.
3. Switch all main-path reads/writes to credential/ref surfaces.
4. Delete compat schema/API/runtime/frontend surfaces in the same slice.
5. Verify no hidden runtime path still depends on `Channel.credentials` or `disabled_api_keys`.

### Hard-Cut Boundary

The same implementation slice must include:

- migration/backfill
- new main-path reads/writes
- deletion of old GraphQL inputs/fields/mutations
- deletion of old frontend queries/forms/dialogs/actions
- deletion of runtime fallback readers

If physical DB schema deletion is delayed for operational reasons, the fields can remain empty/dead storage only. They cannot stay on any main path.

## Topic 4: Copy/Export/Masking Surface Collapse

### Current Code Facts

- Request body drawer currently exposes at least two operator copy/export variants:
  - masked JSON copy for request bodies
  - cURL preview generation for request bodies
  (`frontend/src/features/requests/components/request-body-drawer.tsx:103`, `frontend/src/features/requests/components/request-body-drawer.tsx:119`, `frontend/src/features/requests/components/request-body-drawer.tsx:310`)
- cURL generation and masking already live in separate helper code, which means policy is partly centralized but still action-specific (`frontend/src/features/requests/components/request-body-drawer.tsx:32`).
- The roadmap inventory from earlier tasks already identified product sprawl around copy/export/masking variants and requested one shared masking contract.

### Roadmap Decision

Keep:

- one safe default operator copy/export path
- one explicit debug export path only if a real operational need remains after inventory

Delete:

- per-surface copy/export variants that exist mainly because old UI accumulated features
- surface-specific masking forks that diverge in what counts as safe

Demote:

- raw payload/debug export to explicit debug-only flows, never default request inspection UI

### Required Execution Order

1. Inventory all current copy/export entry points across requests, executions, traces, and related detail drawers.
2. Define one masking contract shared by safe export paths.
3. Collapse user-visible actions to the minimum two-path model.
4. Delete redundant actions and helper branches.

### Main Constraint

Do not delete current debug escape hatches before the shared masking contract is trustworthy. This topic is structurally smaller than the compat cut, but it still needs a single policy before code deletion.

## Topic 5: Retry/Fallback And Health-Score Pruning

### Current Code Facts

- Retry policy remains a first-class system config with `enabled`, `max_channel_retries`, `max_single_channel_retries`, `retry_delay_ms`, `load_balancer_strategy`, `auto_disable_channel`, and `empty_response_detection` (`internal/server/biz/system.go:291`).
- Supported strategy names still include `adaptive`, `failover`, `circuit-breaker`, and `sticky-session` (`internal/server/biz/system.go:267`).
- The system settings page still presents retry as a primary admin tab and surfaces those knobs directly (`frontend/src/features/system/components/retry-settings.tsx:16`, `frontend/src/features/system/components/retry-settings.tsx:188`).
- Channel primary buttons still deep-link users to "Load Balancing Strategy" in retry settings (`frontend/src/features/channels/components/channels-primary-buttons.tsx` via grep results).
- Backend normalization still carries a deprecated alias from `weighted` to `failover`, which is another symptom of naming/config compatibility baggage (`internal/server/biz/system.go:1012`).
- Recent routing work already moved execution behavior toward minimal same-target retry and credential-aware fallback, which means the product surface now overstates how much operator tuning should exist (`.trellis/tasks/archive/2026-06/06-02-credential-aware-sticky-fallback/prd.md`).

### Roadmap Decision

Keep:

- structural recovery order
- narrow same-target retry for clearly transient failures
- backend-only normalization while old configs are being cut over

Delete:

- retry-heavy product framing
- health-score style or strategy-explaining product complexity
- tuning knobs that mostly expose implementation branches rather than stable operator intent
- auto-disable-channel as a primary recovery mental model

Demote:

- detailed per-attempt reasoning to logs and request execution history

### Required Execution Order

1. Freeze new retry/fallback knobs.
2. Inventory which existing settings still have real runtime effect.
3. Collapse product config to the minimum structural set.
4. Rewrite docs/UI so fallback, not retry, is the dominant operator concept.
5. Keep backend normalization only long enough to absorb stored legacy config.

### Practical Deletion Target

The likely durable product surface is much smaller:

- minimal same-target retry enablement if still needed
- bounded same-priority and lower-priority fallback behavior
- maybe one upstream error exposure policy if the product still wants it

Everything else should be evaluated as compat baggage until proven otherwise.

## Topic 6: User-Visible Routing Strategy Naming Simplification

### Current Code Facts

- User-facing strategy names currently include `adaptive`, `failover`, `circuit-breaker`, and `sticky-session` (`internal/server/biz/system.go:267`, `frontend/src/features/system/components/retry-settings.tsx:192`).
- Retry settings also render per-strategy documentation blocks, which means the product is still teaching internal execution branches as selectable modes (`frontend/src/features/system/components/retry-settings.tsx:209`).
- There is already at least one alias normalization layer (`weighted` -> `failover`) in the backend (`internal/server/biz/system.go:1012`).
- The routing spec now distinguishes stable contracts like sticky semantic states and fallback ordering from internal execution details, so the UI vocabulary is wider than the durable conceptual model.

### Roadmap Decision

Keep:

- a small, durable user-facing routing/fallback vocabulary
- rich internal execution reasons in logs, traces, and request execution history

Delete:

- near-synonym user-visible strategy proliferation
- names that primarily map to implementation branches

Demote:

- detailed route-branch labels to debug-only observability

### Required Execution Order

1. Inventory every user/admin/doc-visible routing name.
2. Separate product concepts from implementation branches.
3. Collapse product names.
4. Preserve extra detail only in observability surfaces.

### Likely Naming Outcome

The roadmap direction implies user-facing labels should describe intent:

- normal routing
- sticky cache locality
- fallback/recovery behavior

Not backend mechanism names like `adaptive` or `circuit-breaker`.

## Cross-Topic Ordering

The six topics should be executed in this order:

1. `provider-quota-strong-delete`
2. `compat-hard-cut-channel-credential-legacy`
3. `copy-export-masking-collapse`
4. `retry-fallback-surface-pruning`
5. `routing-strategy-name-collapse`
6. `sticky-credential-first-routing-refactor`

### Why This Order

- Provider quota and legacy compat are the biggest semantic conflicts. They must go first.
- Copy/export/masking collapse is mostly a surface-area cleanup and becomes easier once secret ownership and compatibility rules are settled.
- Retry/fallback and naming pruning should happen after the product stops carrying the old quota/compat language.
- Sticky routing refactor goes last because it depends on the semantic surface already being smaller and more coherent.

## Minimum File Sets For Later Tasks

### Provider quota strong delete

Likely core areas:

- `internal/server/biz/provider_quota.go`
- `internal/ent/schema/provider_quota_status.go`
- GraphQL schema/resolvers exposing provider quota surfaces
- frontend credential quota/status views
- docs such as `docs/en/guides/antigravity.md`

### Compat hard cut

Likely core areas:

- `internal/ent/schema/channel.go`
- `internal/server/gql/axonhub.graphql`
- `internal/server/gql/ent.graphql`
- `internal/server/biz/channel.go`
- `internal/server/biz/channel_llm.go`
- `internal/server/biz/channel_apikey_provider.go`
- `internal/server/biz/channel_credential_identity.go`
- `internal/server/biz/channel_apikey.go`
- `internal/server/biz/upstream_credential.go`
- frontend channel data/schema/dialog/action files
- docs teaching channel-owned secrets

### Copy/export/masking collapse

Likely core areas:

- request detail drawers/dialogs
- request/traces export helpers
- masking utilities

### Retry/fallback pruning and naming collapse

Likely core areas:

- `internal/server/biz/system.go`
- system GraphQL/data hooks
- `frontend/src/features/system/components/retry-settings.tsx`
- channel UI shortcuts that point to retry/load-balancer settings
- docs/admin help that explain old strategies

### Sticky credential-first refactor

Likely core areas:

- orchestrator sticky selection and retry/fallback path
- credential selection/provider code
- request execution observability

## Rollout Risks

### Biggest risk

The biggest risk is not deletion itself. It is deleting one layer while another layer still silently depends on the old meaning.

The highest-risk dependency classes are:

- GraphQL generated inputs still requiring compat fields
- hidden frontend code still writing inline credentials
- runtime fallback readers that make old rows continue to work invisibly
- stored retry/load-balancer config values that no longer map cleanly to the reduced product surface

### Mitigation

- Each deletion task needs a full read-path and write-path audit, not just UI removal.
- The compat hard cut must treat generated schema, backend business logic, and frontend forms as one atomic surface.
- The retry/naming tasks must include config normalization and migration, not just text changes.

## Deliverable Use

Use this file as the technical starting point for later implementation tasks. `prd.md` remains the product/decision source. This file explains where the current repo still contradicts that target and which file clusters need to move together.
