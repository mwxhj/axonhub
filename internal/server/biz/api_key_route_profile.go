package biz

import (
	"fmt"
	"slices"
	"strings"

	"github.com/looplj/axonhub/internal/objects"
)

type APIKeyProfileRouteChannel struct {
	ID             int
	Tags           []string
	OrderingWeight int
}

func enabledRouteChannels(channels []*Channel) []APIKeyProfileRouteChannel {
	result := make([]APIKeyProfileRouteChannel, 0, len(channels))
	for _, ch := range channels {
		if ch == nil {
			continue
		}
		result = append(result, APIKeyProfileRouteChannel{
			ID:             ch.ID,
			Tags:           append([]string(nil), ch.Tags...),
			OrderingWeight: ch.OrderingWeight,
		})
	}
	sortProfileRouteChannels(result)
	return result
}

func normalizeAPIKeyProfilesRoutes(profiles *objects.APIKeyProfiles) {
	if profiles == nil {
		return
	}

	for i := range profiles.Profiles {
		normalizeAPIKeyProfileRoute(&profiles.Profiles[i])
	}
}

func normalizeAPIKeyProfileRoute(profile *objects.APIKeyProfile) {
	if profile == nil {
		return
	}

	if hasRouteTiers(profile.RouteTiers) {
		profile.RouteTiers = normalizeRouteTiers(profile.RouteTiers)
		clearLegacyAPIKeyRouteFilters(profile)
		ensureRouteMigration(profile, objects.APIKeyRouteMigrationSourceExplicit)
		return
	}
	profile.RouteTiers = nil
}

func migrateAPIKeyProfileRoutesForSave(profile *objects.APIKeyProfile, channels []APIKeyProfileRouteChannel) error {
	if profile == nil {
		return nil
	}

	if hasRouteTiers(profile.RouteTiers) {
		profile.RouteTiers = normalizeRouteTiers(profile.RouteTiers)
		clearLegacyAPIKeyRouteFilters(profile)
		ensureRouteMigration(profile, objects.APIKeyRouteMigrationSourceExplicit)
		return nil
	}

	if len(profile.ChannelIDs) > 0 {
		profile.RouteTiers = []objects.APIKeyRouteTier{{
			Name:       defaultRouteTierName(objects.APIKeyRouteMigrationSourceChannelIDs),
			ChannelIDs: uniquePositiveInts(profile.ChannelIDs),
		}}
		clearLegacyAPIKeyRouteFilters(profile)
		ensureRouteMigration(profile, objects.APIKeyRouteMigrationSourceChannelIDs)
		return nil
	}

	if len(profile.ChannelTags) > 0 {
		if !profile.ChannelTagsMatchMode.IsValid() {
			return fmt.Errorf("profile '%s' channelTagsMatchMode is invalid", profile.Name)
		}
		profile.RouteTiers = []objects.APIKeyRouteTier{{
			Name: defaultRouteTierName(objects.APIKeyRouteMigrationSourceChannelTags),
			ChannelIDs: routeChannelIDsMatchingTags(
				channels,
				profile.ChannelTags,
				profile.ChannelTagsMatchMode,
			),
		}}
		clearLegacyAPIKeyRouteFilters(profile)
		ensureRouteMigration(profile, objects.APIKeyRouteMigrationSourceChannelTags)
		return nil
	}

	profile.RouteTiers = nil
	return nil
}

func validateProfileRoutes(profiles []objects.APIKeyProfile) error {
	for _, profile := range profiles {
		if !hasRouteTiers(profile.RouteTiers) {
			return fmt.Errorf("profile '%s' routeTiers must contain at least one channel", profile.Name)
		}
		if profile.PreferredChannelID != nil && !routeTiersContainChannel(profile.RouteTiers, *profile.PreferredChannelID) {
			return fmt.Errorf("profile '%s' preferredChannelID must exist in routeTiers", profile.Name)
		}
	}

	return nil
}

func MigrateAPIKeyProfileRoutes(profile *objects.APIKeyProfile, channels []APIKeyProfileRouteChannel) error {
	if profile == nil {
		return nil
	}

	if hasRouteTiers(profile.RouteTiers) {
		profile.RouteTiers = normalizeRouteTiers(profile.RouteTiers)
		clearLegacyAPIKeyRouteFilters(profile)
		ensureRouteMigration(profile, objects.APIKeyRouteMigrationSourceExplicit)
		return nil
	}

	source := objects.APIKeyRouteMigrationSourceAllEnabledChannels
	channelIDs := routeChannelIDs(channels)
	if len(profile.ChannelIDs) > 0 {
		source = objects.APIKeyRouteMigrationSourceChannelIDs
		channelIDs = uniquePositiveInts(profile.ChannelIDs)
	} else if len(profile.ChannelTags) > 0 {
		if !profile.ChannelTagsMatchMode.IsValid() {
			return fmt.Errorf("profile '%s' channelTagsMatchMode is invalid", profile.Name)
		}
		source = objects.APIKeyRouteMigrationSourceChannelTags
		channelIDs = routeChannelIDsMatchingTags(channels, profile.ChannelTags, profile.ChannelTagsMatchMode)
	}

	profile.RouteTiers = []objects.APIKeyRouteTier{{
		Name:       defaultRouteTierName(source),
		ChannelIDs: channelIDs,
	}}
	clearLegacyAPIKeyRouteFilters(profile)
	ensureRouteMigration(profile, source)
	return nil
}

func hasRouteTiers(tiers []objects.APIKeyRouteTier) bool {
	for _, tier := range tiers {
		if len(uniquePositiveInts(tier.ChannelIDs)) > 0 {
			return true
		}
	}

	return false
}

func normalizeRouteTiers(tiers []objects.APIKeyRouteTier) []objects.APIKeyRouteTier {
	result := make([]objects.APIKeyRouteTier, 0, len(tiers))
	for i, tier := range tiers {
		channelIDs := uniquePositiveInts(tier.ChannelIDs)
		if len(channelIDs) == 0 {
			continue
		}
		name := strings.TrimSpace(tier.Name)
		if name == "" {
			name = fmt.Sprintf("Tier %d", i+1)
		}
		result = append(result, objects.APIKeyRouteTier{
			Name:       name,
			ChannelIDs: channelIDs,
		})
	}

	return result
}

func routeTiersContainChannel(tiers []objects.APIKeyRouteTier, channelID int) bool {
	for _, tier := range tiers {
		if slices.Contains(tier.ChannelIDs, channelID) {
			return true
		}
	}

	return false
}

func routeChannelIDs(channels []APIKeyProfileRouteChannel) []int {
	result := make([]int, 0, len(channels))
	for _, ch := range channels {
		if ch.ID > 0 {
			result = append(result, ch.ID)
		}
	}

	return uniquePositiveInts(result)
}

func routeChannelIDsMatchingTags(
	channels []APIKeyProfileRouteChannel,
	tags []string,
	matchMode objects.ChannelTagsMatchMode,
) []int {
	result := make([]int, 0, len(channels))
	for _, ch := range channels {
		if objects.MatchChannelTags(tags, matchMode, ch.Tags) {
			result = append(result, ch.ID)
		}
	}

	return uniquePositiveInts(result)
}

func uniquePositiveInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result
}

func sortProfileRouteChannels(channels []APIKeyProfileRouteChannel) {
	slices.SortStableFunc(channels, func(a, b APIKeyProfileRouteChannel) int {
		if a.OrderingWeight > b.OrderingWeight {
			return -1
		}
		if a.OrderingWeight < b.OrderingWeight {
			return 1
		}
		return a.ID - b.ID
	})
}

func ensureRouteMigration(profile *objects.APIKeyProfile, source string) {
	if profile.RouteMigration != nil {
		return
	}

	profile.RouteMigration = &objects.APIKeyRouteMigration{
		Version: objects.APIKeyRouteMigrationVersion,
		Source:  source,
	}
}

func clearLegacyAPIKeyRouteFilters(profile *objects.APIKeyProfile) {
	profile.ChannelIDs = nil
	profile.ChannelTags = nil
	profile.ChannelTagsMatchMode = ""
}

func defaultRouteTierName(source string) string {
	switch source {
	case objects.APIKeyRouteMigrationSourceChannelIDs:
		return "Migrated channel list"
	case objects.APIKeyRouteMigrationSourceChannelTags:
		return "Migrated channel tags"
	case objects.APIKeyRouteMigrationSourceAllEnabledChannels:
		return "Migrated enabled channels"
	default:
		return "Primary"
	}
}
