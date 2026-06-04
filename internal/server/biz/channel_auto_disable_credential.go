package biz

import (
	"context"
	"fmt"
	"time"

	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/pkg/xcontext"
)

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
		log.String("reason", reason),
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
