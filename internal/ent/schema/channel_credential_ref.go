package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/looplj/axonhub/internal/scopes"
)

type ChannelCredentialRef struct {
	ent.Schema
}

func (ChannelCredentialRef) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
	}
}

func (ChannelCredentialRef) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("channel_id", "credential_id").
			StorageKey("channel_credential_refs_by_channel_id_credential_id").
			Unique(),
		index.Fields("channel_id", "enabled").
			StorageKey("channel_credential_refs_by_channel_id_enabled"),
		index.Fields("credential_id").
			StorageKey("channel_credential_refs_by_credential_id"),
	}
}

func (ChannelCredentialRef) Fields() []ent.Field {
	return []ent.Field{
		field.Int("channel_id").
			Immutable(),
		field.Int("credential_id").
			Immutable(),
		field.Bool("enabled").
			Default(true),
		field.Int("weight_override").
			Optional().
			Nillable().
			Comment("Internal optional per-channel selection weight. When absent, uses credential.weight.").
			Annotations(
				entgql.Skip(entgql.SkipType, entgql.SkipWhereInput),
			),
	}
}

func (ChannelCredentialRef) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("channel", Channel.Type).
			Ref("credential_refs").
			Field("channel_id").
			Required().
			Immutable().
			Unique().
			Annotations(
				entgql.Directives(forceResolver()),
			),
		edge.From("credential", UpstreamCredential.Type).
			Ref("channel_refs").
			Field("credential_id").
			Required().
			Immutable().
			Unique().
			Annotations(
				entgql.Directives(forceResolver()),
			),
	}
}

func (ChannelCredentialRef) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entgql.QueryField(),
		entgql.RelayConnection(),
	}
}

func (ChannelCredentialRef) Policy() ent.Policy {
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
