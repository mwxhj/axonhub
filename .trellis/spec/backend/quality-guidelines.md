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

---

## Code Review Checklist

Reviewers must check:

- Does each user-facing strategy/config name still correspond to one stable
  contract?
- Are fallback/degrade paths explicitly named and documented?
- Are missing or stale prerequisite data treated as visible degradation instead
  of silent behavior drift?
- Do tests cover both the primary semantic and the degrade boundary?
- Can the final routing choice be explained from logs/state/tests without
  reverse-engineering the implementation?
