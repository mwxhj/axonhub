# Human-Computer Interaction Rules

> Frontend contract for showing operator-facing information instead of backend implementation state.

---

## 1. Scope / Trigger

Read this spec before changing:

- Data tables, detail pages, tooltips, popovers, drawers, and dialogs.
- Credential, channel, request, quota, routing availability, and system settings UI.
- GraphQL queries whose only purpose is to feed visible frontend fields.

The durable rule is:

```text
The frontend is not a database browser.
Every field shown in product UI must help the operator understand state,
make a decision, or take an action.
```

---

## 2. Signatures

Fields allowed in normal operator UI when relevant:

```text
request.id
request.status
request.error_message
request.model_id
request.actual_model_id
request.format
request.stream
request.source
request.ip
request.project.name
request.channel.name
request.execution.credential_name_snapshot
request.execution.status
request.execution.error_message
request.usage tokens / cached tokens / cost / latency / TTFT
```

Credential fields allowed in normal operator UI when relevant:

```text
credential.name
credential.provider_type
credential.base_url
credential.status
credential.quota_scope.used_amount
credential.quota_scope.limit_amount
credential.quota_scope.unit
credential.quota_scope.reset_policy
credential.quota_scope.reset_at
credential.quota_scope.window_started_at
credential.channel_ref_count
credential.updated_at
```

Fields forbidden in normal operator UI:

```text
credential.fingerprint
credential.secret_fingerprint
credential.key_hint
request_execution.credential_fingerprint
request_execution.secret_fingerprint
request_execution.resource_scope_key
request_execution.credential_source
channel inline credential compatibility fields
masked raw API key fragments such as sk-... unless the user is in an explicit secret rotation/copy flow
```

These fields may remain in backend data, logs, database rows, and internal troubleshooting tools. They must not become default table cells, badges, tooltip content, detail fields, or "advanced" UI filler.

---

## 3. Contracts

- Tables should answer operator questions: what is it, what state is it in, how much has it used, can it route, what action is available, and what failed.
- Do not use raw backend identity as a fallback display label. If an entity has no user-facing name, show an unnamed label or `-`, not a fingerprint or key hint.
- Do not use backend IDs as a visible fallback label. If an entity has no user-facing name, show an unnamed label or `-`, not `id`, a fingerprint, or a key hint.
- Do not add "debug details" sections that simply move forbidden internal fields from a table into a detail view.
- Request logs should show channel name, credential name, request state, error, token/cost/latency metrics, and timestamps. They should not show key hints, secret fingerprints, resource scope keys, or credential source strings.
- Credential lists should show credential name, local quota, status, channel count, and update time. They should not show `secret:v1:*`, key hint, or `available · default scope` implementation wording.
- Quota UI must separate local quota and routing availability. Do not collapse them into one generic badge.
- Local quota display must use business language:

```text
2.89 / 576.94 USD · today's usage
59.14 / 100 USD · today's usage
5,160 / 50,000 Token · today's usage
No local quota
```

- Reset timing display must use the configured system timezone and reset rule, for example:

```text
Resets daily at 00:00 Asia/Shanghai
```

- If a value is needed only for developers to correlate backend rows, use request ID, credential ID, channel ID, logs, or backend diagnostics instead of exposing fingerprints in product UI.

---

## 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| UI code wants to display a fingerprint, secret fingerprint, resource scope key, credential source, or key hint | Reject the display. Use credential name, channel name, request ID, or an unnamed placeholder. |
| Credential has a quota scope with used and limit amounts | Show `<used> / <limit> <unit> · today's usage` or the localized equivalent. |
| Credential has no local quota scope | Show `No local quota`; do not show default internal scope wording. |
| Request execution has a credential name | Show the credential name. |
| Request execution lacks a credential name | Show `-` or `Unknown credential`; do not fall back to key hint or fingerprint. |
| Operator needs to diagnose a failed request | Show request ID, status, error, channel, credential name, timing, usage, and cost. Keep backend identities out of UI. |
| A GraphQL query includes forbidden fields only for display | Remove them from the frontend query and schema types used by that UI. |

---

## 5. Good / Base / Bad Cases

- Good: credential table local quota cell says `2.89 / 576.94 USD · today's usage`.
- Good: request table credential cell says `input(lite)` and shows execution status.
- Good: a failed request detail shows request ID, channel, credential name, status, error message, latency, token usage, and cost.
- Base: unnamed credential displays `Unnamed credential` and remains actionable through row actions.
- Bad: credential table name cell displays `secret:v1:c3a...`.
- Bad: request table credential cell displays `sk-2...f1da2a`.
- Bad: request table tooltip displays `ref · ai.input.im:secret:v1:* · available`.
- Bad: credential detail adds an "advanced" block that displays secret fingerprints or resource scope keys.
- Bad: local quota status says `available · credential default` instead of used/limit/window information.

---

## 6. Tests Required

When changing UI covered by this spec:

- Add or update component tests where the feature already has component test coverage.
- Add or update E2E coverage when a visible table/detail workflow is central to the task.
- Verify credential tables do not render `secret:v1`, masked raw API key prefixes, or key hints.
- Verify request logs do not render `resourceScopeKey`, secret fingerprints, credential fingerprints, or credential source strings.
- Verify local quota cells render used amount, limit amount, unit, and today's-usage wording.
- Verify system quota reset settings render and persist timezone/reset-time values.

---

## 7. Wrong vs Correct

### Wrong

```text
codexforme
secret:v1:c3a...dab757d1
clp_...960b5f
available · credential default
```

### Correct

```text
codexforme

Local quota
2.89 / 576.94 USD · today's usage

```

### Wrong

```text
input(lite)
sk-2...f1da2a
ref · ai.input.im:secret:v1:536d... · available
```

### Correct

```text
Channel: input
Credential: input(lite)
Status: completed
```
