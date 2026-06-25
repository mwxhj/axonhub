# Quality Guidelines

> Code quality standards for backend development.

---

## Overview

This file defines project-level quality rules that apply even when the local
implementation still carries older compatibility paths.

The most important rule for routing, quota, fallback, and credential work is:

- user-facing strategy names must map to one stable semantic contract;
- degradation may exist, but it must not silently change the meaning of the
  configured feature.

Project note:

- keep globally auto-loaded rule files short and high-signal;
- place detailed backend design rules here instead of inflating `AGENTS.md`
  with engineering textbook material or task-local decisions.

---

## Forbidden Patterns

### Don't: Silent semantic fallback

Do not keep one product/config/runtime label while silently switching to a
different behavioral contract underneath.

Examples of forbidden behavior:

- A strategy called `sticky-session` sometimes behaves as sticky rebind and
  sometimes behaves as ordinary load balancing without exposing why.
- A quota-aware routing policy silently degrades to non-quota routing because
  prerequisite data was missing, stale, or invalid.
- A fallback path changes user-visible routing semantics but leaves no explicit
  reason in code comments, logs, tests, or request execution metadata.

Why this is forbidden:

- It destroys operator trust. The name the user configures is no longer the
  behavior the system actually provides.
- It makes incidents hard to explain because two different routing policies
  appear under one label.
- It hides data quality bugs and keeps the system "working" in a way that
  prevents fast diagnosis.

### Don't: Compatibility-first behavior drift

Do not preserve an old path merely because it keeps requests moving if that
path changes the advertised semantics of the feature.

Compatibility paths are allowed only when all of the following are true:

- the degraded behavior is explicitly documented;
- the trigger condition is observable;
- tests cover both the primary path and the degraded path;
- there is a clear boundary showing which semantics still hold and which do
  not.

---

## Required Patterns

### Required: Linus-style design discipline

When reviewing or designing backend code, prefer the design discipline commonly
associated with Linus Torvalds:

1. data structures and data flow come before clever control flow;
2. one structure should represent one layer of truth, not several time phases
   mashed together;
3. one feature/config name should correspond to one runtime meaning;
4. solve the common case with the simplest correct model first, then add
   narrowly scoped escape hatches only when reality forces them;
5. if a path exists only to compensate for an earlier design weakness, treat
   that as design debt, not as a new core abstraction.

Applied to this project, that means:

- do not let a single runtime object simultaneously act as candidate pool,
  execution plan, retry queue, and success-memory state;
- do not let selection-time logic absorb failure-recovery responsibilities;
- do not let fallback behavior become the hidden definition of the primary
  routing model;
- do not preserve an abstraction whose only job is to hide that upstream
  capability modeling, credential modeling, or request classification is weak.

Review questions:

- Is this structure representing one phase of truth, or several?
- Is this branch expressing a real product/runtime concept, or compensating for
  an earlier modeling failure?
- If the fallback path were removed, would the primary design still make sense?
- Is a new abstraction reducing actual complexity, or merely moving it around?

### Required: Strong semantic consistency

When a feature is named, configured, logged, or surfaced to operators, it must
have one primary semantic contract.

For backend routing features, this means:

1. define the primary behavior in one place;
2. define all prerequisites explicitly;
3. define every degradation path explicitly;
4. ensure degradation stays observable;
5. ensure tests encode the semantic boundary, not only the happy path.

### Required: Explicit degradation contract

If a feature cannot execute its primary behavior because prerequisites are not
met, the code must make the degradation visible and reviewable.

At minimum, one of these must be true:

- the degraded path is a documented sub-state of the same strategy;
- the code records the degrade reason in structured logs, state, or execution
  metadata;
- tests prove exactly when the code stays in the primary semantic and when it
  leaves it.

Good example:

- `sticky-session` first attempts its documented rebind policy;
- if comparable quota data is unavailable, the code returns a named degrade
  result and tests cover that boundary.

Bad example:

- `sticky-session` quietly falls back to a normal load balancer branch and the
  only evidence is a debug-only label with no documented contract.

### Required: Semantic review before compatibility review

During implementation and review, check these questions before asking whether a
fallback path is convenient:

- Does the configured feature still mean one thing to the user?
- If data is missing or stale, is the resulting behavior still inside the same
  contract?
- If not, is the degrade path named, documented, and test-covered?
- Would an operator reading a trace or request execution be able to explain the
  outcome without reading source code?

---

## Testing Requirements

When changing routing, quota, sticky-session, retry, or credential selection:

- add tests for the primary semantic path;
- add tests for every explicit degradation path;
- assert not only the selected target, but also the semantic marker that proves
  which branch executed;
- prefer production-shaped fixtures over single-candidate toy layouts when the
  bug depends on channel-first vs credential-first behavior.

If a test only proves that "a request still succeeded", it is not enough for
semantic features. The test must prove why that target was selected.

## Scenario: Persisting Request Bodies

### 1. Scope / Trigger

- Trigger: changing request/request-execution persistence, body masking, trace
  snapshots, or any code that saves outbound request payloads to the database
  or external storage.

### 2. Signatures

- `internal/server/biz.(*RequestService).CreateRequest`
- `internal/server/biz.(*RequestService).CreateRequestExecution`
- `llm/httpclient.MaskSensitiveHeaders(http.Header) http.Header`

### 3. Contracts

- Persisted `request_body` and `request_execution.request_body` must remain
  valid JSON when the source payload is valid JSON.
- Header masking may replace sensitive header values because headers are stored
  as structured key/value metadata.
- Request body persistence must not mutate message/prompt text by applying
  regex or free-text replacement across the serialized JSON document.
- If a request body cannot be stored, the code must surface that as a
  persistence failure, not as a fake content diagnosis.

### 4. Validation & Error Matrix

- Valid JSON request body -> stored as valid JSON.
- Sensitive header present -> header value masked in persisted headers.
- Body contains strings like `api_key`, `secret`, `authorization`, or prompt
  text mentioning those tokens -> persistence must still preserve valid JSON.
- Body persistence fails -> log/store a persistence error; do not treat the
  body content itself as `invalid text`.

### 5. Good / Base / Bad Cases

- Good: execution request body stores the exact outbound JSON payload while
  request headers store masked auth values.
- Base: a normal OpenAI/Responses JSON body is persisted unchanged.
- Bad: regex-based body masking rewrites prompt text and produces invalid JSON
  before inserting into `jsonb`.

### 6. Tests Required

- Unit test that `CreateRequestExecution` preserves a JSON body containing
  secret-shaped field names and values.
- Unit test that header masking still hides sensitive header values.
- Regression coverage for request bodies containing long prompt text with words
  like `api_key`, `access_token`, or `secret`.

### 7. Wrong vs Correct

#### Wrong

```go
requestBodyBytes = objects.JSONRawMessage(httpclient.RedactSensitiveBody(requestBodyBytes))
```

#### Correct

```go
requestBodyBytes = channelRequest.JSONBody
requestHeadersBytes, _ = xjson.Marshal(httpclient.MaskSensitiveHeaders(channelRequest.Headers))
```

---

## Code Review Checklist

Reviewers must check:

- Does each major data structure represent one layer/time-phase of truth?
- Does each user-facing strategy/config name still correspond to one stable
  contract?
- Is any new abstraction removing complexity instead of hiding a bad boundary?
- Is fallback being used as recovery for runtime unknowns, rather than as a
  crutch for known modeling gaps?
- Are fallback/degrade paths explicitly named and documented?
- Are missing or stale prerequisite data treated as visible degradation instead
  of silent behavior drift?
- Do tests cover both the primary semantic and the degrade boundary?
- Can the final routing choice be explained from logs/state/tests without
  reverse-engineering the implementation?
