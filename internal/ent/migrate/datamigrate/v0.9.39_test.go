package datamigrate_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/migrate/datamigrate"
	"github.com/looplj/axonhub/internal/objects"
)

func TestV0_9_39_MigratesAPIKeyProfilesAndTemplatesToRouteTiers(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	project, err := client.Project.Create().SetName("route-migration-project").Save(ctx)
	require.NoError(t, err)

	primaryChannel := createRouteMigrationChannel(t, ctx, client, "primary", 10, []string{"human"})
	fallbackChannel := createRouteMigrationChannel(t, ctx, client, "fallback", 5, []string{"batch"})
	disabledChannel := createRouteMigrationChannel(t, ctx, client, "disabled", 100, []string{"disabled"})
	_, err = disabledChannel.Update().SetStatus(channel.StatusDisabled).Save(ctx)
	require.NoError(t, err)

	key, err := client.APIKey.Create().
		SetName("route-key").
		SetKey("ah-route-migration").
		SetProjectID(project.ID).
		SetProfiles(&objects.APIKeyProfiles{
			ActiveProfile: "legacy",
			Profiles: []objects.APIKeyProfile{
				{
					Name:       "legacy",
					ChannelIDs: []int{fallbackChannel.ID, primaryChannel.ID, primaryChannel.ID, 0},
				},
				{
					Name: "empty",
				},
				{
					Name:       "explicit",
					RouteTiers: []objects.APIKeyRouteTier{{Name: "chosen", ChannelIDs: []int{primaryChannel.ID}}},
					ChannelIDs: []int{fallbackChannel.ID},
				},
			},
		}).
		Save(ctx)
	require.NoError(t, err)

	template, err := client.APIKeyProfileTemplate.Create().
		SetName("template").
		SetProject(project).
		SetProfile(&objects.APIKeyProfile{
			Name:        "template",
			ChannelTags: []string{"batch"},
		}).
		Save(ctx)
	require.NoError(t, err)

	err = datamigrate.NewV0_9_39().Migrate(ctx, client)
	require.NoError(t, err)

	updatedKey, err := client.APIKey.Get(ctx, key.ID)
	require.NoError(t, err)
	require.Len(t, updatedKey.Profiles.Profiles, 3)

	legacyProfile := updatedKey.Profiles.Profiles[0]
	require.Equal(t, []int{fallbackChannel.ID, primaryChannel.ID}, legacyProfile.RouteTiers[0].ChannelIDs)
	require.Empty(t, legacyProfile.ChannelIDs)
	require.Equal(t, objects.APIKeyRouteMigrationSourceChannelIDs, legacyProfile.RouteMigration.Source)

	emptyProfile := updatedKey.Profiles.Profiles[1]
	require.Equal(t, []int{primaryChannel.ID, fallbackChannel.ID}, emptyProfile.RouteTiers[0].ChannelIDs)
	require.Equal(t, objects.APIKeyRouteMigrationSourceAllEnabledChannels, emptyProfile.RouteMigration.Source)

	explicitProfile := updatedKey.Profiles.Profiles[2]
	require.Equal(t, []int{primaryChannel.ID}, explicitProfile.RouteTiers[0].ChannelIDs)
	require.Empty(t, explicitProfile.ChannelIDs)
	require.Equal(t, objects.APIKeyRouteMigrationSourceExplicit, explicitProfile.RouteMigration.Source)

	updatedTemplate, err := client.APIKeyProfileTemplate.Get(ctx, template.ID)
	require.NoError(t, err)
	require.Equal(t, []int{fallbackChannel.ID}, updatedTemplate.Profile.RouteTiers[0].ChannelIDs)
	require.Empty(t, updatedTemplate.Profile.ChannelTags)
	require.Equal(t, objects.APIKeyRouteMigrationSourceChannelTags, updatedTemplate.Profile.RouteMigration.Source)
}

func createRouteMigrationChannel(
	t *testing.T,
	ctx context.Context,
	client *ent.Client,
	name string,
	orderingWeight int,
	tags []string,
) *ent.Channel {
	t.Helper()

	ch, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName(name).
		SetBaseURL("https://example.com/v1").
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetOrderingWeight(orderingWeight).
		SetTags(tags).
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	return ch
}
