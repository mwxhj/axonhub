# Routing Cleanup Inventory

## Purpose

This document records the pre-implementation investigation for moving routing
ownership from global channel priority/load-balancer behavior to explicit local
API key route templates.

The target product rule is:

```text
Local API key -> route template/profile -> ordered channel tiers -> concrete attempt target
```

Channel-level global priority must stop being a runtime route chooser.

## Files Inspected

- `internal/server/orchestrator/orchestrator.go`
- `internal/server/orchestrator/select_candidates.go`
- `internal/server/orchestrator/candidates.go`
- `internal/server/orchestrator/sticky_session.go`
- `internal/server/orchestrator/retry.go`
- `internal/server/orchestrator/load_balancer.go`
- `internal/server/orchestrator/lb_strategy_weight.go`
- `internal/server/orchestrator/lb_strategy_rr.go`
- `internal/server/orchestrator/lb_strategy_random.go`
- `internal/server/orchestrator/lb_strategy_rate_limit.go`
- `internal/server/orchestrator/lb_strategy_latency.go`
- `internal/server/orchestrator/model_circuit_breaker.go`
- `internal/server/orchestrator/channel_limiter.go`
- `internal/server/orchestrator/channel_limiter_manager.go`
- `internal/server/orchestrator/rate_limit_tracking.go`
- `internal/server/biz/system.go`
- `internal/server/biz/system_default.go`
- `internal/server/biz/api_key_profile_template.go`
- `internal/objects/apikey.go`
- `internal/ent/schema/channel.go`
- `frontend/src/features/channels/components/channels-primary-buttons.tsx`
- `frontend/src/features/channels/components/channels-columns.tsx`
- `frontend/src/features/channels/components/channels-bulk-ordering-dialog.tsx`
- `frontend/src/features/apikeys/components/apikeys-profiles-dialog.tsx`
- `frontend/src/features/apikeys/components/apikeys-profile-templates-dialog.tsx`
- `frontend/src/features/apikeys/components/apikeys-create-template-dialog.tsx`
- `frontend/src/features/apikeys/components/apikeys-edit-template-dialog.tsx`
- `frontend/src/features/apikeys/data/schema.ts`
- `.trellis/spec/backend/routing-guidelines.md`
- `.trellis/spec/backend/credential-routing-model.md`
- `.trellis/spec/frontend/human-computer-interaction.md`

## Current Runtime Shape

### Candidate Construction

Current candidate construction is mostly reasonable as a hard feasibility phase:

1. `DefaultSelector.Select` resolves the requested model through AxonHub model
   associations.
2. `selectModelCandidates` resolves model associations into
   `ChannelModelsCandidate` values with a `Priority`.
3. Project profile filters narrow candidates by `ChannelIDs` and `ChannelTags`.
4. API key profile filters narrow candidates by `ChannelIDs` and `ChannelTags`.
5. Google/Anthropic native tool filters and stream policy filters narrow by
   request capability.

Problem: API key profile `ChannelIDs` are only a filter. They do not define an
ordered route. The later ordering phase still comes from global channel priority
and load-balancer strategy.

### Primary Ordering

`orderCandidates` chooses between:

- `StickySessionRouter.Order` when system strategy is `sticky-session`;
- `loadBalancedCandidates` for every other system strategy.

`loadBalancedCandidates` groups candidates by model association `Priority`, then
sorts each priority group by `LoadBalancer.Sort`.

Configured runtime strategy comes from `RetryPolicy.LoadBalancerStrategy`.
Supported values are:

- `adaptive`
- `failover`
- `circuit-breaker`
- `sticky-session`

This is the core source of semantic complexity: the first attempt can be decided
by global system strategy, channel weight, historical metrics, random tie-breaks,
rate-limit score, latency score, circuit-breaker score, sticky binding, and model
association priority.

### Sticky Session

Sticky session currently has two responsibilities:

1. Extract a sticky identity from request/session/response/prefix signals.
2. Pick/order candidates when the system strategy is `sticky-session`.

The extraction scope already includes API key ID, project ID, API key profile,
project profile, model, request type, and API format.

Problem: sticky still chooses first-bind by falling back to load-balanced order.
That makes sticky a route chooser instead of only a session binding mechanism.

### Retry/Fallback

The pipeline retry options come from `RetryPolicy`:

- `MaxChannelRetries`
- `MaxSingleChannelRetries`
- `RetryDelayMs`
- `EmptyResponseDetection`

Error classification is currently simple:

- Retryable: retryable HTTP status.
- Fallbackable: queue errors, circuit-breaker skips, empty responses, 401/402/403/404/408/429, 5xx, upstream transport errors.
- Not fallbackable: 400.

Problem: retry and fallback are coupled to the ordered candidate list built
before attempts. They do not yet operate on route-template tiers as the single
source of order.

### Channel Weight

`Channel.ordering_weight` is defined in Ent as:

```text
Ordering weight for display sorting
```

But runtime uses it in multiple places:

- `WeightStrategy.Score`
- `WeightRoundRobinStrategy`
- `LoadBalancer.sortProduction` tie-breaker
- channel list ordering / bulk ordering UI labelled "Weight"

This is a direct semantic mismatch. The same field is both display ordering and
runtime route priority.

### API Key Profiles and Templates

Existing `APIKeyProfile` fields:

- `Name`
- `ModelMappings`
- `Quota`
- `ChannelIDs`
- `ChannelTags`
- `ChannelTagsMatchMode`
- `ModelIDs`

Existing API key profile templates persist an `APIKeyProfile` and can load it
into an API key.

This is a good migration shell, but it is not yet a route template because:

- `ChannelIDs` are unordered;
- there is no explicit tier model;
- there is no key-level preferred channel assignment;
- an empty channel filter means "all candidates", which conflicts with the new
  "no implicit default group" rule.

## Delete / Demote / Preserve / Redesign

### Delete Runtime Semantics

These should stop affecting runtime route choice in this task.

| Component | Current Role | Target |
| --- | --- | --- |
| `WeightStrategy` | Scores candidates by channel `ordering_weight`. | Delete from runtime load balancer. |
| `WeightRoundRobinStrategy` runtime weight behavior | Uses channel weight to proportionally distribute traffic. | Delete or replace with template-tier ordering. |
| `LoadBalancer.sortProduction` ordering-weight tie-breaker | Uses `OrderingWeight` when strategy scores tie. | Remove. Tie-break must come from route template order / stable key affinity. |
| `RetryPolicy.LoadBalancerStrategy` as product choice | Lets operators choose global routing strategy. | Freeze/migrate to the single route-template strategy; remove UI entry. |
| `adaptive` as first-attempt route chooser | Mixes trace, error, weight round-robin, latency, rate-limit scores. | Delete as primary chooser. Some observations may remain diagnostics. |
| `failover` as first-attempt route chooser | Weight + random + rate-limit sort. | Delete as separate strategy. Fallback order comes from route template. |
| `circuit-breaker` as first-attempt route chooser | Weight + circuit-breaker score + rate-limit sort. | Delete as separate strategy. Circuit breaker can remain a hard gate. |
| `RandomStrategy` as tie-breaker | Adds random score to equal candidates. | Delete for primary routing. Randomness hurts explainability and user/channel isolation. |
| Sticky first-bind via `loadBalancedCandidatesWithoutTracking` | First sticky bind is chosen by global load-balancer results. | Replace with route-template primary selection. Sticky should not choose routes. |
| `WithLoadBalancedSelector` decorator | Sorts candidates inside selector. | Remove if unused after route-template ordering. Selection should build feasible candidates only. |

### Demote to UI-only or Rename

These can remain as operator organization tools, but not runtime behavior.

| Component | Current Role | Target |
| --- | --- | --- |
| `Channel.ordering_weight` storage | Display comment, runtime priority today. | Keep only as `display_order` semantics or hide behind channel table sorting. |
| `BulkUpdateChannelOrdering` GraphQL/API | Updates channel ordering weight. | Rename/copy semantics to display order; remove wording that implies traffic weight. |
| Channel table `orderingWeight` column | Visible "Weight" column with inline editing. | Rename to "Display order" or remove from default columns. |
| `channels-bulk-ordering-dialog.tsx` | Drag/drop "Weight" ordering. | Keep only as table/display ordering if still useful. |
| Channel primary button "Load Balancing Strategy" | Navigates to a non-existent/legacy retry tab. | Remove. Route policy should live under API key route templates. |

### Preserve as Hard Filters / Admission

These should remain, but they must not become an alternate route-ordering source.

| Component | Current Role | Target |
| --- | --- | --- |
| Disabled/archived channel filtering | Excludes unusable channels. | Preserve. |
| Disabled/archived credential filtering | Excludes unusable credential views. | Preserve. |
| Credential local quota (`CredentialQuotaScope`) | Filters exhausted/paused/disabled credential view. | Preserve as key/credential-local quota only. |
| Model association feasibility | Maps request model to possible channel/model/API format candidates. | Preserve, but route template should constrain/order the candidate set. |
| API format / stream / native tool filters | Capability filters. | Preserve as hard feasibility filters. |
| Channel limiter hard queue rejection | Admission control after target selection. | Preserve. Queue full/timeout is attempt failure. |
| Rate-limit tracker cooldown from `Retry-After` | Temporary hard-ish local state from explicit upstream header. | Preserve carefully as eligibility/admission signal, not scoring. |
| Model circuit breaker open/probe logic | Avoids known failing channel/model pairs. | Preserve as explicit hard gate if enabled by product rule, not as a separate global strategy. |
| Request/usage/cost persistence | Observability and billing. | Preserve. |
| Sticky key extraction and success-time binding refresh | Session continuity. | Preserve, scoped under route template/tier. |
| Fallback after pre-output attempt failure | Recovery behavior. | Preserve, but source order must be route template. |

### Redesign into Route Template Semantics

These are existing concepts that should be reshaped rather than deleted.

| Component | Current Role | Target |
| --- | --- | --- |
| `APIKeyProfile.ChannelIDs` | Unordered allow-list filter. | Replace/evolve into ordered route tiers. |
| `APIKeyProfile.ChannelTags` | Broad channel tag filter. | Keep as optional template expansion/filter, but expanded result must be explainable. |
| `APIKeyProfileTemplate` | Saves reusable API key profile. | Recommended shell for `Route Template`; add route tiers and optional key assignment. |
| `APIKeyProfile.ModelIDs` | Model access filter. | Preserve as model access policy within key/template. |
| `APIKeyProfile.ModelMappings` | Key-level model aliasing. | Preserve. It is key-local and understandable. |
| `APIKeyProfile.Quota` | Local API key quota. | Preserve. It is caller quota, not channel/provider quota. |
| Project profile channel filters | Upper boundary filters. | Re-evaluate after route-template design. They may remain as admin guardrail, but must not silently inject a default route. |

## Product Model Recommendation

Recommended minimum model:

```text
API key
  active route template/profile
  optional preferred primary channel assignment

Route template/profile
  name
  model access
  model mappings
  local caller quota
  route tiers:
    tier 0: ordered channel IDs or tag-expanded channel set
    tier 1: ordered channel IDs or tag-expanded channel set
    ...
```

Selection semantics:

1. Resolve model/capability feasible candidates.
2. Apply project guardrails, if any.
3. Require active API key route template/profile.
4. Intersect feasible candidates with the current route template.
5. Pick the first eligible tier.
6. Inside the tier:
   - use key-level preferred assignment if configured and feasible;
   - otherwise use stable key affinity, not random/global load balancing;
   - preserve template order as the explainable fallback order.
7. Sticky binding can keep an existing session on a concrete target only if that
   target is still inside the active key/template/tier boundary.
8. Fallback walks the route-template order after an attempt failure and never
   silently crosses outside the template.

## One-Shot Cutover Recommendation

If this is implemented in one slice, the cutover should be a hard runtime cut,
not a dual-router compatibility period.

Allowed compatibility:

- one-shot persisted migration from old profile fields to route tiers;
- operator-visible migration metadata;
- clear startup/admin diagnostics for profiles that could not be migrated.

Forbidden compatibility:

- runtime reads old `ChannelIDs` / `ChannelTags` when `routeTiers` is missing;
- runtime treats missing route tiers as "all channels";
- runtime falls back to `adaptive`, `failover`, `circuit-breaker`, or sticky
  load-balanced first-bind;
- runtime uses channel `ordering_weight` as tie-break;
- runtime uses random tie-break when the new route model is underspecified.

Migration rules:

1. Existing explicit route tiers win.
2. Old `ChannelIDs` becomes one ordered tier.
3. Old `ChannelTags` is expanded once into explicit channel IDs.
4. Old empty filters are converted once into an explicit visible tier containing
   current enabled channels, or the profile is marked invalid if that cannot be
   resolved.
5. New profiles must explicitly define tiers; they never inherit "all channels"
   from an empty route.

Runtime error rules:

- No active API key profile: fail clearly.
- Active profile has no route tiers: fail clearly.
- Route tiers exist but no feasible candidate remains after hard filters: fail
  clearly with filtered reasons.
- Sticky key unavailable: use the route template primary rule, not old load
  balancing.
- Preferred channel unavailable: use deterministic key affinity within the
  selected tier, not random/global load balancing.

## Concrete Components Likely Removed in Implementation

Backend:

- Remove `WeightStrategy` from runtime path.
- Remove channel `OrderingWeight` tie-breaker from `LoadBalancer.Sort`.
- Remove `adaptiveLoadBalancer`, `failoverLoadBalancer`, and
  `circuitBreakerLoadBalancer` as user-selectable primary routing modes.
- Replace `deriveLoadBalancerStrategy` with route-template semantics.
- Remove or neuter `RetryPolicy.LoadBalancerStrategy`.
- Remove sticky first-bind load-balancer fallback.
- Remove `LoadBalancedSelector` if no remaining production caller exists.
- Remove route decisions based on latency/error/round-robin/random scores.

Frontend/API:

- Remove channel page "Load Balancing Strategy" button.
- Remove or rename channel "Weight" column.
- Remove or rename bulk channel ordering as display ordering only.
- Remove system load-balancer strategy controls if any stale route still exposes
  them.
- Redesign API key profile/template dialogs from allow-list controls to route
  tiers plus optional preferred assignment.

Database/schema:

- Do not physically delete `ordering_weight` in the first implementation slice
  unless migration scope is accepted. Demote runtime meaning first.
- Keep `Channel.credentials` / `disabled_api_keys` cleanup separate unless the
  task explicitly includes legacy storage deletion again.

Tests likely impacted:

- `lb_strategy_weight_test.go`
- `lb_strategy_rr_test.go`
- `lb_strategy_random_test.go`
- `lb_strategy_latency_test.go`
- `lb_strategy_composite_test.go`
- load-balancer simulation tests
- sticky-session tests involving first-bind ordering
- candidate load-balancing tests
- channel ordering frontend tests
- API key profile/template frontend tests

## Risks

- A hard one-shot deletion will break many tests because current tests encode
  global load-balancer behavior.
- Removing runtime `ordering_weight` without UI rename will leave misleading
  product language.
- Stable key affinity can still collide two users onto the same channel if no
  preferred assignment exists. If "one user mostly owns one channel" is a hard
  product requirement, key-level preferred assignment is required.
- Keeping project profile filters may still create hidden routing boundaries.
  They need explicit language as guardrails, not route templates.
- If route templates allow tag expansion, the expanded result must be visible in
  the UI; otherwise tags become another hidden routing rule.

## Discussion Question

Recommended decision:

Use existing API key profiles/profile templates as the route-template shell, and
add route tiers plus optional key preferred assignment there. Do not introduce a
separate new route-template entity unless schema constraints block the migration.

Reason:

- The existing product already teaches users to configure per-key profiles.
- Templates already exist and are project-scoped.
- Model mappings, model access, and caller quota already belong there.
- The main missing piece is ordered route tiers, not a new top-level concept.
