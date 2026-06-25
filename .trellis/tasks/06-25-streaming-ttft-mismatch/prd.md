# streaming TTFT mismatch investigation

## Goal

Align the request-level streaming TTFT metric and UI with the user's perceived first visible output, or explicitly keep the current first-event semantics and rename it. The current implementation appears to record the first streamed LLM event, which can precede visible text.

## What I already know

* `internal/server/orchestrator/performance.go` marks TTFT on the first `llm.Response` event seen by `recordPerformanceStream.Current()`.
* That first streamed event can be role-only, reasoning-only, tool-call-only, or otherwise not user-visible.
* `internal/server/orchestrator/request.go`, `inbound.go`, and `outbound.go` persist `metricsFirstTokenLatencyMs` from `biz.PerformanceRecord`.
* The requests table displays `TTFT: ...` from `frontend/src/features/requests/components/requests-columns.tsx`.
* Responses API and Gemini streams can emit metadata / empty / non-visible frames before the first visible text chunk.

## Assumptions (temporary)

* The user's complaint is about perceived latency, not transport latency.
* If we change TTFT semantics, the UI label and downstream metrics should stay consistent with the new definition.
* If we need to preserve the old transport-oriented number, it should become a separate explicit metric instead of being hidden under TTFT.

## Open Questions

* Should TTFT mean "first user-visible output token/chunk" or "first streamed event"?

## Requirements (evolving)

* Identify which streamed chunks count as user-visible output across the supported protocol transformers.
* Exclude role-only, reasoning-only, tool-call-only, empty, and terminal marker frames from TTFT if we choose visible-output semantics.
* Keep request total latency unchanged.
* Keep request/execution persistence and charts consistent with the chosen metric definition.
* Update any display labels or helper text if the metric meaning changes.

## Acceptance Criteria (evolving)

* [ ] For a stream whose first few frames are metadata/tool/reasoning frames and whose later frame contains visible text, TTFT matches the first visible text frame.
* [ ] For a stream with no visible text at all, the behavior is explicit and documented.
* [ ] The requests table and request detail views show the same TTFT meaning as the backend metric.
* [ ] Regression tests cover at least one stream that emits non-visible frames before visible content.

## Definition of Done (team quality bar)

* Tests added/updated (unit/integration where appropriate)
* Lint / typecheck / CI green
* Docs/notes updated if behavior changes
* Rollout/rollback considered if risky

## Out of Scope (explicit)

* Changing total request latency semantics.
* Reworking unrelated routing, sticky-session, or quota behavior.
* Adding a silent fallback TTFT mode.

## Technical Notes

* `internal/server/orchestrator/performance.go`
* `internal/server/orchestrator/request.go`
* `internal/server/orchestrator/inbound.go`
* `internal/server/orchestrator/outbound.go`
* `internal/server/biz/channel_metrics.go`
* `frontend/src/features/requests/components/requests-columns.tsx`
* `llm/transformer/openai/responses/outbound_stream.go`
* `llm/transformer/gemini/outbound_stream.go`
