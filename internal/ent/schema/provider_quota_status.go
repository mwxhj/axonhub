package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/looplj/axonhub/internal/ent/schema/schematype"
)

type ProviderQuotaStatus struct {
	ent.Schema
}

func (ProviderQuotaStatus) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},                  // Provides created_at, updated_at
		schematype.SoftDeleteMixin{}, // Provides deleted_at for soft delete
	}
}

func (ProviderQuotaStatus) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_type", "scope_key").
			StorageKey("provider_quota_status_by_provider_scope"),
		index.Fields("credential_id"),
		index.Fields("credential_fingerprint"),
		index.Fields("secret_fingerprint"),
		index.Fields("resource_scope_key"),
		index.Fields("quota_scope_id"),
		index.Fields("channel_id"),
		index.Fields("channel_id", "credential_id", "resource_scope_key").
			StorageKey("provider_quota_status_by_channel_credential_resource"),
		index.Fields("next_check_at"),
	}
}

func (ProviderQuotaStatus) Fields() []ent.Field {
	return []ent.Field{
		field.Int("channel_id").
			Optional().
			Comment("Channel that observed or last updated this quota status; not part of provider quota identity"),
		field.String("scope_key").
			Default("channel").
			MaxLen(640).
			Comment("Stable provider quota row identity, e.g. channel:<id>, credential/resource/quota scope"),
		field.Int("credential_id").
			Optional().
			Comment("Upstream credential represented by this quota status when known"),
		field.String("credential_fingerprint").
			Optional().
			MaxLen(128).
			Comment("Legacy safe upstream credential identity represented by this quota status when known"),
		field.String("secret_fingerprint").
			Optional().
			MaxLen(128).
			Comment("Safe secret-only identity represented by this quota status when known"),
		field.String("resource_scope_key").
			Optional().
			MaxLen(512).
			Comment("Safe runtime resource scope represented by this quota status when known"),
		field.Int("quota_scope_id").
			Optional().
			Comment("Credential quota scope represented by this quota status when known"),
		field.Enum("provider_type").
			Values("claudecode", "codex", "github_copilot", "nanogpt", "wafer", "synthetic", "neuralwatt").
			Immutable(),
		field.Enum("status").
			Values("available", "warning", "exhausted", "unknown").
			Comment("Overall status: available, warning, exhausted, unknown"),
		field.JSON("quota_data", map[string]any{}).
			Comment("Provider-specific quota data"),
		field.Time("next_reset_at").
			Optional().
			Nillable().
			Comment("Timestamp for next quota reset (primary window)"),
		field.Bool("ready").
			Default(true).
			Comment("True if status is available or warning"),
		field.Time("next_check_at").
			Comment("Timestamp for next scheduled quota check"),
	}
}

func (ProviderQuotaStatus) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("channel", Channel.Type).
			Ref("provider_quota_statuses").
			Field("channel_id").
			Unique(),
		edge.From("credential", UpstreamCredential.Type).
			Ref("provider_quota_statuses").
			Field("credential_id").
			Unique(),
		edge.From("quota_scope", CredentialQuotaScope.Type).
			Ref("provider_quota_statuses").
			Field("quota_scope_id").
			Unique(),
	}
}
