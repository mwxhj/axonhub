# Upstream Beta4 Safe Patch Port

## Goal

Port low-risk fixes from upstream `v1.0.0-beta4` into the local fork without
changing the local product model for routing, credentials, quota, retry, or
fallback. This task treats upstream as a patch source, not as a target state.

## What I Already Know

* Local branch: `unstable`.
* Local head at audit time: `c9e778d refactor(routing): route requests through api key tiers`.
* Upstream tag: `v1.0.0-beta4 = 7122f329 opt: add busy timeout for sqlite dsn, close #1768 (#1880)`.
* Merge base between local head and `v1.0.0-beta4`: `88d2a59d9b84e81ba33ef1368f05f27e20cbe904`.
* `HEAD...v1.0.0-beta4` is not a normal fast-forward sync: local has 51 unique commits and upstream has 73 unique commits.
* A dry merge-tree reports conflicts across provider quota, channel key management, system retry settings, route/sticky/orchestrator, request UI, Ent, GraphQL, and backup/restore.
* Local routing ownership is API key route tiers, not global channel priority or load-balancer strategy.
* Local credential ownership is `UpstreamCredential + ChannelCredentialRef`, not `Channel.credentials`.
* Local quota product concept is credential/key-local quota. Provider quota must not become product state or runtime route truth.
* The frontend must not expose fingerprints, key hints, or backend identity strings as normal operator UI.
* The complete evaluation now covers every commit in `HEAD..v1.0.0-beta4`.
  Each commit is explicitly classified as selected, partially selected,
  rejected, deferred/conditional, or already covered by another selected patch.

## Assumptions

* Direct merge/rebase of `v1.0.0-beta4` is out of scope.
* Upstream commits may be used as references, but implementation should prefer manual porting or very small isolated cherry-picks.
* Generated Ent/GraphQL files are regenerated from local schema after adapting changes; upstream generated output is not accepted blindly.
* Lint/build commands are not run unless explicitly requested by the user.

## Requirements

* Create a complete safe-patch port plan before modifying code.
* Do not merge, rebase, or adopt upstream as the target tree.
* Port only changes that do not alter route ownership, credential ownership, quota semantics, or retry/fallback semantics.
* Treat this as one complete safe-patch implementation slice, not a pilot that only ports the easiest protocol tests.
* Do not satisfy upstream tests by weakening or rewriting tests around missing behavior. Tests must prove the real behavior was ported or intentionally rejected.
* Preserve local Trellis, `.agents`, `.codex`, AGENTS.md, and project rules.
* Keep provider quota fully out of runtime candidate filtering, scoring, UI, schema, and persisted domain model.
* Keep channel raw API key management out of product UI and runtime write paths.
* Keep API key route tiers as the only runtime route ordering source.
* Keep `Channel.ordering_weight` as display ordering only.
* Keep sticky-session as session preservation inside existing route-tier candidates; do not restore sticky first-bind load balancing.
* Keep local credential quota and request UI HCI masking rules.
* Audit every selected upstream patch for hidden coupling to provider quota, channel-owned credentials, global load-balancer settings, or system retry settings.
* Record every evaluated upstream commit as selected, partially selected, rejected, deferred, already satisfied, or out of scope.
* Record any intentionally skipped upstream commit with the reason.
* Treat safe security/permission/account correctness fixes as part of this
  one-shot slice when they do not conflict with local routing or credential
  ownership.
* Hand-port partial UI fixes that still apply to local credential/ref flows;
  do not restore upstream channel inline credential assumptions to make those
  patches apply cleanly.

## One-Shot Implementation Scope

This task should port the full selected safe-patch set in one implementation
slice. It should not stop after only `A1` or after only the tests that are easy
to make green.

Selected work is grouped below only to make review manageable. The deliverable
is one complete safe-patch port, not separate MVP phases.

### Bucket A: LLM Protocol / Transformer Fixes

These affect protocol translation and provider compatibility rather than route
selection. They are selected for this task.

Selected upstream commits:

* `54ffbdf7` fix Anthropic server-side tool blocks without panicking.
* `1bc963a6` tolerate empty Claude Code tool arguments and add shared read helper.
* `3e8f4d5c` drop invalid signature handling for unknown OpenAI/Anthropic payloads.
* `7b10a3b3` update Claude Code headers.
* `18aeef7c` update Claude Code OAuth token URL, user-agent, and beta header.
* Partial `e5c22810`: take only LLM Claude Code header constants if still needed; do not port provider quota checker changes.
* `28adad0a` fix OpenAI stream aggregation nil panic for non-zero choice index.
* `c9ee64d5` from `upstream/unstable`, if still applicable, fix OpenAI stream aggregation nil panics for non-zero tool-call indexes and nil usage.
* `c3bb25b3` preserve Responses function call namespace.
* `ad096f57` add OpenAI audio APIs, including TTS/STT and SSE streaming support.
* `7891a8d8` fix TTS streaming accept/storage/memory behavior.
* `68672716` add Codex image generation/edit support.
* `12e6082d` fix Codex image generation behavior.
* `d9fb1555` fix image generation bypass behavior.
* `31d43681` add Cerebras transformer.
* `94394eb0` fix OpenAI video schema.
* `d1d9779b` refine OpenAI Responses schema handling.
* `b6826c10` adjust image input size limit.
* `da81d220` from `upstream/unstable`, if still applicable, build image data URLs through the shared URL helper.
* `23ae5f0d` fix auto reasoning effort for Qwen `*-max` model names.
* `15b48fa1` avoid duplicate `/v1` for Anthropic-like model fetcher endpoints.
* `8066aa4e` default model type when model type is missing.
* `c0899a64` add `modalities` in `/v1/models` response.

Porting constraints:

* Do not import upstream route selection, retry timeout, provider quota, or channel key management changes while porting protocol fixes.
* For API endpoint additions, adapt them to local request execution and credential target semantics.
* For frontend request detail/cURL updates, apply local HCI masking rules.
* For `ad096f57` / `7891a8d8`, implement the runtime behavior and persistence path; do not only copy audio tests.
* For `31d43681`, verify whether local Cerebras currently aliases through OpenRouter; if so, replace only the transformer behavior that must differ from OpenRouter and keep credential routing unchanged.

### Bucket B: Infrastructure / Runtime Stability Fixes

These are selected after local review, with local schema/config adaptation.

* `23b062cf` support disabling DB auto migration.
* `7122f329` add SQLite busy timeout for SQLite DSN.
* `4a6ade51` support Redis multi mode.
* `cd1157ce` remove unbounded in-memory body cache to avoid OOM.
* `f41f6de8` add IP blocklist settings and middleware.
* `cd6ef40f` add the request-log IP ban icon visibility toggle after blocklist.
* Partial `4562ac2b`: port GC batch deletion and GraphQL forbidden/unauthenticated error handling if still applicable; do not port anything that changes retry semantics.
* `be5523c6` split backup/restore usage logs, after review for local schema drift.
* `ac640758` make old channel token-provider cleanup asynchronous and review
  token auto-refresh cleanup under the local OAuth provider implementation.
* `28ecfc8b` initialize channel performance data asynchronously on startup.
* `621ee5fe` hide prefixed/auto-trimmed mapped model aliases when
  `HideMappedModels` is enabled.
* Partial `370e7313`: add missing translations only for channel variants that
  already exist locally.
* `6add8dfd` align development docs with the Go version required by local
  `go.mod` if the docs are stale.

Porting constraints:

* Configuration defaults must not silently change production behavior without explicit docs.
* Backup/restore changes must be reconciled with local credential and quota schema.
* Middleware must not bypass existing auth, scope, or request logging semantics.
* `disable_auto_migration` must skip both Ent schema migration and data migrations, matching upstream behavior, but the default remains false.
* SQLite busy timeout must preserve existing user-specified `busy_timeout` and existing WAL behavior.

### Bucket C: Request Observability / Export Improvements

These are selected if they can be adapted without exposing backend identity or
secret material.

* `98c15184` record request URL and pass-through flag.
* `d9fb1555` cURL generator changes related to image bypass, if not already covered by Bucket A.
* `72f121b6` from `upstream/unstable`, if `passThrough` becomes a table column, make it hideable/toggleable correctly.
* `63c41116` low cache hit-rate highlight, if it can be kept as operator-facing usage signal without adding backend identity leakage.
* `b3810e4c` from `upstream/unstable`, if still applicable, fix channel health statistics so a failed original execution counts toward that original channel even when later fallback succeeds.
* Request detail and request body/cURL export UI improvements that do not expose internal identities.
* `c6ee88fd` request body drawer performance/animation changes if local request
  detail rendering still blocks drawer open.
* `fcfc8ea9` preserve request filters and pagination when navigating to/from
  request detail pages.

Porting constraints:

* No secret, fingerprint, key hint, `resourceScopeKey`, or credential source string may appear in normal UI or export.
* Request records may store operational facts, but table/detail display must remain operator-facing.
* Do not restore upstream request UI fields that conflict with local HCI rules.

### Bucket D: Security / Permission / Account Correctness

These are selected because they fix real permission, scope, or account behavior
without changing runtime route selection or credential ownership.

Selected upstream commits:

* `974b28f5` add a localized generic resource fallback for GraphQL NOT_FOUND
  errors without a resource extension.
* `afaf90f9` avoid requesting execution channel fields unless the viewer can
  view channels.
* `78d72aa9` add an own-profile update path so users can update language and
  allowed self fields without requiring broader user-management permission.
* `a88c652a` require channel write scope for model fetching and prevent stored
  credentials from being reused against changed channel type/base URL inputs.
* `13f9525e` avoid OIDC pre-auth privacy-deny errors by reading the user's own
  OIDC links under an explicit system bypass.
* `a6267fb7` fix the API key creator filter scope typo.
* `5a305f27` hide API key creator column/filter unless the viewer has system
  user-read permission.
* `defb349d` fix API key token usage aggregation for project-level scopes and
  cross-project API key IDs.

Porting constraints:

* `a88c652a` must adapt to local `CredentialViews()` and must not restore
  `Channel.credentials` as the source of truth.
* Scope fixes must preserve project isolation and avoid broad system bypasses
  except where the upstream bug is specifically a pre-auth self-check.

### Bucket E: Partial Channel UI Compatibility Fixes

These are selected only where they still apply after the local credential
migration.

* Partial `eb8fd574`: preserve the useful Codex protocol/API-format switching
  behavior without restoring channel inline auth JSON or raw key UI.
* Partial `7ad8f17a`: keep third-party Codex base URL editable and prevent
  protocol switching from silently overwriting custom endpoints.

Porting constraints:

* Do not reintroduce channel-owned credential entry, raw API key side panels,
  or auth JSON detection as product logic.
* Any UI changes must be checked against the local HCI forbidden-field list.

## Explicitly Out of Scope

These are not part of this safe-patch task even if they exist in `v1.0.0-beta4`:

* Provider quota service, provider quota schema, provider quota UI, quota-aware selector, or quota-aware load-balancer strategy.
* Channel-owned raw API key management, disabled API keys, channel test API keys, or channel API key deletion UI.
* Runtime reads or writes that make `Channel.credentials` the product owner of API keys or OAuth secrets again.
* Global load-balancer strategy restoration, adaptive/failover/circuit-breaker strategy as primary route chooser, random tie-breaks, or channel weight runtime ordering.
* Response timeout retry as implemented upstream.
* Channel retryable status/error pattern rules.
* Media condition routing.
* Model circuit breaker changes.
* Strict local channel RPM admission changes.
* Channel test-all-API-keys behavior.
* API key quota usage OpenAPI query.
* Channel duplication price-copy behavior.
* Request override `array_remove`.
* New built-in provider/channel defaults, default model catalog sync, sponsor
  docs, dependency upgrades, dashboard visual-only polish, and unrelated
  channel dialog polish.
* Any generated Ent/GraphQL replacement that removes local `UpstreamCredential`, `ChannelCredentialRef`, `CredentialQuotaScope`, API key route tiers, or local HCI fields.

The out-of-scope items may become separate design-first tasks later.

## Acceptance Criteria

* [ ] A complete evaluation inventory lists selected upstream commits, skipped commits, and reasons.
* [ ] Every `HEAD..v1.0.0-beta4` commit appears in the evaluation inventory with a disposition.
* [ ] No direct merge/rebase of `v1.0.0-beta4` is performed.
* [ ] Ported changes do not reintroduce provider quota product/runtime state.
* [ ] Ported changes do not reintroduce channel-owned key management UI or write paths.
* [ ] Ported changes do not alter API key route-tier runtime ordering semantics.
* [ ] Ported request UI/export changes respect local HCI masking rules.
* [ ] Security/permission fixes preserve local project isolation and do not
  broaden access by using generic bypasses.
* [ ] Model fetching cannot reuse stored credentials for a changed channel type
  or base URL.
* [ ] Partial Codex channel UI fixes do not restore channel inline auth JSON or
  raw API key management.
* [ ] Any schema/API additions are regenerated or adapted from local schema, not blindly copied from upstream generated output.
* [ ] Tests verify the selected behavior; tests are not edited merely to match an incomplete implementation.
* [ ] Selected post-`v1.0.0-beta4` fixes are limited to clearly safe follow-ups documented in the evaluation inventory.
* [ ] Verification is scoped to touched areas and follows repository command constraints.

## Definition of Done

* Complete evaluation inventory completed before code changes.
* Code changes are grouped by safe patch bucket.
* Tests added/updated where behavior changes.
* Lint/build are not run unless explicitly requested by the user.
* Specs updated if a safe patch introduces durable local conventions.
* Commit plan separates work commits from Trellis/archive/journal commits.

## Technical Notes

* Dry merge conflict command used during discussion:
  `git merge-tree --write-tree --name-only HEAD v1.0.0-beta4`.
* The conflict list includes provider quota, channel key management, system retry,
  orchestrator, request UI, Ent/GraphQL, and backup/restore.
* Full beta4 commit disposition lives in
  `research/complete-safe-patch-evaluation.md`; implementation must check that
  matrix before taking each upstream patch.
* Existing local specs that govern this task:
  `.trellis/spec/backend/routing-guidelines.md`,
  `.trellis/spec/backend/credential-routing-model.md`,
  `.trellis/spec/frontend/human-computer-interaction.md`.
* Current route-template task remains separate:
  `.trellis/tasks/06-22-local-key-route-templates-cleanup/`.

## Research References

* [`research/safe-patch-inventory.md`](research/safe-patch-inventory.md) — initial audit of safe, risky, and rejected upstream areas.
* [`research/complete-safe-patch-evaluation.md`](research/complete-safe-patch-evaluation.md) — complete commit-level evaluation used as the execution scope.
