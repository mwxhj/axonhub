# Sticky quota ratio balancing

## Goal

Add a simple quota-aware first-bind rule for sticky sessions: when a new sticky session has no valid existing binding, prefer a key whose current local quota window has the lowest `usedAmount / limitAmount` ratio inside the current priority tier. Existing sticky bindings remain stable, and normal retry/fallback still handles execution failures.

The intent is not to create a health score, a second quota system, or a new channel quota. The intent is to spread new sticky sessions so local key quota consumption progresses at roughly similar ratios across comparable quota-managed keys.

## What We Already Know

- Sticky sessions exist to preserve upstream cache/context locality.
- Sticky routing must not cross priority tiers only to preserve or balance a target.
- Key and OAuth secrets live on `UpstreamCredential`; channels stay routing-only.
- `CredentialQuotaScope` is the local key/credential quota product surface.
- Local quota can already make a credential view non-executable when paused, disabled, or exhausted with a blocking action.
- Provider quota status is separate and should not be used as provider-specific ratio math in this MVP.
- The user wants a simple model: no health score, no complex balancing formula, no UI-heavy design.

## Product Design

Human-readable rule:

> Sticky keeps old sessions stable. For new sticky sessions, if the normal route lands in the quota-managed pool, use the key whose current local quota window is least consumed.

More specifically:

1. Existing sticky binding wins.
   - If the sticky key is already bound to an eligible target, keep using that target.
   - Do not rebalance active sessions just because another key has a lower usage ratio.

2. Only first binding is quota-balanced.
   - This applies when the request has a sticky key but no valid binding yet.
   - The selected target is written only after upstream success, following the existing sticky contract.

3. Stay inside the current priority tier.
   - Do not route to a lower-priority tier just because its quota ratio is lower.
   - If retry/fallback later reaches a lower tier, existing retry/fallback semantics continue to apply.

4. Reuse existing eligibility.
   - Disabled/archived credentials, disabled refs, locally blocked quotas, and provider-quota-blocked credentials should not be introduced into the balancing pool.
   - This feature does not add a new filtering model; it only reorders candidates that are already executable.

5. Compare local quota ratios only when the data is comparable.
   - Ratio is `usedAmount / limitAmount`.
   - `limitAmount` must be parseable and greater than zero.
   - `usedAmount` must be parseable.
   - The quota scope must represent the current local quota window.
   - Daily windows are the primary MVP target.

## Mixed Quota / No-Quota Behavior

This is the important mixed case:

> No-quota keys keep using the original load-balancer path. Quota-managed keys rebalance only inside the quota-managed pool.

Selection rule:

1. Run existing priority and load-balancer ordering.
2. Look at the normal top choice for this new sticky session.
3. If that top choice has no comparable local quota ratio, keep it.
4. If that top choice has a comparable local quota ratio, treat it as entering the quota-managed pool and replace it with the comparable quota-managed key in the same priority tier with the lowest `usedAmount / limitAmount`.
5. If ratios tie or are close enough to be indistinguishable, fall back to existing weight/order behavior.

Example:

```text
A: local daily quota, used 10 / 100 = 10%
B: local daily quota, used 80 / 200 = 40%
C: no local quota
```

- If the existing load balancer picks `C`, use `C`.
- If the existing load balancer picks `A` or `B`, enter the quota-managed pool and use `A`.

This avoids pretending no-quota keys are `0%` or `100%`. Operators who want a whole priority tier to balance by quota ratio should attach comparable local quota scopes to all keys in that tier.

## Shared Quota Scope Behavior

If multiple credentials share the same `quotaScopeID`, they represent one local quota pool.

MVP behavior:

1. Compare quota scopes first, not individual keys.
2. Pick the scope with the lowest `usedAmount / limitAmount`.
3. Inside the selected scope, choose the concrete credential using the existing weight/order behavior.

Do not pretend keys inside one shared scope have independent ratios.

## Different Quota Units / Windows

Units:

- Ratios may be compared across units only because they are percentages of each key's own local quota.
- The MVP should still prefer clearly local, configured quotas over provider quota data.
- If unit behavior is ambiguous or not automatically updated, the scope should not participate unless the stored `usedAmount` is considered trustworthy by the existing local quota system.

Windows:

- Daily windows are the MVP target because the product goal is "today's usage ratio".
- Daily vs daily is comparable.
- Custom windows can be considered later if their window boundaries are explicit and product copy does not call them "daily".
- Monthly vs daily should not be mixed in the daily balancer MVP.
- Missing or stale window metadata means no participation in ratio balancing.

## Provider Quota Behavior

Provider quota is not used for ratio balancing in this MVP.

Provider quota still matters for eligibility:

- Provider exhausted/unready can make a credential non-executable according to existing routing rules.
- Provider warning may remain visible in UI, but does not become ratio input.

Reason: provider `quota_data` differs by provider, and normalizing it into daily ratios would be a separate product/design task.

## UX / Human Interaction

Keep UI minimal.

Configuration:

- Add this under sticky-session/routing settings, not channel quota settings.
- Copy should say:

```text
Balance new sticky sessions by local key quota usage
New sticky sessions prefer keys with a lower used/limit ratio in the current local quota window.
Existing sticky sessions keep their current key while it remains available.
```

Credential list/detail:

- Show local quota ratio where already showing local quota:

```text
23% used
1,230 / 5,000 tokens
Reset 00:00
```

- For no quota:

```text
No local quota
```

Route/request explanation:

```text
Selected because this new sticky session entered the quota-managed pool and this key had the lowest local quota usage ratio in the current priority tier.
```

For no-quota selections:

```text
Selected by normal load balancing; this key has no comparable local quota ratio.
```

## Requirements

- Apply only to sticky-session routing and only for new/unbound sticky sessions.
- Preserve existing sticky bindings when the bound target is eligible.
- Never write sticky binding before upstream success.
- Never cross priority tiers for quota balancing.
- Reuse existing candidate and credential-view eligibility.
- Use only local `CredentialQuotaScope` ratio inputs for MVP.
- Leave provider quota as eligibility/status, not ratio math.
- Preserve original load-balancer behavior for no-quota choices.
- Rebalance only within comparable local quota-managed pools.
- Treat shared quota scopes as one pool.
- Keep UI copy focused on local key quota and sticky first-bind behavior.

## Acceptance Criteria

- [ ] New sticky session with two comparable local daily quotas chooses the lower `usedAmount / limitAmount` key.
- [ ] Existing sticky binding is not moved to a lower-ratio key while the bound target remains eligible.
- [ ] Balancer does not cross priority tiers.
- [ ] Mixed local-quota and no-quota keys preserve normal no-quota selection when the base load balancer chooses a no-quota key.
- [ ] Mixed local-quota and no-quota keys rebalance within the quota-managed pool when the base load balancer chooses a quota-managed key.
- [ ] Shared `quotaScopeID` credentials are balanced at scope level, then selected inside the scope by existing behavior.
- [ ] Missing, invalid, zero, stale, or non-comparable quota data does not participate in ratio balancing.
- [ ] Provider quota status is not used as ratio input.
- [ ] Request/routing explanation distinguishes normal load balancing, sticky binding reuse, and quota-ratio first binding.

## Definition of Done

- Focused backend tests cover sticky first-bind quota-ratio ordering.
- Existing sticky-session tests continue to pass.
- Existing provider/local quota eligibility tests continue to pass.
- Frontend/settings copy, if implemented, clearly says this affects new sticky sessions only.
- Specs are updated if implementation introduces a new routing contract.

## Implementation Notes

- Implemented backend-only MVP. No new UI switch was added; behavior is active when the retry/load-balancer strategy is `sticky-session`.
- Sticky first-bind now starts from the existing load-balancer order, then enters quota-ratio balancing only when the normal first credential has comparable local daily `CredentialQuotaScope` data.
- Runtime `ChannelCredentialView` now carries local quota scope used/limit/unit/source snapshots needed for routing.
- Ratio balancing compares only local budget daily scopes with valid positive limits, valid used amounts, and future reset time.
- Existing sticky bindings still win and are not rebalanced by quota ratio.
- No-quota credentials stay on normal load-balancer behavior unless the normal first choice is already in the comparable local quota-managed pool.
- Shared quota scopes are deduped by `quotaScopeID` before comparing ratios.
- Provider quota remains an eligibility/status input only and is not used in ratio math.

Verification:

- `go test ./internal/server/orchestrator -run 'TestStickySessionRouter_'`
- `go test ./internal/server/biz -run 'TestTraceStickyKeyProvider|TestCredentialViewsFromRefs'`
- `go test ./internal/server/orchestrator -run 'TestStickySession|TestModelCircuitBreakerMiddleware'`
- `go test ./internal/server/orchestrator`
- `go test ./internal/server/biz`
- `git diff --check`

## Out of Scope

- General health scoring.
- Weighted score formulas or configurable balance factors.
- Rebalancing existing sticky sessions.
- Cross-priority quota optimization.
- Provider-quota ratio normalization.
- Channel-local quota.
- Organization/project/consumer budgeting.
- Full UI dashboard for quota balancing.

## Likely Implementation Areas

- `.trellis/spec/backend/routing-guidelines.md`
- `.trellis/spec/backend/credential-routing-model.md`
- `internal/server/orchestrator/sticky_session.go`
- `internal/server/orchestrator/sticky_session_test.go`
- `internal/server/orchestrator/load_balancer.go`
- `internal/server/biz/channel_credential_identity.go`
- `internal/server/biz/upstream_credential.go`
- Frontend settings and credential quota display files only if a UI switch/explanation is implemented in the same task.

## Open Questions

- Should this be default-off initially, or default-on when sticky-session load balancing is enabled?
- What exact tolerance should count as a ratio tie before falling back to existing order? A conservative default is exact sort first, existing order as deterministic tie-breaker.
