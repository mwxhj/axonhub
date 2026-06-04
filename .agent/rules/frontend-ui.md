---
alwaysApply: false
globs: "frontend/**/*.tsx"
---

# Frontend UI Rules

1. When using `AutoComplete` or `AutoCompleteSelect` inside a `Dialog`, pass `portalContainer` pointing at the dialog container element to avoid scroll and layering issues.
2. Frontend UI must be human-facing, not a database browser. Every visible field must help the operator understand state, make a decision, or take an action.
3. Do not show implementation identities in normal product UI: fingerprints, secret fingerprints, resource scope keys, credential source strings, masked raw key hints, or compatibility refs. Use user-facing names, request IDs, channel names, credential names, status, errors, usage, cost, and timing instead.
4. Do not create "advanced" or detail sections that merely move internal identifiers out of tables. Developer-only correlation belongs in backend logs, database rows, or explicit backend tooling.
5. Quota displays must use business language. Prefer labels like `2.89 / 576.94 USD · today's usage` and `No local quota` over implementation labels like `available · credential default`.
6. Do not use backend IDs as a display fallback for names in product UI. If a user-facing name is missing, show an unnamed label or `-`, not `id`, a fingerprint, or a key hint.
