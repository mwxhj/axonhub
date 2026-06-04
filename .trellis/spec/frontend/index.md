# Frontend Development Guidelines

> Product-facing UI and frontend implementation contracts for AxonHub.

---

## Pre-Development Checklist

Before changing frontend tables, dialogs, detail pages, request logs, credential views, quota views, or system settings:

- [ ] Read [Human-Computer Interaction Rules](./human-computer-interaction.md).
- [ ] Confirm each visible field helps the operator understand state, make a decision, or take an action.
- [ ] Confirm the UI does not expose internal implementation identifiers such as fingerprints, secret fingerprints, resource scope keys, raw key hints, or compatibility refs.
- [ ] Confirm quota displays use business language: used amount, limit, unit, current window, reset rule, and routing impact when relevant.
- [ ] Confirm developer-only diagnostics stay in backend logs, database rows, or explicit backend tooling, not product UI.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Human-Computer Interaction Rules](./human-computer-interaction.md) | User-facing information hierarchy, forbidden internal-field display, quota/request-log presentation | Active |
