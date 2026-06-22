# Local Key Route Templates and Routing Cleanup

## Goal

Move routing ownership from global channel priority/load-balancer behavior to explicit local API key route templates. The goal is to make request routing explainable, reduce accidental user/traffic mixing, and remove route decisions that come from global channel weights, random tie-breakers, or hidden load-balancer scoring.

## What I Already Know

* The current architecture mixes caller identity, route preference, channel priority, sticky session, quota filtering, retry, fallback, and load-balancer scoring.
* The desired direction is not a smarter traffic classifier. It is explicit routing by local API key.
* Local API keys should represent caller/use-case identity.
* Route templates should define allowed channel tiers and fallback boundaries.
* A key may bind to a route template and optionally have a preferred primary channel/assignment.
* Channels should describe capability and availability, not global runtime priority.
* There should be no implicit default route group. A key without explicit routing configuration should fail clearly.
* Existing API key profiles already contain channel/model/profile/quota configuration, but today they filter candidates instead of defining ordered route behavior.
* Existing API key profile templates are the strongest candidate shell for route templates.
* `Channel.ordering_weight` is documented as display sorting but is currently used as runtime route priority.
* `RetryPolicy.LoadBalancerStrategy` currently lets runtime route choice come from global system strategy instead of key-owned route configuration.
* Sticky session currently preserves sessions and also participates in first-bind route choice; only the session preservation part should remain.

## Assumptions

* API key profiles/profile templates can be evolved into route templates without adding a new top-level entity.
* Channel `ordering_weight` should stop participating in runtime routing and remain only for display ordering if retained.
* Sticky session should become scoped to the selected key route template/tier and should not choose first-bind routes through global load balancing.
* Fallback should stay within the selected template and walk route-template order.
* Credential local quota remains a hard credential filter.
* One-shot migration is allowed; runtime compatibility fallback is not allowed.
* Existing `ChannelIDs` / `ChannelTags` can be converted into explicit route tiers during migration, but runtime must not continue interpreting them as alternate route rules.

## Open Questions

* Should existing API key profiles/profile templates be upgraded in place into route templates? Recommended: yes.
* Should key-level preferred channel assignment be optional or required for human/user keys? Recommended: optional globally, strongly visible for human templates.
* Should `Channel.ordering_weight` be renamed to display order now, or simply removed from runtime first and renamed later?
* Should project profile channel filters remain as upper-bound guardrails, or should they be folded into the same route-template model later?
* Should old empty channel filters be migrated into an explicit "all current eligible channels" route tier, or should those keys fail until manually configured? Recommended: migrate once into an explicit visible tier.

## Requirements

* Produce and discuss the deletion/demotion/preservation inventory before implementation.
* Upgrade local API key profiles/profile templates into the target route-template model unless discussion rejects this path.
* Define the minimum product model:
  * local API key has an active route profile/template;
  * route profile/template defines ordered channel tiers;
  * optional key assignment chooses a preferred primary channel inside a tier;
  * stable key affinity may choose a primary when no preferred assignment exists;
  * fallback stays inside the template and follows template order.
* Remove global channel runtime priority from the target design.
* Stop using `Channel.ordering_weight` for runtime route choice.
* Stop exposing global load-balancer strategy as the operator's route policy.
* Preserve hard filters for disabled/archived channels, unsupported model/API format, local quota exhaustion, channel limiter rejection, and circuit-breaker blocks where still needed as safety gates.
* Document how an operator can answer: "Why did this key use this channel?"
* Keep provider quota out of the product model. Provider quota-like responses are attempt failures, not durable routing state.
* Perform a one-shot profile migration that persists route tiers and then removes runtime reads of old route fields.
* Fail clearly when a key has no active route profile or the active profile has no valid route tiers after migration.
* Remove all runtime fallback to old load-balancer behavior.

## Acceptance Criteria

* [x] PRD records the target route-template direction before implementation starts.
* [x] Research inventory lists backend components to delete, demote, preserve, or redesign.
* [x] Research inventory lists frontend/API key UI components to delete, demote, or redesign.
* [x] The target design has no implicit default group.
* [x] The target design has one clear ordering source for runtime routing: local key route template plus optional key assignment.
* [x] Discussion resolves whether to upgrade API key profile templates in place.
* [x] Discussion resolves whether key preferred channel assignment is optional or required for human templates.
* [x] Runtime does not call old load-balanced ordering when route tiers are missing, empty, or invalid.
* [x] Migrated profiles are persisted as explicit route tiers and visible in the API key/profile UI.

## Implemented Decisions

* API key profiles and API key profile templates are upgraded in place; no new top-level route-template entity is introduced in this slice.
* `preferredChannelID` is optional. When it is absent, runtime uses deterministic API-key affinity inside the current route tier.
* New profile/template writes must send explicit `routeTiers`. Empty route tiers fail validation instead of meaning "all channels".
* Legacy `channelIDs`, `channelTags`, and `channelTagsMatchMode` are migration/backfill inputs only. Runtime selection does not read them.
* Sticky-session no longer owns first-bind route choice. It preserves a successful target only when that target remains eligible inside the current API-key route tier.
* `Channel.ordering_weight` remains a display-order field and no longer decides runtime route order in the API-key route-template path.
* `RetryPolicy.LoadBalancerStrategy` is no longer a product route chooser. It is retained only as a legacy settings field; only `sticky-session` enables sticky binding behavior.
* Channel limiter soft mode is presented as non-blocking observation/admission state, not load-balancer down-ranking.

## Definition of Done

* Tests added/updated where behavior changes.
* Lint/typecheck are not run unless explicitly requested by the user.
* Docs/specs updated if routing semantics change.
* Migration/rollback path considered before destructive cleanup.

## Out of Scope

* No code implementation before the investigation is discussed and accepted.
* No automatic traffic-shape classifier.
* No provider-side anti-abuse or evasion design.
* No global “smart” load-balancer replacement.
* No physical deletion of legacy channel credential storage unless explicitly added back into this task.
* No runtime compatibility mode that silently routes with the previous load balancer when route-template data is missing.

## Technical Notes

* `internal/objects/apikey.go` defines `APIKeyProfiles` and `APIKeyProfile`.
* `internal/ent/extra.go` returns nil when a key has no active profile.
* `internal/server/orchestrator/select_candidates.go` applies project profile filters, then orders API-key candidates by explicit route tiers.
* `internal/server/orchestrator/lb_strategy_weight.go` remains legacy/dead primary-routing code after this cut and must not be wired into the main API-key route path.
* `internal/ent/schema/channel.go` describes `ordering_weight` as display sorting, creating a semantic mismatch with runtime use.
* `internal/server/orchestrator/candidates.go` groups candidates by model association priority, then sorts each priority group through `LoadBalancer.Sort`.
* `internal/server/orchestrator/orchestrator.go` logs `api-key-route-tiers` and no longer constructs adaptive/failover/circuit-breaker load balancers for main routing.
* `internal/server/orchestrator/sticky_session.go` originally used load-balanced order for sticky first-bind; the implementation now keeps first-bind inside the API-key route-tier order only.
* `frontend/src/features/channels/components/channels-primary-buttons.tsx` no longer exposes a "Load Balancing Strategy" button.
* `frontend/src/features/channels/components/channels-columns.tsx` exposes `orderingWeight` as display order, not runtime traffic weight.
* `frontend/src/features/apikeys/components/apikeys-*-template-dialog.tsx` already provides profile-template CRUD that can be evolved into route-template UX.

## Research References

* [`research/routing-cleanup-inventory.md`](research/routing-cleanup-inventory.md) — concrete delete/demote/preserve/redesign inventory from backend and frontend inspection.

## Proposed Technical Approach

Recommended approach: evolve API key profiles/profile templates in place and
hard-cut runtime routing to the new route-tier model in the same implementation
slice.

1. Treat API key profile/template as the route policy shell.
2. Add ordered route tiers to the profile/template model.
3. Add optional key preferred channel assignment.
4. Run one-shot migration from old profile allow-lists/tags into explicit tiers.
5. Make candidate construction produce feasible candidates only.
6. Make route-template order the only runtime ordering source.
7. Keep sticky session as a binding lookup/refresh mechanism inside the selected key/template boundary.
8. Keep retry/fallback after failed attempts, but make the fallback order come from route-template tiers.
9. Demote channel ordering weight to UI/display order or remove it from default product UI.

## One-Shot Hard-Cut Plan

The implementation should not ship with two competing routing systems. The
acceptable compatibility boundary is data migration only.

### Data Model

Extend `APIKeyProfile` with explicit route data:

```text
routeTiers:
  - name
    channelIDs: ordered channel IDs
preferredChannelID: optional channel ID for this key/profile
routeMigration:
  version
  source
```

`routeMigration` is metadata for operators and tests. Runtime routing must not
branch on it except for diagnostics.

### Migration

Run an idempotent one-shot migration for existing API key profiles and API key
profile templates:

1. If `routeTiers` already exists and is non-empty, keep it.
2. Else if old `ChannelIDs` is non-empty, create one explicit tier from those IDs
   in stored order.
3. Else if old `ChannelTags` is non-empty, resolve matching channels once and
   store the resulting ordered channel IDs as an explicit tier.
4. Else create one explicit migrated tier from the current enabled channels
   visible to the project/profile at migration time.
5. If a tier cannot be created, mark the profile invalid and make runtime fail
   with a clear "route profile has no channels" error.

Important: step 4 is not a runtime default group. It is a persisted migration
artifact visible in the UI. New profiles must not get this behavior implicitly.

### Runtime Selection

Runtime route selection must follow this order:

1. Build the full feasible candidate set from model association, API format,
   stream/native-tool capability, channel/credential status, and credential
   local quota.
2. Load the active API key profile.
3. Require non-empty `routeTiers`.
4. Intersect feasible candidates with tier 0. If empty, continue to tier 1, then
   later tiers.
5. Within the selected tier:
   - use `preferredChannelID` when it is configured and feasible;
   - otherwise use deterministic key affinity over the tier's ordered channels;
   - never use global channel weight, random score, adaptive scoring, or old load
     balancer ordering.
6. Sticky binding may keep a session on a target only if the target remains
   inside the active key/profile/tier boundary.
7. Fallback after an attempt failure walks the same explicit route-tier order.

### Forbidden Runtime Fallbacks

These are explicitly forbidden:

* Missing `routeTiers` -> old `ChannelIDs` / `ChannelTags` runtime filter.
* Missing `routeTiers` -> all channels.
* Empty selected tier -> global load balancer.
* Sticky key missing -> global load balancer.
* Preferred channel unavailable -> random channel.
* Unknown strategy value -> adaptive.
* Equal candidates -> `ordering_weight` tie-break.
* Fallback after user-visible streaming output has started.

### UI Cutover

API key profile/template UI becomes the route policy UI:

* replace "Allowed channels" with ordered route tiers;
* allow dragging channels within a tier;
* allow adding fallback tiers;
* provide an "add by tag" helper that expands to concrete channel IDs on save;
* show `preferredChannelID` for this key/profile;
* show why a route is currently unavailable when all channels in a tier are
  filtered by hard state.

Channel UI must stop implying runtime traffic priority:

* remove "Load Balancing Strategy" from the channel page;
* remove or rename "Weight" as display order;
* keep channel health/limiter state as observability, not route policy.

### Retry / Fallback Cutover

Retry/fallback must operate on explicit attempt targets produced from route
tiers:

* fallbackable pre-output attempt failures may move to the next target in the
  same tier, then the next tier;
* request-invalid errors do not fallback;
* same-target retry must be explicit in diagnostics and must not hide the failed
  attempt;
* streaming after user-visible output started returns the failure instead of
  silently switching targets.

### Rollback

Rollback is operational, not runtime compatibility:

* take a database backup before migration;
* keep migration idempotent;
* if rollback is needed, restore the old binary plus database backup;
* do not keep an old-router compatibility branch in the new runtime.

## Initial Deletion Inventory

Delete from runtime route choice:

* `WeightStrategy`
* `WeightRoundRobinStrategy` runtime weight behavior
* `LoadBalancer.sortProduction` ordering-weight tie-breaker
* `RandomStrategy` for primary route tie-breaking
* global `RetryPolicy.LoadBalancerStrategy` as a product route chooser
* sticky first-bind via load-balanced candidate order
* adaptive/failover/circuit-breaker as user-selectable primary routing modes
* runtime interpretation of old `APIKeyProfile.ChannelIDs` / `ChannelTags` after migration

Demote or rename:

* channel `ordering_weight`
* `BulkUpdateChannelOrdering`
* channel table "Weight" column
* channel bulk ordering dialog
* channel page "Load Balancing Strategy" button

Preserve as hard filters/admission/diagnostics:

* disabled/archived channel and credential filtering
* credential local quota filtering
* model/API-format/capability filters
* channel limiter hard queue admission
* explicit `Retry-After` cooldown handling
* model circuit breaker as a hard gate, if still needed
* sticky key extraction and success-time binding refresh
* fallback after pre-output attempt failure
