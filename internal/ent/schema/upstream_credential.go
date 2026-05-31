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
		index.Fields("issuer_scope", "status").
			StorageKey("upstream_credentials_by_issuer_scope_status"),
		index.Fields("base_url").
			StorageKey("upstream_credentials_by_base_url"),
		index.Fields("quota_scope_id").
			StorageKey("upstream_credentials_by_quota_scope_id"),
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
			Default("").
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
			Default("api_key").
			Immutable().
			Annotations(
				entgql.OrderField("AUTH_KIND"),
			),
		field.Enum("secret_kind").
			Values("api_key", "oauth", "azure", "gcp", "other").
			Default("api_key").
			Comment("Internal secret kind for the upstream account asset; not a channel/API-format setting").
			Annotations(
				entgql.OrderField("SECRET_KIND"),
			),
		field.String("issuer_scope").
			Optional().
			Default("").
			Comment("Coarse upstream issuer namespace used for credential identity; not the channel base URL").
			Annotations(
				entgql.OrderField("ISSUER_SCOPE"),
			),
		field.String("key_hint").
			Optional().
			Default("").
			Comment("Safe display hint for the secret; never contains the full secret").
			Annotations(
				entgql.OrderField("KEY_HINT"),
			),
		field.Int("quota_scope_id").
			Optional().
			Nillable().
			Comment("Optional shared quota/billing scope for credentials that belong to one upstream account pool").
			Annotations(
				entgql.OrderField("QUOTA_SCOPE_ID"),
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
		field.String("quota_status").
			Optional().
			Default("unknown").
			Comment("Latest credential-scoped quota or budget status summary").
			Annotations(
				entgql.OrderField("QUOTA_STATUS"),
			),
		field.String("last_error").
			Optional().
			Default("").
			Comment("Latest credential-scoped error summary safe for operator display"),
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
