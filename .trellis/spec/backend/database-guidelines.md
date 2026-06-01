# Database Guidelines

> Database patterns and conventions for this project.

---

## Overview

<!--
Document your project's database conventions here.

Questions to answer:
- What ORM/query library do you use?
- How are migrations managed?
- What are the naming conventions for tables/columns?
- How do you handle transactions?
-->

(To be filled by the team)

---

## Query Patterns

### GraphQL Association Resolvers Inside Mutations

#### 1. Scope / Trigger

This applies to GraphQL resolvers that load optional associations from returned mutation payloads, especially helpers used by fields such as `channel`, `credential`, or `quotaScope`.

#### 2. Signatures

Resolver helpers should accept the request context and a fallback client:

```go
func getNilableThing(ctx context.Context, client *ent.Client, id int) (*ent.Thing, error)
```

When a transaction client exists in context, use it:

```go
func clientFromContext(ctx context.Context, fallback *ent.Client) *ent.Client {
    if client := ent.FromContext(ctx); client != nil {
        return client
    }
    return fallback
}
```

#### 3. Contracts

GraphQL mutations are wrapped by `entgql.Transactioner`. During mutation response shaping, child field resolvers may run before the transaction is committed. Association resolvers must query with the transaction client from `ent.FromContext(ctx)` when present, falling back to the root resolver client only outside a transaction.

#### 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| `id == 0` or nil-like foreign key | Return `nil, nil`. |
| Entity not found | Return `nil, nil`. |
| Ent privacy denies association read | Return `nil, nil`. |
| Any other database error | Return a wrapped field-specific error. |
| `ent.FromContext(ctx)` returns a client | Use that client for the query. |

#### 5. Good / Base / Bad Cases

- Good: `createUpstreamCredential` returns `quotaScope` by querying through the transaction client used by the mutation.
- Base: a query resolver has no transaction client in context and safely falls back to `r.client`.
- Bad: a mutation creates an entity, then a child resolver uses the root client to reload an association while the transaction is still open.

#### 6. Tests Required

When changing mutation return fields or nullable association resolvers, add a GraphQL handler-level regression test that:

- Executes the actual mutation through the GraphQL handler.
- Requests the frontend-like selection set, including association fields.
- Covers a nil association and a present association.
- Fails on GraphQL errors, timeouts, or missing returned IDs.

#### 7. Wrong vs Correct

Wrong:

```go
scope, err := r.client.CredentialQuotaScope.Query().Where(credentialquotascope.ID(id)).First(ctx)
```

Correct:

```go
client := clientFromContext(ctx, r.client)
scope, err := client.CredentialQuotaScope.Query().Where(credentialquotascope.ID(id)).First(ctx)
```

---

## Migrations

<!-- How to create and run migrations -->

(To be filled by the team)

---

## Naming Conventions

<!-- Table names, column names, index names -->

(To be filled by the team)

---

## Common Mistakes

- Using the root resolver Ent client inside a mutation response child resolver. This can block under SQLite single-connection tests and can miss uncommitted mutation state.
