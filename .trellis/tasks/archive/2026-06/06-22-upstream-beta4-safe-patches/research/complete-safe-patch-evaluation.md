# Complete Safe Patch Evaluation

## Purpose

This file is the execution gate for the safe upstream patch port. It expands
the initial inventory into a commit-level evaluation so implementation does not
cherry-pick only easy tests, and also does not accidentally turn this task into
a full upstream merge.

## Evaluation Rules

* Upstream is a patch source, not the target tree.
* No direct merge or rebase.
* A selected patch must preserve local API key route tiers, local credential
  ownership, local credential/key quota semantics, and frontend HCI masking.
* Tests must verify real behavior. Do not weaken tests to hide incomplete ports.
* Generated Ent/GraphQL code is accepted only after local schema/resolver changes
  have been adapted and generation is appropriate for the local tree.
* Post-`v1.0.0-beta4` commits may be selected only when they are clear safe
  follow-up fixes to already-selected work.
* Every `HEAD..v1.0.0-beta4` commit must appear in the full disposition matrix
  below as selected, partially selected, rejected, deferred, or already
  satisfied. Missing from the matrix means the evaluation is incomplete.

## Selected: LLM Protocol / Transformer / API Compatibility

| Commit | Decision | Local Port Shape | Must Verify |
| --- | --- | --- | --- |
| `54ffbdf7` Anthropic server-side tool blocks | Select | Port Anthropic block model/conversion/stream aggregation support and request parser behavior. | Server tool use round-trip, stream round-trip, no panic on server-side tool blocks. |
| `1bc963a6` empty tolerance for Claude Code read | Select | Port helper that tolerates empty tool args and shared stream parsing behavior. | Empty tool arguments do not crash or corrupt Anthropic/Claude Code conversion. |
| `3e8f4d5c` drop invalid signature for unknown OpenAI/Anthropic | Select | Port shared signature handling for unknown payloads without weakening known-provider validation. | Unknown signatures are dropped; known structured payloads still round-trip. |
| `7b10a3b3` Claude Code headers | Select | Update Claude Code headers only. | Header constants and outbound request headers match expected behavior. |
| `18aeef7c` Claude Code OAuth update | Select | Update OAuth token URL, UA, and beta header. | OAuth flow uses the new token endpoint and still stores OAuth as credential secret, not channel inline credential. |
| Partial `e5c22810` Claude Code quota check header | Partially select | Take only reusable Claude Code header constant if useful. Do not port provider quota checker code. | No provider quota code/schema/UI returns. |
| `28adad0a` OpenAI non-zero choice index | Select | Port aggregation by actual choice index instead of assuming dense zero-based indexes. | Non-zero choice index stream aggregates without panic. |
| `c9ee64d5` OpenAI aggregation nil panics, post-beta4 | Select as safe follow-up | Port non-zero tool-call index handling and nil usage guard. | Non-zero tool-call index and missing usage do not panic. |
| `c3bb25b3` Responses function call namespace | Select | Preserve namespace and call identity across Responses inbound/outbound/stream conversions. | Function-call and function-call-output round-trip keeps namespace/call IDs. |
| `d1d9779b` OpenAI Responses schema refinements | Select | Port schema event field adjustments that match selected Responses changes. | Existing Responses tests plus new namespace/schema cases pass. |
| `68672716` Codex image generation/edit | Select | Port Codex and Responses image request/response conversion. | Codex image gen/edit outbound and Responses image aggregation tests cover real conversion. |
| `12e6082d` Codex image generation fix | Select | Port follow-up fixes on top of image support. | Image stream and response model tests cover the corrected behavior. |
| `d9fb1555` image generation bypass | Select | Port pass-through/image bypass fix and cURL generation changes only under local HCI masking. | Pass-through image requests keep correct body/cURL without secret leakage. |
| `ad096f57` OpenAI audio APIs | Select | Add audio model/API types, inbound/outbound transformers, OpenAI routes, request persistence, and request detail/cURL support adapted to local execution model. | TTS/STT request, streaming, persistence, and cURL behavior work. |
| `7891a8d8` TTS streaming/storage/OOM follow-up | Select | Port streaming accept, external storage, decoder, and memory behavior fixes. | Streaming TTS persists without unbounded memory and respects Accept handling. |
| `94394eb0` OpenAI video schema | Select | Port video schema fixes for OpenAI/Doubao video transforms. | Video inbound/outbound schema tests cover changed fields. |
| `31d43681` Cerebras transformer | Select with adaptation | Add Cerebras transformer behavior. If local Cerebras currently aliases OpenRouter, replace only what must differ while preserving local credential routing. | Cerebras outbound request uses correct transformer and request-scoped credential selection. |
| `b6826c10` image input size limit | Select | Port size-limit behavior and tests. | Oversized image input is rejected according to intended limit. |
| `da81d220` BuildDataURL helper, post-beta4 | Select as safe follow-up if helper exists or is ported | Use shared data URL builder for large image/audio/video data URL construction. | Data URL output is identical and avoids ad hoc formatting. |
| `23ae5f0d` Qwen max auto reasoning effort | Select | Prevent `qwen-*-max` from being parsed as `reasoning_effort=max`. | Qwen max model stays intact; supported effort suffixes still work. |
| `15b48fa1` duplicate `/v1` model fetcher | Select | Port Anthropic-like endpoint URL normalization. | Model fetcher does not generate duplicate `/v1`. |
| `8066aa4e` missing model type default | Select | Port default model type behavior if local model validation still lacks it. | Missing model type gets the expected default without loosening validation. |
| `c0899a64` `/v1/models` modalities | Select | Add modalities to OpenAI-compatible model response if compatible with local model objects. | `/v1/models` response includes modalities without breaking clients. |

## Selected: Infrastructure / Runtime Stability

| Commit | Decision | Local Port Shape | Must Verify |
| --- | --- | --- | --- |
| `23b062cf` disable DB auto migration | Select | Add config field/default and skip both schema migration and data migration when disabled. | Default remains migration enabled; disabled mode skips migrations. |
| `7122f329` SQLite busy timeout | Select | Replace `ensureSQLiteWAL` with local `ensureSQLiteDSN` behavior preserving existing WAL and user busy timeout. | DSN helper covers WAL disabled, existing WAL, existing busy timeout, non-SQLite. |
| `4a6ade51` Redis multi mode | Select | Port Redis cluster/ring/sentinel or multi mode as upstream designed, adapted to current cache/watchers. | Single Redis still works; multi mode tests cover config and client creation. |
| `cd1157ce` data storage OOM | Select | Remove unbounded `afero.NewCacheOnReadFs(...NewMemMapFs...)` wrappers for S3/GCS. | Remote storage uses base filesystem directly and comments explain why. |
| `f41f6de8` IP blocklist | Select | Add system setting, middleware, UI, GraphQL resolver, and request list display if aligned with current auth/scope model. | Blocked IP is rejected; settings persist; request UI display remains operator-facing. |
| Partial `4562ac2b` GC and GraphQL errors | Partially select | Port GC batch delete and GraphQL forbidden/unauthenticated error status handling. Do not port retry semantic changes. | GC deletes in bounded batches; GraphQL auth/permission errors map correctly. |
| `be5523c6` backup/restore usage log split | Select with schema reconciliation | Port backup option and restore logic, adapted to local credential/quota/request schema. | Backup/restore handles usage logs separately without losing local credential/quota data. |

## Selected: Request Observability / Export

| Commit | Decision | Local Port Shape | Must Verify |
| --- | --- | --- | --- |
| `98c15184` request URL and pass-through flag | Select | Add request execution fields/resolvers/UI only after local schema adaptation. | URL/pass-through persist and display without exposing secret/fingerprint/key hint. |
| `c6ee88fd` request body drawer open animation | Select with HCI review | Delay heavy request detail fetch/render until the drawer opens, if local drawer still mounts large body content during animation. | Drawer still shows correct request details and does not expose forbidden identity fields. |
| `fcfc8ea9` preserve request filters during detail navigation | Select | Preserve current request list filters/search when navigating to and from request detail pages. | Returning from request detail preserves filters, pagination, and canceled status filter. |
| `72f121b6` passThrough column hideable, post-beta4 | Select only if `passThrough` column is ported | Add accessor so column visibility works. Ignore unrelated `.gitignore` additions unless already desired. | Column can be toggled. |
| `63c41116` low cache-hit highlight | Select if still meaningful with local request UI | Port operator-facing cache-hit warning only. | No backend identity leaks; threshold display is clear. |
| `b3810e4c` failed original channel stats, post-beta4 | Select if channel health stats code still matches | Count failed original execution toward that channel even if fallback later succeeds. | Failed original channel contributes to its health stats; success fallback contributes to success channel. |

## Selected: Security / Permission / Account Correctness

These are selected because they close visible permission, auth, or cross-scope
bugs without changing routing ownership or credential ownership.

| Commit | Decision | Local Port Shape | Must Verify |
| --- | --- | --- | --- |
| `974b28f5` NOT_FOUND resource fallback | Select | Add localized generic resource fallback so GraphQL NOT_FOUND errors without `resource` do not leak interpolation placeholders. | Toast/error text is readable in English and Chinese. |
| `afaf90f9` request execution channel permission guard | Select | Request execution channel fields only when the viewer can view channels. | Request-log-only users do not trigger forbidden execution-channel GraphQL fields. |
| `78d72aa9` update own profile/language | Select | Add an own-profile update path that allows a user to update allowed self fields without requiring broader user-management permission. | `updateMe` and password update still mutate only the current user and invalidate cache. |
| `a88c652a` model fetcher scope and stored credential boundary | Select with local credential adaptation | Require channel write scope for `fetchModels`; when `ChannelID` is provided without an explicit key, reuse stored credential views only if input channel type and base URL match the stored channel. Do not read `Channel.credentials` as new product state. | Stored credential is not sent to a changed/attacker endpoint; existing local `CredentialViews()` still works. |
| `13f9525e` OIDC pre-auth privacy bypass | Select | Use system bypass only for the pre-auth user's own OIDC restriction check. | Sign-in no longer logs privacy-deny errors; OIDC-only restriction behavior is unchanged. |
| `a6267fb7` API key creator filter scope typo | Select | Correct frontend scope check from `read_apikeys` to `read_api_keys`. | Creator filter fetches users only for principals with the real scope. |
| `5a305f27` API key creator column permission | Select | Hide creator column and creator filter unless the viewer has system user-read permission. | Project/API-key users without system user access do not see creator user data. |
| `defb349d` API key token usage project scope | Select | Teach authz scope checks about project membership/roles and validate accessible API keys before usage aggregation. | Project-level readers can see their own key usage but not cross-project key usage. |

## Selected: Runtime / Cache / Model Correctness

These are selected because they fix correctness or startup behavior while staying
outside global load-balancing, retry, provider quota, and channel-owned keys.

| Commit | Decision | Local Port Shape | Must Verify |
| --- | --- | --- | --- |
| `ac640758` channel cache/token refresh cleanup | Select with dependency review | Make old channel token-provider cleanup asynchronous and port token auto-refresh loop simplification if local OAuth provider still has the executor-based bug. Avoid unrelated dependency churn except what the code removal requires. | Channel cache swap does not block new channel availability; token auto-refresh stops cleanly. |
| `28ecfc8b` async channel performance initialization | Select | Move channel performance initialization out of `NewChannelService` construction into async startup with panic logging. | App startup does not block on performance initialization; scheduled tasks still register. |
| `621ee5fe` hide prefixed mapped models | Select | When `HideMappedModels` is enabled, hide all non-mapping entries that resolve to the mapped target model, including prefixed and auto-trimmed variants. | Direct, prefix, and auto-trim model entries are hidden consistently while mapping entries remain. |
| `370e7313` missing anthropic variant translations | Partially select | Add only missing translations for channel variants that already exist locally. Do not introduce new channel types solely for translation parity. | Existing local channel variant labels resolve in both locales. |
| `6add8dfd` Go version docs | Select if local docs are stale | Align development docs with the local `go.mod` floor. | Docs do not tell developers to use an older unsupported Go version. |
| `cd6ef40f` IP ban icon visibility toggle | Select as follow-up to `f41f6de8` | Add a setting to hide/show the request-log IP ban quick action after the IP blocklist feature is ported. | Default preserves current quick action; disabling setting removes the icon without disabling blocklist enforcement. |
| `eb8fd574` Codex protocol switching | Partially select | Port only the still-applicable API-format/protocol switching behavior. Do not restore channel inline auth JSON, raw key panels, or channel-owned credentials. | Codex protocol can be switched where product rules allow; local credential/ref model remains intact. |
| `7ad8f17a` third-party Codex base URL edit | Partially select | Keep third-party Codex base URL editable and avoid overwriting custom endpoints on protocol switch. Adapt to local credential UI. | Third-party Codex channel base URL is editable and not silently reset. |

## Rejected In This Task

| Commit / Area | Reason |
| --- | --- |
| `e5be6212`, `98c7855a`, `f111e478` provider quota work | Provider quota becomes durable domain state and route/filter/score input. This violates local credential/quota contracts. |
| `21284f2e`, `e260e0b4` channel retryable status/error patterns | Useful but design-first. It changes retry/fallback classification and channel settings semantics. |
| `0c749008` response timeout retry | Conflicts with project rule against hidden time-budget fallback. |
| `960690ae` media condition routing | Useful but design-first. Must be expressed as feasibility filtering under API key route tiers, not upstream route semantics. |
| `77b94e09` strict local channel RPM admission | Useful but design-first. Must be an admission gate under route tiers, not load-balancer scoring. |
| `b5ab115d` model circuit breaker | Useful but design-first. Must remain explicit hard state, not a primary strategy. |
| `151317de` test all API keys | Reintroduces channel-owned raw key testing/management. Must be redesigned around `UpstreamCredential`. |
| `90ec4653` API key quota usage OpenAPI | Separate API design task; must ensure it stays local API key quota and not provider quota. |
| `12389ba6` duplicate channel copies model prices | Separate product feature. Not a safe patch dependency. |
| `c2403700` request override array remove | Separate product feature. Not a safe patch dependency. |
| `70417ccd`, `d1c2aca8`, `8566eaa0`, `980ce5ad` model developer/provider data sync | Data/catalog sync is product data drift, not a safe behavior patch. |
| `76a001fa`, `4ae2e5da`, `33ad6fe9`, `d160cf53` built-in provider/channel/default model changes | Provider/channel additions and default model catalogs are product features and schema/default-data changes, not safe patch dependencies. |
| `cd7bd2ac`, `4c7f010f` dependency upgrades | Separate dependency-update task; `4c7f010f` also deletes a local OAuth test upstream no longer has. |
| `2cc3b60d` channel action dialog minor UI | Mostly polishes retry fields and legacy key side panels that are not part of the local product surface. |
| `c0f2d0d1` dashboard success-rate card styling | Unrelated dashboard polish; not a behavior or safety patch. |
| `d59d5949` sponsor assets/readme changes | Sponsor/docs-only content is not part of safe patching. |
| `2f6d0678` post-beta4 OpenAPI query by name | New OpenAPI feature. Not a follow-up fix to any selected beta4 behavior. |

## Full Beta4 Commit Disposition Matrix

This matrix is the audit checklist for all 73 commits in
`HEAD..v1.0.0-beta4`.

| Commit | Disposition | Notes |
| --- | --- | --- |
| `70417ccd` | Reject | Model developer data sync; product data drift. |
| `54ffbdf7` | Select | Anthropic server-side tool blocks. |
| `c6ee88fd` | Select | Request detail drawer performance/HCI, with local masking review. |
| `8066aa4e` | Select | Missing model type default. |
| `23b062cf` | Select | Disable DB auto migration. |
| `974b28f5` | Select | Generic NOT_FOUND resource fallback. |
| `23ae5f0d` | Select | Qwen max auto reasoning effort guard. |
| `b6826c10` | Select | Image input size limit. |
| `c0899a64` | Select | `/v1/models` modalities. |
| `f41f6de8` | Select | IP blocklist settings and middleware. |
| `afaf90f9` | Select | Request execution channel permission guard. |
| `be5523c6` | Select with adaptation | Backup/restore usage-log split under local schema. |
| `7b10a3b3` | Select | Claude Code headers. |
| `78d72aa9` | Select | Own-profile/language update. |
| `ac640758` | Select with adaptation | Channel cache/token refresh cleanup. |
| `cd1157ce` | Select | Data storage OOM fix. |
| `e5c22810` | Partial | Header constant only; no provider quota checker. |
| `90ec4653` | Reject | New OpenAPI quota query; separate API design. |
| `28adad0a` | Select | OpenAI non-zero choice index aggregation. |
| `4c7f010f` | Reject | Dependency upgrade and local OAuth test deletion. |
| `ad096f57` | Select | OpenAI audio APIs. |
| `d1c2aca8` | Reject | Model developer data sync. |
| `7891a8d8` | Select | TTS streaming/storage/OOM follow-up. |
| `4ae2e5da` | Reject | New channel type/default data; separate product feature. |
| `4a6ade51` | Select | Redis multi mode. |
| `98c7855a` | Reject | Provider quota status schema semantics. |
| `c3bb25b3` | Select | Responses function call namespace. |
| `0c749008` | Reject | Response timeout retry. |
| `76a001fa` | Reject | Built-in provider/channel addition. |
| `e5be6212` | Reject | Provider quota monitoring and routing coupling. |
| `21284f2e` | Reject | Channel retryable status code rules. |
| `960690ae` | Reject | Media condition routing. |
| `a88c652a` | Select with adaptation | Model fetcher scope and stored credential boundary. |
| `fcfc8ea9` | Select | Preserve request filters on detail navigation. |
| `1bc963a6` | Select | Empty Claude Code tool argument tolerance. |
| `12389ba6` | Reject | Duplicate channel price-copy feature. |
| `4562ac2b` | Partial | GC batch delete and GraphQL status only; no retry semantics. |
| `370e7313` | Partial | Existing local translation keys only. |
| `18aeef7c` | Select | Claude Code OAuth endpoint/header update. |
| `eb8fd574` | Partial | Codex protocol switch UI only under local credential model. |
| `e260e0b4` | Reject | Channel retryable error pattern rules. |
| `8566eaa0` | Reject | Model developer data sync. |
| `33ad6fe9` | Reject | Channel default model update. |
| `68672716` | Select | Codex image generation/edit. |
| `2cc3b60d` | Reject | Minor channel dialog polish tied to rejected surfaces. |
| `6add8dfd` | Select if stale | Go version docs alignment. |
| `d1d9779b` | Select | OpenAI Responses schema refinements. |
| `d59d5949` | Reject | Sponsor/readme content. |
| `cd6ef40f` | Select | IP ban icon visibility toggle after blocklist. |
| `d160cf53` | Reject | Default model catalog update. |
| `13f9525e` | Select | OIDC pre-auth privacy bypass. |
| `b5ab115d` | Reject | Model circuit breaker design-first. |
| `15b48fa1` | Select | Avoid duplicate `/v1` model fetcher URL. |
| `c2403700` | Reject | Request override `array_remove` feature. |
| `f111e478` | Reject | Provider quota reset action. |
| `a6267fb7` | Select | API key creator filter scope typo. |
| `7ad8f17a` | Partial | Third-party Codex base URL edit behavior only. |
| `77b94e09` | Reject | Strict local channel RPM admission design-first. |
| `94394eb0` | Select | OpenAI video schema. |
| `5a305f27` | Select | Hide API key creator column/filter without permission. |
| `151317de` | Reject | Test all channel-owned API keys. |
| `28ecfc8b` | Select | Async channel performance initialization. |
| `c0f2d0d1` | Reject | Dashboard visual polish. |
| `3e8f4d5c` | Select | Drop invalid unknown signatures. |
| `defb349d` | Select | Project-level API key token usage scope. |
| `63c41116` | Select | Low cache-hit highlight. |
| `621ee5fe` | Select | Hide prefixed mapped models. |
| `12e6082d` | Select | Codex image generation follow-up. |
| `cd7bd2ac` | Reject | Frontend dependency upgrade. |
| `31d43681` | Select with adaptation | Cerebras transformer under local credential routing. |
| `d9fb1555` | Select | Image generation bypass and masked cURL/pass-through changes. |
| `98c15184` | Select | Request URL and pass-through flag. |
| `7122f329` | Select | SQLite busy timeout DSN helper. |

## Post-Beta4 Follow-Up Disposition

Only safe follow-ups to selected beta4 work are allowed here.

| Commit | Disposition | Notes |
| --- | --- | --- |
| `da81d220` | Select if applicable | Data URL helper for selected image/audio/video transformer work. |
| `980ce5ad` | Reject | Model developer data sync. |
| `72f121b6` | Select if applicable | Pass-through column visibility after `passThrough` is ported. |
| `b3810e4c` | Select if applicable | Original failed channel stats after fallback, if local stats code matches. |
| `c9ee64d5` | Select | OpenAI stream aggregation nil panic follow-up. |
| `2f6d0678` | Reject | New OpenAPI query feature, not a selected follow-up. |

## Already Partially Present Locally

These require careful diffing before implementation because local code has some
related behavior already:

* `internal/server/orchestrator/auto_reasoning_effort.go` already exists but lacks the Qwen max guard from `23ae5f0d`.
* `internal/server/biz/channel_llm.go` already treats Cerebras like OpenRouter; `31d43681` may need a true Cerebras transformer instead of another alias.
* Request UI already has pass-through settings, but request executions do not yet expose request URL / pass-through applied fields.
* Usage logs already have audio token fields, but audio endpoint/transformer files are not present.
* OpenAI image endpoints exist, but Responses image request/response files and Codex image follow-ups are not present.
* `internal/server/biz/model_fetcher.go` already resolves credentials via local
  credential views, so `a88c652a` must be adapted to that boundary instead of
  restoring `Channel.credentials`.
* `frontend/src/features/channels/components/channels-action-dialog.tsx` has
  local credential/ref product changes, so Codex protocol/base URL UI fixes
  must be hand-ported rather than copied.

## Implementation Guardrails

* Use upstream tests as behavior references, not as the implementation itself.
* If a selected patch has upstream tests, port or rewrite equivalent tests that
  exercise the same behavior in the local tree.
* If a selected patch touches request execution schema, adapt from local Ent
  schema first, then regenerate generated files only as required.
* If a selected patch touches channel/provider code, confirm it does not write
  to `Channel.credentials` or bypass `ChannelCredentialRef` / request-scoped
  credential selection.
* If a selected patch touches request UI/export, run a targeted search for
  forbidden UI strings: `secret:v1`, `credentialFingerprint`,
  `secretFingerprint`, `resourceScopeKey`, `credentialSource`, `keyHint`,
  and masked raw key fragments outside explicit rotation/copy flows.
