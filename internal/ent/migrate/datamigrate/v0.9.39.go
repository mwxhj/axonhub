package datamigrate

import (
	"context"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
)

type V0_9_39 struct{}

func NewV0_9_39() DataMigrator {
	return &V0_9_39{}
}

func (v *V0_9_39) Version() string {
	return "v0.9.39"
}

func (v *V0_9_39) Migrate(ctx context.Context, client *ent.Client) (err error) {
	ctx = authz.WithSystemBypass(context.Background(), "database-migrate")

	channels, err := loadAPIKeyRouteMigrationChannels(ctx, client)
	if err != nil {
		return err
	}

	ctx, tx, err := client.OpenTx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	txClient := ent.FromContext(ctx)

	apiKeysUpdated, err := migrateAPIKeyProfiles(ctx, txClient, channels)
	if err != nil {
		return err
	}

	templatesUpdated, err := migrateAPIKeyProfileTemplates(ctx, txClient, channels)
	if err != nil {
		return err
	}

	log.Info(ctx, "migrated api key route tiers",
		log.Int("api_keys_updated", apiKeysUpdated),
		log.Int("templates_updated", templatesUpdated))

	return tx.Commit()
}

func loadAPIKeyRouteMigrationChannels(ctx context.Context, client *ent.Client) ([]biz.APIKeyProfileRouteChannel, error) {
	channels, err := client.Channel.Query().
		Where(channel.StatusEQ(channel.StatusEnabled)).
		Order(
			ent.Desc(channel.FieldOrderingWeight),
			ent.Asc(channel.FieldID),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]biz.APIKeyProfileRouteChannel, 0, len(channels))
	for _, ch := range channels {
		result = append(result, biz.APIKeyProfileRouteChannel{
			ID:             ch.ID,
			Tags:           append([]string(nil), ch.Tags...),
			OrderingWeight: ch.OrderingWeight,
		})
	}

	return result, nil
}

func migrateAPIKeyProfiles(
	ctx context.Context,
	client *ent.Client,
	channels []biz.APIKeyProfileRouteChannel,
) (int, error) {
	apiKeys, err := client.APIKey.Query().All(ctx)
	if err != nil {
		return 0, err
	}

	updated := 0
	for _, key := range apiKeys {
		if key.Profiles == nil {
			continue
		}
		for i := range key.Profiles.Profiles {
			if err := biz.MigrateAPIKeyProfileRoutes(&key.Profiles.Profiles[i], channels); err != nil {
				return updated, err
			}
		}

		if err := client.APIKey.UpdateOneID(key.ID).SetProfiles(key.Profiles).Exec(ctx); err != nil {
			return updated, err
		}
		updated++
	}

	return updated, nil
}

func migrateAPIKeyProfileTemplates(
	ctx context.Context,
	client *ent.Client,
	channels []biz.APIKeyProfileRouteChannel,
) (int, error) {
	templates, err := client.APIKeyProfileTemplate.Query().All(ctx)
	if err != nil {
		return 0, err
	}

	updated := 0
	for _, template := range templates {
		if template.Profile == nil {
			continue
		}
		if err := biz.MigrateAPIKeyProfileRoutes(template.Profile, channels); err != nil {
			return updated, err
		}
		if err := client.APIKeyProfileTemplate.UpdateOneID(template.ID).SetProfile(template.Profile).Exec(ctx); err != nil {
			return updated, err
		}
		updated++
	}

	return updated, nil
}
