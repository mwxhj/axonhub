package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/looplj/axonhub/internal/ent/schema/schematype"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/scopes"
)

type UpstreamCredential struct {
	ent.Schema
}

func (UpstreamCredential) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
		schematype.SoftDeleteMixin{},
	}
}

func (UpstreamCredential) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("fingerprint", "deleted_at").
			StorageKey("upstream_credentials_by_fingerprint").
			Unique(),
		index.Fields("provider_type", "status").
			StorageKey("upstream_credentials_by_provider_type_status"),
		index.Fields("base_url").
			StorageKey("upstream_credentials_by_base_url"),
	}
}

func (UpstreamCredential) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			Optional().
			Default("").
			Annotations(
				entgql.OrderField("NAME"),
			),
		field.String("provider_type").
			Immutable().
			Comment("Provider or channel type scope used to identify the upstream credential").
			Annotations(
				entgql.OrderField("PROVIDER_TYPE"),
			),
		field.String("base_url").
			Optional().
			Default("").
			Comment("Normalized upstream base URL scope for this credential").
			Annotations(
				entgql.OrderField("BASE_URL"),
			),
		field.Enum("auth_kind").
			Values("api_key", "oauth", "azure", "gcp", "other").
			Immutable().
			Annotations(
				entgql.OrderField("AUTH_KIND"),
			),
		field.JSON("secret_payload", objects.UpstreamCredentialSecret{}).
			Sensitive().
			Annotations(
				entgql.Skip(entgql.SkipAll),
			),
		field.String("fingerprint").
			MaxLen(128).
			Comment("Safe upstream credential identity; never contains raw secret material").
			Annotations(
				entgql.OrderField("FINGERPRINT"),
			),
		field.Enum("status").
			Values("enabled", "disabled", "archived").
			Default("enabled").
			Annotations(
				entgql.OrderField("STATUS"),
			),
		field.Int("weight").
			Default(100).
			Comment("Default credential selection weight inside eligible channel refs").
			Annotations(
				entgql.OrderField("WEIGHT"),
			),
		field.String("remark").
			Optional().
			Default(""),
	}
}

func (UpstreamCredential) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("channel_refs", ChannelCredentialRef.Type).
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
		edge.To("provider_quota_statuses", ProviderQuotaStatus.Type).
			Annotations(
				entgql.Skip(entgql.SkipMutationCreateInput, entgql.SkipMutationUpdateInput),
			),
	}
}

func (UpstreamCredential) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entgql.QueryField(),
		entgql.RelayConnection(),
	}
}

func (UpstreamCredential) Policy() ent.Policy {
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
