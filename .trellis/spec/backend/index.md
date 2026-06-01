# Backend Development Guidelines

> Best practices for backend development in this project.

---

## Overview

This directory contains guidelines for backend development. Fill in each file with your project's specific conventions.

---

## Pre-Development Checklist

Before changing backend routing, load balancing, retry/fallback, provider quota, credential handling, or circuit-breaker behavior:

- [ ] Read [Credential Routing Model](./credential-routing-model.md) before changing channel credentials, upstream credentials, OAuth, or provider quota behavior.
- [ ] Read [Routing Guidelines](./routing-guidelines.md), especially the sticky-session routing contract.
- [ ] Confirm whether the change can affect priority tiers, weight semantics, retry/fallback, or upstream cache locality.
- [ ] Confirm sticky-session writes happen only after upstream success and never from candidate selection alone.
- [ ] Confirm the change does not reintroduce channel-owned key/OAuth storage or channel-local quota. `CredentialQuotaScope` is allowed only as credential/key-local quota and may filter that credential view, not the whole channel.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Module organization and file layout | To fill |
| [Database Guidelines](./database-guidelines.md) | ORM patterns, queries, migrations, GraphQL transaction client usage | Active |
| [Credential Routing Model](./credential-routing-model.md) | Channel routing-only model, credential-owned secrets/OAuth, provider quota ownership, legacy migration | Active |
| [Error Handling](./error-handling.md) | Error types, handling strategies | To fill |
| [Routing Guidelines](./routing-guidelines.md) | Channel routing, sticky-session, retry/fallback, credential/quota contracts | Active |
| [Quality Guidelines](./quality-guidelines.md) | Code standards, forbidden patterns | To fill |
| [Logging Guidelines](./logging-guidelines.md) | Structured logging, log levels | To fill |

---

## How to Fill These Guidelines

For each guideline file:

1. Document your project's **actual conventions** (not ideals)
2. Include **code examples** from your codebase
3. List **forbidden patterns** and why
4. Add **common mistakes** your team has made

The goal is to help AI assistants and new team members understand how YOUR project works.

---

**Language**: All documentation should be written in **English**.
