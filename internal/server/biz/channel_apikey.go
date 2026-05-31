package biz

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcontext"
)

// DisableAPIKey 禁用指定 key；若所有 key 都不可用则禁用 channel.
func (svc *ChannelService) DisableAPIKey(ctx context.Context, channelID int, key string, errorCode int, reason string) error {
	if key == "" {
		return fmt.Errorf("api key cannot be empty")
	}

	// 读取 channel
	ch, err := svc.entFromContext(ctx).Channel.Get(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}

	// 检查 key 是否在 credentials 中
	allKeys := ch.Credentials.GetAllAPIKeys()

	found := slices.Contains(allKeys, key)
	if !found {
		// key 不在 credentials 中，忽略
		return nil
	}

	disabled := lo.ContainsBy(ch.DisabledAPIKeys, func(dk objects.DisabledAPIKey) bool {
		return dk.Key == key
	})

	if disabled {
		// 已禁用，忽略
		return nil
	}

	// 追加到 disabled_api_keys
	disabledKey := objects.DisabledAPIKey{
		Key:        key,
		DisabledAt: time.Now(),
		ErrorCode:  errorCode,
		Reason:     reason,
	}

	newDisabledKeys := append(ch.DisabledAPIKeys, disabledKey)

	// 计算 enabled keys
	enabledKeys := ch.Credentials.GetEnabledAPIKeys(newDisabledKeys)

	// 更新 channel
	update := svc.entFromContext(ctx).Channel.UpdateOneID(channelID).
		SetDisabledAPIKeys(newDisabledKeys)

	// 如果没有可用 key 了，禁用整个 channel
	channelDisabled := len(enabledKeys) == 0
	if channelDisabled {
		update.SetStatus(channel.StatusDisabled)
		update.SetErrorMessage(fmt.Sprintf("All API keys disabled (last error: %d)", errorCode))
		log.Warn(ctx, "Channel disabled because all API keys are disabled",
			log.Int("channel_id", channelID),
			log.String("channel_name", ch.Name),
		)
	}

	if _, err := update.Save(ctx); err != nil {
		return fmt.Errorf("failed to disable api key: %w", err)
	}

	log.Info(ctx, "API key disabled",
		log.Int("channel_id", channelID),
		log.Int("error_code", errorCode),
	)

	if channelDisabled {
		// Synchronously reload the local cache to immediately stop selecting this channel.
		// This matches the behavior of markChannelUnavailable.
		reloadCtx, cancel := xcontext.DetachWithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := svc.enabledChannelsCache.Load(reloadCtx, true); err != nil {
			log.Warn(ctx, "Failed to synchronously reload channels after API key exhaustion",
				log.Int("channel_id", channelID),
				log.Cause(err),
			)
		}
	}

	// Also notify other instances via the watcher for cross-instance cache invalidation.
	svc.asyncReloadChannels()

	return nil
}

// DisableCredentialFingerprint disables every API key matching the derived credential fingerprint.
// This keeps duplicated upstream keys across channels in the same failure domain without exposing the raw key.
func (svc *ChannelService) DisableCredentialFingerprint(ctx context.Context, fingerprint string, errorCode int, reason string) (int, error) {
	if fingerprint == "" {
		return 0, fmt.Errorf("credential fingerprint cannot be empty")
	}

	channels, err := svc.entFromContext(ctx).Channel.Query().All(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to query channels: %w", err)
	}

	affectedKeys := 0
	disabledChannels := 0

	for _, ch := range channels {
		matchingKeys := make(map[string]struct{})
		for _, key := range ch.Credentials.GetAllAPIKeys() {
			if ChannelCredentialFingerprintForAPIKey(ch.Type.String(), ch.BaseURL, key) == fingerprint {
				matchingKeys[key] = struct{}{}
			}
		}
		if len(matchingKeys) == 0 {
			continue
		}

		disabledSet := make(map[string]struct{}, len(ch.DisabledAPIKeys)+len(matchingKeys))
		for _, dk := range ch.DisabledAPIKeys {
			disabledSet[dk.Key] = struct{}{}
		}

		newDisabledKeys := slices.Clone(ch.DisabledAPIKeys)
		channelChanged := false
		now := time.Now()

		for key := range matchingKeys {
			if _, disabled := disabledSet[key]; disabled {
				continue
			}

			newDisabledKeys = append(newDisabledKeys, objects.DisabledAPIKey{
				Key:        key,
				DisabledAt: now,
				ErrorCode:  errorCode,
				Reason:     reason,
			})
			disabledSet[key] = struct{}{}
			channelChanged = true
			affectedKeys++
		}

		if !channelChanged {
			continue
		}

		enabledKeys := ch.Credentials.GetEnabledAPIKeys(newDisabledKeys)
		update := svc.entFromContext(ctx).Channel.UpdateOneID(ch.ID).
			SetDisabledAPIKeys(newDisabledKeys)

		if len(enabledKeys) == 0 {
			update.SetStatus(channel.StatusDisabled)
			update.SetErrorMessage(fmt.Sprintf("All API keys disabled (last credential error: %d)", errorCode))
			disabledChannels++
		}

		if _, err := update.Save(ctx); err != nil {
			return affectedKeys, fmt.Errorf("failed to disable credential on channel %d: %w", ch.ID, err)
		}
	}

	if affectedKeys == 0 {
		return 0, nil
	}

	log.Info(ctx, "Credential disabled across channels",
		log.String("credential_fingerprint", fingerprint),
		log.Int("affected_keys", affectedKeys),
		log.Int("disabled_channels", disabledChannels),
		log.Int("error_code", errorCode),
	)

	reloadCtx, cancel := xcontext.DetachWithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := svc.enabledChannelsCache.Load(reloadCtx, true); err != nil {
		log.Warn(ctx, "Failed to synchronously reload channels after credential disable",
			log.String("credential_fingerprint", fingerprint),
			log.Cause(err),
		)
	}

	svc.asyncReloadChannels()

	return affectedKeys, nil
}

// DisableCredentialID disables a first-class upstream credential globally.
// The channel cache then resolves every attached ChannelCredentialRef as
// unavailable while leaving unrelated credentials on the same channels usable.
func (svc *ChannelService) DisableCredentialID(ctx context.Context, credentialID int, errorCode int, reason string) (int, error) {
	if credentialID <= 0 {
		return 0, fmt.Errorf("credential id cannot be empty")
	}

	affected, err := svc.entFromContext(ctx).UpstreamCredential.Update().
		Where(
			upstreamcredential.ID(credentialID),
			upstreamcredential.StatusEQ(upstreamcredential.StatusEnabled),
		).
		SetStatus(upstreamcredential.StatusDisabled).
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to disable upstream credential: %w", err)
	}
	if affected == 0 {
		exists, err := svc.entFromContext(ctx).UpstreamCredential.Query().
			Where(upstreamcredential.ID(credentialID)).
			Exist(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to check upstream credential: %w", err)
		}
		if exists {
			return 1, nil
		}

		return 0, nil
	}

	log.Info(ctx, "Upstream credential disabled",
		log.Int("credential_id", credentialID),
		log.Int("error_code", errorCode),
	)

	reloadCtx, cancel := xcontext.DetachWithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := svc.enabledChannelsCache.Load(reloadCtx, true); err != nil {
		log.Warn(ctx, "Failed to synchronously reload channels after upstream credential disable",
			log.Int("credential_id", credentialID),
			log.Cause(err),
		)
	}

	svc.asyncReloadChannels()

	return affected, nil
}

// EnableAPIKey 重新启用指定 key（从 disabled_api_keys 中移除）.
func (svc *ChannelService) EnableAPIKey(ctx context.Context, channelID int, key string) error {
	// 读取 channel
	ch, err := svc.entFromContext(ctx).Channel.Get(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}

	if len(ch.DisabledAPIKeys) == 0 {
		// 没有禁用的 key，忽略
		return nil
	}

	// 从 disabled_api_keys 中移除指定 key
	newDisabledKeys := make([]objects.DisabledAPIKey, 0, len(ch.DisabledAPIKeys))
	found := false

	for _, dk := range ch.DisabledAPIKeys {
		if dk.Key == key {
			found = true
			continue
		}

		newDisabledKeys = append(newDisabledKeys, dk)
	}

	if !found {
		// key 不在禁用列表中，忽略
		return nil
	}

	// 更新 channel
	update := svc.entFromContext(ctx).Channel.UpdateOneID(channelID).
		SetDisabledAPIKeys(newDisabledKeys)

	if _, err := update.Save(ctx); err != nil {
		return fmt.Errorf("failed to enable api key: %w", err)
	}

	svc.asyncReloadChannels()

	return nil
}

// EnableAllAPIKeys 清空 disabled_api_keys.
func (svc *ChannelService) EnableAllAPIKeys(ctx context.Context, channelID int) error {
	// 读取 channel
	ch, err := svc.entFromContext(ctx).Channel.Get(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}

	if len(ch.DisabledAPIKeys) == 0 {
		// 没有禁用的 key，忽略
		return nil
	}

	// 更新 channel，清空 disabled_api_keys
	update := svc.entFromContext(ctx).Channel.UpdateOneID(channelID).
		SetDisabledAPIKeys([]objects.DisabledAPIKey{})

	if _, err := update.Save(ctx); err != nil {
		return fmt.Errorf("failed to enable all api keys: %w", err)
	}

	log.Info(ctx, "All API keys enabled",
		log.Int("channel_id", channelID),
	)

	svc.asyncReloadChannels()

	return nil
}

// EnableSelectedAPIKeys re-enables multiple specific keys from disabled_api_keys.
func (svc *ChannelService) EnableSelectedAPIKeys(ctx context.Context, channelID int, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	ch, err := svc.entFromContext(ctx).Channel.Get(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}

	if len(ch.DisabledAPIKeys) == 0 {
		return nil
	}

	keysToEnable := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		keysToEnable[k] = struct{}{}
	}

	newDisabledKeys := make([]objects.DisabledAPIKey, 0, len(ch.DisabledAPIKeys))
	for _, dk := range ch.DisabledAPIKeys {
		if _, found := keysToEnable[dk.Key]; !found {
			newDisabledKeys = append(newDisabledKeys, dk)
		}
	}

	if len(newDisabledKeys) == len(ch.DisabledAPIKeys) {
		return nil
	}

	update := svc.entFromContext(ctx).Channel.UpdateOneID(channelID).
		SetDisabledAPIKeys(newDisabledKeys)

	if _, err := update.Save(ctx); err != nil {
		return fmt.Errorf("failed to enable selected api keys: %w", err)
	}

	log.Info(ctx, "Selected API keys enabled",
		log.Int("channel_id", channelID),
		log.Int("count", len(keys)),
	)

	svc.asyncReloadChannels()

	return nil
}

// DeleteDisabledAPIKeysResult is the result of deleting disabled API keys.
type DeleteDisabledAPIKeysResult struct {
	Success bool
	Message string
}

// DeleteDisabledAPIKeys removes disabled API keys from both disabled_api_keys list and credentials.
// It ensures at least one API key remains and prevents deletion for OAuth channels.
func (svc *ChannelService) DeleteDisabledAPIKeys(ctx context.Context, channelID int, keys []string) (*DeleteDisabledAPIKeysResult, error) {
	if len(keys) == 0 {
		return &DeleteDisabledAPIKeysResult{Success: true}, nil
	}

	ch, err := svc.entFromContext(ctx).Channel.Get(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel: %w", err)
	}

	// Check if channel uses OAuth - cannot delete keys for OAuth channels
	if ch.Credentials.IsOAuth() {
		return nil, fmt.Errorf("cannot delete API keys for OAuth channels")
	}

	keysToDelete := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		keysToDelete[k] = struct{}{}
	}

	// Remove from disabled_api_keys
	newDisabledKeys := make([]objects.DisabledAPIKey, 0, len(ch.DisabledAPIKeys))
	for _, dk := range ch.DisabledAPIKeys {
		if _, found := keysToDelete[dk.Key]; !found {
			newDisabledKeys = append(newDisabledKeys, dk)
		}
	}

	// Remove from credentials
	newCredentials := ch.Credentials
	if len(newCredentials.APIKeys) > 0 {
		filteredKeys := make([]string, 0, len(newCredentials.APIKeys))
		for _, k := range newCredentials.APIKeys {
			if _, found := keysToDelete[k]; !found {
				filteredKeys = append(filteredKeys, k)
			}
		}

		newCredentials.APIKeys = filteredKeys
	}

	if newCredentials.APIKey != "" {
		if _, found := keysToDelete[newCredentials.APIKey]; found {
			newCredentials.APIKey = ""
		}
	}

	// Ensure at least one API key remains
	allKeys := newCredentials.GetAllAPIKeys()
	if len(allKeys) == 0 {
		// Restore at least one key from the keys being deleted
		// Prefer the first key that was supposed to be deleted
		restoredKey := keys[0]
		newCredentials.APIKeys = []string{restoredKey}
	}

	update := svc.entFromContext(ctx).Channel.UpdateOneID(channelID).
		SetDisabledAPIKeys(newDisabledKeys).
		SetCredentials(newCredentials)

	if _, err := update.Save(ctx); err != nil {
		return nil, fmt.Errorf("failed to delete disabled api keys: %w", err)
	}

	log.Info(ctx, "Disabled API keys deleted",
		log.Int("channel_id", channelID),
		log.Int("count", len(keys)),
	)

	// Check if we had to preserve a key
	result := &DeleteDisabledAPIKeysResult{Success: true}
	if len(allKeys) == 0 {
		result.Message = "ONE_KEY_PRESERVED"
	}

	svc.asyncReloadChannels()

	return result, nil
}
