package biz

import (
	"context"
	"fmt"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
)

// ChannelOrderingItem represents a channel ordering update.
type ChannelOrderingItem struct {
	ID             int
	OrderingWeight int
}

// BulkUpdateChannelOrdering updates the ordering weight for multiple channels in a single transaction.
func (svc *ChannelService) BulkUpdateChannelOrdering(ctx context.Context, items []*ChannelOrderingItem) ([]*ent.Channel, error) {
	client := svc.entFromContext(ctx)

	updatedChannels := make([]*ent.Channel, 0, len(items))
	for _, update := range items {
		channel, err := client.Channel.
			UpdateOneID(update.ID).
			SetOrderingWeight(update.OrderingWeight).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to update channel %d: %w", update.ID, err)
		}

		updatedChannels = append(updatedChannels, channel)
	}

	svc.asyncReloadChannels()

	return updatedChannels, nil
}

func (svc *ChannelService) bulkUpdateChannelStatus(ctx context.Context, ids []int, status channel.Status, action string, clearErrorMessage bool) error {
	if len(ids) == 0 {
		return nil
	}

	client := svc.entFromContext(ctx)

	// Verify all channels exist
	count, err := client.Channel.Query().
		Where(channel.IDIn(ids...)).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("failed to query channels: %w", err)
	}

	if count != len(ids) {
		return fmt.Errorf("expected to find %d channels, but found %d", len(ids), count)
	}

	updater := client.Channel.Update().
		Where(channel.IDIn(ids...)).
		SetStatus(status)

	if clearErrorMessage {
		updater.ClearErrorMessage()
	}

	if _, err = updater.Save(ctx); err != nil {
		return fmt.Errorf("failed to %s channels: %w", action, err)
	}

	svc.asyncReloadChannels()

	return nil
}

// BulkArchiveChannels updates the status of multiple channels to archived.
func (svc *ChannelService) BulkArchiveChannels(ctx context.Context, ids []int) error {
	return svc.bulkUpdateChannelStatus(ctx, ids, channel.StatusArchived, "archive", false)
}

// BulkDisableChannels updates the status of multiple channels to disabled.
func (svc *ChannelService) BulkDisableChannels(ctx context.Context, ids []int) error {
	return svc.bulkUpdateChannelStatus(ctx, ids, channel.StatusDisabled, "disable", false)
}

// BulkEnableChannels updates the status of multiple channels to enabled.
func (svc *ChannelService) BulkEnableChannels(ctx context.Context, ids []int) error {
	return svc.bulkUpdateChannelStatus(ctx, ids, channel.StatusEnabled, "enable", false)
}

// BulkRecoverChannels enables multiple channels and clears their error messages.
func (svc *ChannelService) BulkRecoverChannels(ctx context.Context, ids []int) error {
	return svc.bulkUpdateChannelStatus(ctx, ids, channel.StatusEnabled, "recover", true)
}

// BulkDeleteChannels deletes multiple channels by their IDs.
func (svc *ChannelService) BulkDeleteChannels(ctx context.Context, ids []int) error {
	if len(ids) == 0 {
		return nil
	}

	deleted, err := svc.entFromContext(ctx).Channel.Delete().Where(channel.IDIn(ids...)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to bulk delete channels: %w", err)
	}

	for _, id := range ids {
		svc.forgetLimiter(id)
	}

	log.Info(ctx, "bulk deleted channels", log.Int("count", deleted))
	svc.asyncReloadChannels()

	return nil
}
