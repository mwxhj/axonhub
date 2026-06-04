# Feature Pruning Roadmap and Semantic Simplification

## Goal

Produce one complete roadmap document for a coordinated deletion/simplification pass across routing, quota, compatibility, retry/fallback, and debugging surfaces. The target end state is a smaller product with stronger semantic consistency: channels describe execution surfaces, credentials own upstream identity and quota, sticky routing is explicit and predictable, and operators do not need to understand multiple overlapping reliability/quota models to explain one request.

This task is documentation-first. It defines what to delete, what to demote, what to hard-cut, and what must remain because it is part of the durable routing model.

## What I Already Know

* The user wants one complete pruning roadmap, not piecemeal note-taking.
* The project has already moved toward a credential-owned model:
  * `UpstreamCredential` owns API keys / OAuth / cloud credentials.
  * `ChannelCredentialRef` is the intended channel-to-credential binding layer.
  * `CredentialQuotaScope` is the intended local key quota model.
* The project direction already rejects channel-local quota and double quota.
* Sticky-session recently exposed an architectural mismatch:
  * priority/weight live at channel level;
  * quota and affinity live at credential/key level;
  * current first-bind is still channel-first and then key-corrected.
* The user agrees with these pruning directions:
  1. rethink sticky target selection around key/credential affinity rather than channel-first correction;
  2. remove provider quota as a primary product capability;
  3. remove old compatibility fields / backfill / legacy alias layers;
  4. collapse copy/export/masking variants;
  5. simplify retry/fallback/health-score knobs;
  6. simplify user-visible routing strategy names.
* The user clarified topic 1:
  * the issue is not "channel vs key" as a simple either/or;
  * the issue is that the decision order is wrong;
  * quota ownership belongs to key/credential;
  * execution still resolves to channel + credential.
* The user locked two strategic calls for this roadmap:
  * provider quota = strong delete;
  * compatibility cleanup = one-shot cut in the same implementation slice that performs migration/backfill.

## Assumptions

* The roadmap should optimize for strong semantic consistency, not backward-compatibility comfort.
* "Delete" may mean one of three things:
  * hard delete from code/schema/product,
  * demote from primary UI to debug/detail surface,
  * or in a few cases keep as internal-only execution/debug state.
* It is acceptable for the roadmap to require multiple later implementation slices, but the semantic target state should be defined now as one coherent design.
* The roadmap should treat request explainability and operator understanding as first-class product requirements.

## Locked Decisions

These are fixed roadmap inputs, not open debate:

1. Sticky routing must stop being "channel-first plus key correction as a patch layer".
2. Sticky affinity and quota reasoning belong to credential / resource-scope semantics inside a selected priority tier.
3. Final execution still resolves to concrete `channel + credential`; the target model is not "key only".
4. Provider quota is a strong-delete target, not a demotion target.
5. Legacy compatibility cleanup is a one-shot cut in the same implementation slice that performs migration/backfill.
6. Copy/export/masking variants should be collapsed aggressively.
7. Retry/fallback should be simplified toward a small structural rule set, with health-score style complexity removed.
8. User-visible routing strategy names should be simplified; detailed internal states remain debug-only.

## Requirements

### Product-Level Rules

* Every user-facing routing or quota concept must map to one stable semantic contract.
* Silent semantic fallback must be eliminated or turned into explicit degraded states.
* Features that are not part of the durable routing model should be removed or demoted out of the primary operator path.
* The roadmap must separate:
  * quota ownership,
  * routing decision order,
  * execution target materialization,
  * debug/observability surfaces.

### Roadmap Coverage

The document must fully cover all six pruning topics:

1. Sticky-session decision order and binding semantics.
2. Provider quota product surface and routing role.
3. Legacy compatibility fields, backfill code, and aliases.
4. Copy/export/masking surface sprawl.
5. Retry/fallback/health-score configuration sprawl.
6. User-visible routing strategy naming sprawl.

### Decision Format

For each topic, the roadmap must specify:

* Current problem.
* Target semantic model.
* Keep / delete / demote decision.
* Migration or rollout constraints.
* Recommended implementation order.
* Main risk if we move too early.
* Main cost if we delay.

### Sticky Routing Direction

The roadmap must explicitly capture this routing direction:

* priority tier remains a channel/service-level concept;
* quota and sticky affinity move to credential/resource-scope decision semantics inside the selected tier;
* final execution still resolves to a concrete `channel + credential` target;
* sticky should not remain "channel-first with key correction as a patch layer";
* sticky semantic states must remain explicit:
  * binding hit,
  * documented rebind policy,
  * documented degrade path.

### Provider Quota Direction

The roadmap must treat provider quota as:

* deleted as a primary product capability,
* deleted from backend checker/cache/status aggregation rather than merely demoted,
* replaced only by raw request failure/error observation that does not create a second quota model.

### Compatibility Cleanup Direction

The roadmap must define a same-slice hard cut for old compatibility surfaces:

* perform migration/backfill,
* switch main-path reads/writes,
* remove compatibility API/schema/runtime surfaces in the same slice,
* remove dead code and migration helpers rather than keeping a long compatibility window.

### Reliability Simplification Direction

The roadmap must define a simpler fallback model:

* narrow same-target retry,
* favor same-channel credential fallback,
* then same-priority fallback,
* then lower-priority fallback only after exhaustion,
* remove health-score style tuning that weakens explainability.

## Final Roadmap

### Topic 1: Sticky-session semantic simplification

#### Current problem

The current design mixes three concerns in the wrong order:

* priority and weight are channel-level concepts;
* quota and affinity are credential-level concepts;
* sticky first-bind still starts from a channel-first primary choice and only later corrects the credential.

This creates semantic drift:

* one top-level sticky label hides multiple distinct behaviors;
* quota reasoning becomes secondary to channel-first selection;
* the system is harder to explain because cache locality, execution surface, and credential budget are resolved in different layers.

#### Target semantic model

Sticky routing should mean:

1. select the best eligible priority tier;
2. choose credential / resource-scope affinity inside that tier;
3. resolve to a concrete execution surface `channel + credential`;
4. preserve explicit semantic state:
   * `binding-hit`
   * `rebind-policy`
   * `degraded`

#### Keep / Delete / Demote

* Keep:
  * sticky-session as a cache-locality mechanism
  * priority tiers as channel/service-level policy
  * concrete execution as `channel + credential`
* Delete:
  * silent semantic fallback
  * channel-first first-bind as the long-term architecture
  * "ordinary load balancing" as an implicit sticky sub-behavior
* Demote:
  * fine-grained internal sticky branch names stay debug-only, not user-facing strategy vocabulary

#### Recommended implementation order

1. Make semantic states explicit in code, logs, request state, and debug output.
2. Remove silent fallback semantics.
3. Refactor first-bind/rebind from channel-first to credential/resource-scope-first within the chosen priority tier.
4. Revisit binding persistence shape only after routing order is corrected.

#### Main risk if moved too early

If execution target materialization is removed too aggressively, routing can lose endpoint/baseURL/transformer context and break valid execution surfaces.

#### Main cost if delayed

Sticky keeps leaking multiple behaviors under one name, making every routing bug harder to explain and every future simplification more expensive.

---

### Topic 2: Provider quota strong deletion

#### Current problem

Provider quota currently behaves like a second quota model:

* it competes with local quota for operator attention;
* it introduces heterogeneous semantics across providers;
* it increases routing and UI complexity;
* it weakens the rule that one visible concept should map to one stable meaning.

#### Target semantic model

The product should have one quota model:

* local credential quota is the only explicit quota product concept;
* upstream/provider failures are treated as ordinary request execution failures;
* there is no independent provider-quota subsystem driving product explanation or routing identity.

#### Keep / Delete / Demote

* Keep:
  * ordinary request execution failures
  * retry/fallback behavior driven by actual request outcomes
  * debug/request snapshots that already belong to request execution
* Delete:
  * provider quota UI
  * provider quota checker/cache/status aggregation
  * product-level provider quota badges or route-availability semantics
* Do not demote:
  * this is not "hide in debug for now"; it is a strong-delete target

#### Recommended implementation order

1. Remove provider quota from UI and docs as a product concept.
2. Remove provider quota from routing semantics.
3. Remove backend checker/cache/status aggregation paths.
4. Confirm request failure observation still covers operational needs.

#### Main risk if moved too early

If some fallback or blocking logic still secretly depends on provider quota rows, deleting the subsystem before that dependency is removed can create hidden routing regressions.

#### Main cost if delayed

The system keeps carrying two overlapping quota truths, which confuses both the code and the operator experience.

---

### Topic 3: Legacy compatibility one-shot hard cut

#### Current problem

Old compatibility fields, aliases, readers, and backfill helpers keep the old model alive:

* channel-owned secret shapes still exist as live compatibility surfaces;
* GraphQL/schema compatibility can keep old semantics routable by accident;
* every cleanup must account for both old and new ownership models.

#### Target semantic model

There is one durable model only:

* Channel = routing surface
* UpstreamCredential = upstream identity
* ChannelCredentialRef = binding
* CredentialQuotaScope = local credential quota

No long-lived compatibility shadow model remains in the same implementation area.

#### Keep / Delete / Demote

* Keep:
  * one-shot migration/backfill logic only as much as required for the cut
* Delete:
  * compat writers
  * compat readers in main path
  * GraphQL/schema aliases that preserve old product meaning
  * runtime fallback readers that keep old semantics alive
* Do not demote:
  * the user chose same-slice hard cut instead of a compatibility-window coexistence

#### Recommended implementation order

1. Inventory every compatibility writer/reader/alias.
2. Implement migration/backfill for existing data.
3. Switch all main-path reads/writes to the new model.
4. Delete compat schema/API/runtime surfaces in the same implementation slice.
5. Verify no remaining path depends on old fields.

#### Main risk if moved too early

A missed runtime/schema dependency will fail hard rather than degrade gracefully, because this roadmap explicitly rejects a long compatibility window.

#### Main cost if delayed

The codebase keeps paying a permanent ambiguity tax and future deletions become harder because more new logic accumulates around compatibility.

---

### Topic 4: Copy/export/masking surface collapse

#### Current problem

The product carries too many subtly different copy/export variants:

* body vs cURL vs export payload
* different masking rules
* different operator expectations

This creates both maintenance risk and security risk.

#### Target semantic model

There should be only two output classes:

1. safe default copy/export
2. explicit debug export

Masking must come from one shared policy, not per-surface improvisation.

#### Keep / Delete / Demote

* Keep:
  * one safe default operator-facing copy/export path
  * one explicit debug path if truly needed
* Delete:
  * most copy/export variants
  * surface-specific masking forks
* Demote:
  * deep/raw debug payloads to detail or debug-only surfaces

#### Recommended implementation order

1. Inventory all copy/export entry points and their masking behavior.
2. Define one masking contract.
3. Replace product actions with the minimum two-path model.
4. Delete unused variants and dead helpers.

#### Main risk if moved too early

If the unified masking contract is underspecified, deletion can remove a legitimately useful operator path before the replacement is trustworthy.

#### Main cost if delayed

More variants accumulate, and secret-handling consistency keeps drifting.

---

### Topic 5: Retry/fallback and health-score pruning

#### Current problem

The system still exposes too much reliability tuning surface:

* retry-centric settings overlap with fallback semantics;
* health-score style ideas increase hidden decision complexity;
* the operator cannot easily explain why one request moved the way it did.

#### Target semantic model

Reliability should be expressed through a small structural rule set:

1. minimal same-target retry
2. same-channel credential fallback
3. same-priority fallback
4. lower-priority fallback only after exhaustion

No large tuning surface should remain around this.

#### Keep / Delete / Demote

* Keep:
  * structural fallback ordering
  * narrow transient retry where justified
* Delete:
  * health-score style complexity
  * retry-heavy configuration sprawl
  * hidden time-budget or similar opaque escape logic
* Demote:
  * detailed execution reasons to logs/debug rather than user-facing control knobs

#### Recommended implementation order

1. Freeze new retry/fallback knobs.
2. Inventory which knobs still affect execution.
3. Collapse config surface to the minimum structural set.
4. Update docs/UI so fallback, not retry, is the dominant mental model.

#### Main risk if moved too early

If some providers still need special-case retry behavior that has not been codified structurally, over-aggressive deletion can remove real resilience before replacement logic exists.

#### Main cost if delayed

Reliability remains hard to reason about and keeps conflicting with the new sticky/credential-first direction.

---

### Topic 6: User-visible routing strategy naming simplification

#### Current problem

Too many strategy names or half-product / half-internal labels create false choice:

* users think they are choosing distinct product modes;
* in reality they are often choosing between implementation branches;
* documentation and debugging become harder because names do not cleanly map to mental models.

#### Target semantic model

The user-facing routing surface should expose only a small number of stable modes. Internal execution states remain richer, but they belong to debug and request observability, not to the main configuration vocabulary.

#### Keep / Delete / Demote

* Keep:
  * a small user-facing routing mode set
  * rich internal execution/debug states
* Delete:
  * user-visible proliferation of near-synonym routing names
* Demote:
  * implementation-state labels to logs, traces, and request execution metadata

#### Recommended implementation order

1. Inventory all routing names seen by users/admins/docs.
2. Separate product modes from internal execution states.
3. Collapse product naming.
4. Preserve internal detail only in debug/observability surfaces.

#### Main risk if moved too early

If user-visible names are collapsed before internal observability improves, operators may temporarily lose useful distinctions during diagnosis.

#### Main cost if delayed

The product keeps teaching operators implementation details instead of stable user concepts.

## Cross-Topic Sequencing

The roadmap is unified, but implementation should still be staged in a strict order so semantic cleanup is not undermined by leftover old surfaces.

### Phase A: Semantic freeze

Goals:

* stop adding new complexity;
* define the durable target state before deletion.

Contents:

* sticky semantic-state cleanup
* retry/fallback simplification rules
* routing naming simplification rules

### Phase B: Hard product-surface deletion

Goals:

* remove product concepts that the new model rejects.

Contents:

* provider quota strong deletion
* copy/export/masking collapse
* user-facing routing naming collapse

### Phase C: Ownership hard cut

Goals:

* complete the same-slice compatibility cut.

Contents:

* migration/backfill
* switch to new reads/writes
* delete compatibility schema/runtime paths

### Phase D: Routing architecture correction

Goals:

* make implementation match the simplified model.

Contents:

* refactor sticky first-bind/rebind to credential/resource-scope-first within priority tier
* narrow fallback structure around the simplified model

## Recommended Implementation Slices

Even though the semantic roadmap is unified, execution should still be split into later tasks:

1. `provider-quota-strong-delete`
2. `compat-hard-cut-channel-credential-legacy`
3. `copy-export-masking-collapse`
4. `retry-fallback-surface-pruning`
5. `routing-strategy-name-collapse`
6. `sticky-credential-first-routing-refactor`

The sticky refactor is deliberately last because it depends on the semantic cleanup from the earlier slices.

## Acceptance Criteria

* [ ] `prd.md` defines one coherent target state for all six pruning topics.
* [ ] The roadmap distinguishes keep/delete/demote for each topic and explicitly calls out the same-slice hard-cut decision for compatibility cleanup.
* [ ] Sticky routing direction is captured precisely enough to avoid "channel-only" vs "key-only" confusion.
* [ ] Provider quota treatment is explicit rather than left ambiguous.
* [ ] Compatibility removal order is spelled out as a same-slice migration + cut sequence.
* [ ] The roadmap can be split into later implementation tasks without reopening the semantic debates from scratch.

## Definition of Done

* A complete roadmap document exists in this task.
* The document is specific enough to drive later code tasks.
* It reflects the current user decisions from discussion, not generic advice.
* Any still-open strategic question is reduced to zero blocking ambiguity for later implementation planning.

## Out of Scope

* Implementing the pruning itself in this task.
* Running migrations in this task.
* Deleting code in this task.
* Finalizing exact GraphQL schema diffs in this task.

## Technical Notes

* Prior inventory task: [`06-02-feature-pruning-legacy-cleanup/prd.md`](../06-02-feature-pruning-legacy-cleanup/prd.md)
* Credential ownership model: [../../spec/backend/credential-routing-model.md](../../spec/backend/credential-routing-model.md)
* Sticky/retry routing constraints: [../../spec/backend/routing-guidelines.md](../../spec/backend/routing-guidelines.md)
* The recently archived sticky bug task confirmed that current sticky first-bind still carries channel-first architecture and needed explicit semantic-state cleanup.

## Open Questions

* None blocking for this roadmap draft. Remaining uncertainty should be handled in later implementation tasks, not by reopening the semantic target state.
