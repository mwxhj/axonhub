package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/looplj/axonhub/internal/ent/schema/schematype"
	"github.com/looplj/axonhub/internal/scopes"
)

type CredentialQuotaScope struct {
	ent.Schema
}

func (CredentialQuotaScope) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
		schematype.SoftDeleteMixin{},
	}
}

func (CredentialQuotaScope) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status").
			StorageKey("credential_quota_scopes_by_status"),
		index.Fields("source").
			StorageKey("credential_quota_scopes_by_source"),
		index.Fields("reset_at").
			StorageKey("credential_quota_scopes_by_reset_at"),
	}
}

func (CredentialQuotaScope) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			Optional().
			Default("").
			Annotations(
				entgql.OrderField("NAME"),
			),
		field.Enum("status").
			Values("available", "warning", "exhausted", "paused", "disabled", "unknown").
			Default("unknown").
			Annotations(
				entgql.OrderField("STATUS"),
			),
		field.Enum("unit").
			Values("usd", "token", "request", "credit", "custom", "unknown").
			Default("unknown").
			Annotations(
				entgql.OrderField("UNIT"),
			),
		field.String("limit_amount").
			Optional().
			Default("").
			Comment("Configured quota limit stored as decimal text to avoid precision loss").
			Annotations(
				entgql.OrderField("LIMIT_AMOUNT"),
			),
		field.String("used_amount").
			Optional().
			Default("").
			Comment("Current quota usage stored as decimal text to avoid precision loss").
			Annotations(
				entgql.OrderField("USED_AMOUNT"),
			),
		field.Int("warning_threshold_percent").
			Optional().
			Nillable().
			Comment("Warning threshold percentage for local budget scopes"),
		field.Enum("reset_policy").
			Values("none", "manual", "daily", "monthly", "custom").
			Default("none").
			Annotations(
				entgql.OrderField("RESET_POLICY"),
			),
		field.Time("reset_at").
			Optional().
			Nillable().
			Annotations(
				entgql.OrderField("RESET_AT"),
			),
		field.Time("window_started_at").
			Optional().
			Nillable().
			Annotations(
				entgql.OrderField("WINDOW_STARTED_AT"),
			),
		field.Enum("over_limit_action").
			Values("warn", "pause", "disable").
			Default("warn").
			Annotations(
				entgql.OrderField("OVER_LIMIT_ACTION"),
			),
		field.Time("pause_until").
			Optional().
			Nillable().
			Annotations(
				entgql.OrderField("PAUSE_UNTIL"),
			),
		field.Enum("source").
			Values("local_budget", "provider_api", "response_error", "manual", "inferred", "unknown").
			Default("local_budget").
			Annotations(
				entgql.OrderField("SOURCE"),
			),
		field.String("last_error").
			Optional().
			Default("").
			Comment("Latest quota error safe for operator display"),
		field.String("remark").
			Optional().
			Default(""),
	}
}

func (CredentialQuotaScope) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("credentials", UpstreamCredential.Type).
			Annotations(
				entgql.Skip(entgql.SkipMutationCreateInput, entgql.SkipMutationUpdateInput),
				entgql.RelayConnection(),
			),
		edge.To("executions", RequestExecution.Type).
			Annotations(
				entgql.Skip(entgql.SkipMutationCreateInput, entgql.SkipMutationUpdateInput),
				entgql.RelayConnection(),
			),
		edge.To("usage_logs", UsageLog.Type).
			Annotations(
				entgql.Skip(entgql.SkipMutationCreateInput, entgql.SkipMutationUpdateInput),
				entgql.RelayConnection(),
			),
	}
}

func (CredentialQuotaScope) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entgql.QueryField(),
		entgql.RelayConnection(),
	}
}

func (CredentialQuotaScope) Policy() ent.Policy {
	return scopes.Policy{
		Query: scopes.QueryPolicy{
			scopes.OwnerRule(),
			scopes.UserReadScopeRule(scopes.ScopeReadChannels),
		},
		Mutation: scopes.MutationPolicy{
			scopes.OwnerRule(),
			scopes.UserWriteScopeRule(scopes.ScopeWriteChannels),
		},
	}
}
