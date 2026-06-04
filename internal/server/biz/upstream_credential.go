package biz

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channelcredentialref"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
)

type UpstreamCredentialServiceParams struct {
	fx.In

	Ent            *ent.Client
	ChannelService *ChannelService
	SystemService  *SystemService
}

type UpstreamCredentialService struct {
	*AbstractService

	channelService *ChannelService
	systemService  *SystemService
}

func NewUpstreamCredentialService(params UpstreamCredentialServiceParams) *UpstreamCredentialService {
	return &UpstreamCredentialService{
		AbstractService: &AbstractService{db: params.Ent},
		channelService:  params.ChannelService,
		systemService:   params.SystemService,
	}
}

type CreateUpstreamCredentialInput struct {
	Name         *string
	ProviderType *string
	BaseURL      *string
	AuthKind     *upstreamcredential.AuthKind
	SecretKind   *upstreamcredential.SecretKind
	IssuerScope  *string
	Secret       objects.UpstreamCredentialSecret
	Status       *upstreamcredential.Status
	Weight       *int
	QuotaScopeID *objects.GUID
	Quota        *CreateCredentialQuotaScopeInput
	Remark       *string
}

type UpdateUpstreamCredentialInput struct {
	Name            *string
	Status          *upstreamcredential.Status
	Weight          *int
	QuotaScopeID    *objects.GUID
	ClearQuotaScope bool
	Quota           *UpdateCredentialQuotaScopeInput
	Remark          *string
}

type RotateUpstreamCredentialSecretInput struct {
	Secret objects.UpstreamCredentialSecret
}

type AttachCredentialToChannelInput struct {
	ChannelID      objects.GUID
	CredentialID   objects.GUID
	Enabled        *bool
	WeightOverride *int
}

type UpdateChannelCredentialRefInput struct {
	Enabled             *bool
	WeightOverride      *int
	ClearWeightOverride bool
}

type CreateCredentialQuotaScopeInput struct {
	Name                    *string
	Status                  *credentialquotascope.Status
	Unit                    *credentialquotascope.Unit
	LimitAmount             *string
	UsedAmount              *string
	WarningThresholdPercent *int
	ResetPolicy             *credentialquotascope.ResetPolicy
	ResetAt                 *time.Time
	WindowStartedAt         *time.Time
	OverLimitAction         *credentialquotascope.OverLimitAction
	PauseUntil              *time.Time
	Source                  *credentialquotascope.Source
	LastError               *string
	Remark                  *string
}

type UpdateCredentialQuotaScopeInput struct {
	Name                    *string
	Status                  *credentialquotascope.Status
	Unit                    *credentialquotascope.Unit
	LimitAmount             *string
	UsedAmount              *string
	WarningThresholdPercent *int
	ClearWarningThreshold   bool
	ResetPolicy             *credentialquotascope.ResetPolicy
	ResetAt                 *time.Time
	ClearResetAt            bool
	WindowStartedAt         *time.Time
	ClearWindowStartedAt    bool
	OverLimitAction         *credentialquotascope.OverLimitAction
	PauseUntil              *time.Time
	ClearPauseUntil         bool
	Source                  *credentialquotascope.Source
	LastError               *string
	Remark                  *string
}

type BackfillCredentialSecretFingerprintsPayload struct {
	ScannedCredentials  int
	UpdatedCredentials  int
	MergedCredentials   int
	SkippedCredentials  int
	MigratedRefs        int
	DisabledSourceRefs  int
	ArchivedCredentials int
}

type credentialRefMigrationPayload struct {
	MigratedRefs       int
	DisabledSourceRefs int
}

func (svc *UpstreamCredentialService) CreateUpstreamCredential(ctx context.Context, input CreateUpstreamCredentialInput) (*ent.UpstreamCredential, error) {
	secretKind := resolveCredentialSecretKind(input.SecretKind, input.AuthKind, input.Secret)
	issuerScope := resolveCredentialIssuerScope(input.IssuerScope, input.ProviderType, input.BaseURL, input.Secret)
	fingerprint := CredentialFingerprintForSecret(issuerScope, secretKind.String(), input.Secret)
	secretFingerprint := CredentialSecretFingerprintForSecret(secretKind.String(), input.Secret)
	if fingerprint == "" {
		return nil, fmt.Errorf("credential secret is empty or unsupported")
	}

	existing, err := svc.entFromContext(ctx).UpstreamCredential.Query().
		Where(upstreamcredential.Or(
			upstreamcredential.SecretFingerprintEQ(secretFingerprint),
			upstreamcredential.Fingerprint(fingerprint),
		)).
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check credential fingerprint: %w", err)
	}
	if existing != nil {
		if existing.Status == upstreamcredential.StatusArchived {
			return svc.reactivateArchivedCredential(ctx, existing.ID, input)
		}
		return existing, nil
	}

	var credential *ent.UpstreamCredential
	if err := svc.RunInTransaction(ctx, func(ctx context.Context) error {
		resetConfig := svc.credentialQuotaResetConfig(ctx)
		create := svc.entFromContext(ctx).UpstreamCredential.Create().
			SetProviderType(strings.TrimSpace(stringValuePtr(input.ProviderType))).
			SetBaseURL(normalizeCredentialBaseURL(stringValuePtr(input.BaseURL))).
			SetAuthKind(authKindFromSecretKind(secretKind)).
			SetSecretKind(secretKind).
			SetIssuerScope(issuerScope).
			SetKeyHint(CredentialKeyHintForSecret(secretKind.String(), input.Secret)).
			SetSecretPayload(input.Secret).
			SetFingerprint(fingerprint).
			SetSecretFingerprint(secretFingerprint)

		if input.Name != nil {
			create.SetName(strings.TrimSpace(*input.Name))
		}
		if input.Status != nil {
			create.SetStatus(*input.Status)
		}
		if input.Weight != nil {
			create.SetWeight(normalizeCredentialWeight(*input.Weight))
		}
		if input.QuotaScopeID != nil {
			create.SetQuotaScopeID(input.QuotaScopeID.ID)
		}
		if input.Quota != nil {
			quotaInput, err := normalizeCreateCredentialQuotaScopeInput(*input.Quota, time.Now(), resetConfig)
			if err != nil {
				return err
			}
			scopeCreate := svc.entFromContext(ctx).CredentialQuotaScope.Create()
			applyCreateCredentialQuotaScopeInput(scopeCreate, quotaInput)
			scope, err := scopeCreate.Save(ctx)
			if err != nil {
				return fmt.Errorf("failed to create credential quota scope: %w", err)
			}
			create.SetQuotaScopeID(scope.ID)
		}
		if input.Remark != nil {
			create.SetRemark(strings.TrimSpace(*input.Remark))
		}

		var err error
		credential, err = create.Save(ctx)
		if err != nil {
			return fmt.Errorf("failed to create upstream credential: %w", err)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	svc.reloadChannels()

	return credential, nil
}

func (svc *UpstreamCredentialService) reactivateArchivedCredential(ctx context.Context, id int, input CreateUpstreamCredentialInput) (*ent.UpstreamCredential, error) {
	status := upstreamcredential.StatusEnabled
	if input.Status != nil {
		status = *input.Status
	}

	var credential *ent.UpstreamCredential
	if err := svc.RunInTransaction(ctx, func(ctx context.Context) error {
		client := svc.entFromContext(ctx)
		resetConfig := svc.credentialQuotaResetConfig(ctx)
		current, err := client.UpstreamCredential.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to get archived upstream credential: %w", err)
		}

		update := client.UpstreamCredential.UpdateOneID(id).
			SetStatus(status)

		if input.Name != nil {
			update.SetName(strings.TrimSpace(*input.Name))
		}
		if input.Weight != nil {
			update.SetWeight(normalizeCredentialWeight(*input.Weight))
		}
		if input.QuotaScopeID != nil {
			update.SetQuotaScopeID(input.QuotaScopeID.ID)
		}
		if input.Quota != nil {
			if current.QuotaScopeID != nil && *current.QuotaScopeID > 0 {
				quotaInput := UpdateCredentialQuotaScopeInput{
					Name:                    input.Quota.Name,
					Status:                  input.Quota.Status,
					Unit:                    input.Quota.Unit,
					LimitAmount:             input.Quota.LimitAmount,
					UsedAmount:              input.Quota.UsedAmount,
					WarningThresholdPercent: input.Quota.WarningThresholdPercent,
					ResetPolicy:             input.Quota.ResetPolicy,
					ResetAt:                 input.Quota.ResetAt,
					WindowStartedAt:         input.Quota.WindowStartedAt,
					OverLimitAction:         input.Quota.OverLimitAction,
					PauseUntil:              input.Quota.PauseUntil,
					Source:                  input.Quota.Source,
					LastError:               input.Quota.LastError,
					Remark:                  input.Quota.Remark,
				}
				scope, err := client.CredentialQuotaScope.Get(ctx, *current.QuotaScopeID)
				if err != nil {
					return fmt.Errorf("failed to get credential quota scope: %w", err)
				}
				normalized, err := normalizeUpdateCredentialQuotaScopeInput(scope, quotaInput, time.Now(), resetConfig)
				if err != nil {
					return err
				}
				scopeUpdate := client.CredentialQuotaScope.UpdateOneID(*current.QuotaScopeID)
				applyUpdateCredentialQuotaScopeInput(scopeUpdate, normalized)
				if _, err := scopeUpdate.Save(ctx); err != nil {
					return fmt.Errorf("failed to update credential quota scope: %w", err)
				}
			} else {
				quotaInput, err := normalizeCreateCredentialQuotaScopeInput(*input.Quota, time.Now(), resetConfig)
				if err != nil {
					return err
				}
				scopeCreate := client.CredentialQuotaScope.Create()
				applyCreateCredentialQuotaScopeInput(scopeCreate, quotaInput)
				scope, err := scopeCreate.Save(ctx)
				if err != nil {
					return fmt.Errorf("failed to create credential quota scope: %w", err)
				}
				update.SetQuotaScopeID(scope.ID)
			}
		}
		if input.Remark != nil {
			update.SetRemark(strings.TrimSpace(*input.Remark))
		}

		credential, err = update.Save(ctx)
		if err != nil {
			return fmt.Errorf("failed to reactivate archived upstream credential: %w", err)
		}

		if status == upstreamcredential.StatusEnabled {
			if err := svc.restoreCredentialRefs(ctx, id); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	svc.reloadChannels()

	return credential, nil
}

func (svc *UpstreamCredentialService) UpdateUpstreamCredential(ctx context.Context, id int, input UpdateUpstreamCredentialInput) (*ent.UpstreamCredential, error) {
	var credential *ent.UpstreamCredential
	if err := svc.RunInTransaction(ctx, func(ctx context.Context) error {
		client := svc.entFromContext(ctx)
		resetConfig := svc.credentialQuotaResetConfig(ctx)
		current, err := client.UpstreamCredential.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to get upstream credential: %w", err)
		}

		update := client.UpstreamCredential.UpdateOneID(id)

		if input.Name != nil {
			update.SetName(strings.TrimSpace(*input.Name))
		}
		if input.Status != nil {
			update.SetStatus(*input.Status)
		}
		if input.Weight != nil {
			update.SetWeight(normalizeCredentialWeight(*input.Weight))
		}
		if input.ClearQuotaScope {
			update.ClearQuotaScopeID()
		} else if input.QuotaScopeID != nil {
			update.SetQuotaScopeID(input.QuotaScopeID.ID)
		} else if input.Quota != nil {
			if current.QuotaScopeID != nil && *current.QuotaScopeID > 0 {
				scope, err := client.CredentialQuotaScope.Get(ctx, *current.QuotaScopeID)
				if err != nil {
					return fmt.Errorf("failed to get credential quota scope: %w", err)
				}
				quotaInput, err := normalizeUpdateCredentialQuotaScopeInput(scope, *input.Quota, time.Now(), resetConfig)
				if err != nil {
					return err
				}
				scopeUpdate := client.CredentialQuotaScope.UpdateOneID(*current.QuotaScopeID)
				applyUpdateCredentialQuotaScopeInput(scopeUpdate, quotaInput)
				if _, err := scopeUpdate.Save(ctx); err != nil {
					return fmt.Errorf("failed to update credential quota scope: %w", err)
				}
			} else {
				quotaInput, err := updateCredentialQuotaScopeCreateInput(*input.Quota, time.Now(), resetConfig)
				if err != nil {
					return err
				}
				scopeCreate := client.CredentialQuotaScope.Create()
				applyCreateCredentialQuotaScopeInput(scopeCreate, quotaInput)
				scope, err := scopeCreate.Save(ctx)
				if err != nil {
					return fmt.Errorf("failed to create credential quota scope: %w", err)
				}
				update.SetQuotaScopeID(scope.ID)
			}
		}
		if input.Remark != nil {
			update.SetRemark(strings.TrimSpace(*input.Remark))
		}

		credential, err = update.Save(ctx)
		if err != nil {
			return fmt.Errorf("failed to update upstream credential: %w", err)
		}

		if input.Status != nil && current.Status == upstreamcredential.StatusArchived && *input.Status == upstreamcredential.StatusEnabled {
			if err := svc.restoreCredentialRefs(ctx, id); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	svc.reloadChannels()

	return credential, nil
}

func (svc *UpstreamCredentialService) ArchiveUpstreamCredential(ctx context.Context, id int) (*ent.UpstreamCredential, error) {
	var credential *ent.UpstreamCredential
	if err := svc.RunInTransaction(ctx, func(ctx context.Context) error {
		client := svc.entFromContext(ctx)

		var err error
		credential, err = client.UpstreamCredential.UpdateOneID(id).
			SetStatus(upstreamcredential.StatusArchived).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("failed to archive upstream credential: %w", err)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	svc.reloadChannels()

	return credential, nil
}

func (svc *UpstreamCredentialService) DeleteUpstreamCredential(ctx context.Context, id int) (bool, error) {
	if err := svc.RunInTransaction(ctx, func(ctx context.Context) error {
		client := svc.entFromContext(ctx)
		if _, err := client.ChannelCredentialRef.Delete().
			Where(channelcredentialref.CredentialID(id)).
			Exec(ctx); err != nil {
			return fmt.Errorf("failed to delete credential channel refs: %w", err)
		}

		if err := client.UpstreamCredential.DeleteOneID(id).Exec(ctx); err != nil {
			return fmt.Errorf("failed to delete upstream credential: %w", err)
		}

		return nil
	}); err != nil {
		return false, err
	}

	svc.reloadChannels()

	return true, nil
}

func (svc *UpstreamCredentialService) restoreCredentialRefs(ctx context.Context, credentialID int) error {
	if _, err := svc.entFromContext(ctx).ChannelCredentialRef.Update().
		Where(channelcredentialref.CredentialID(credentialID)).
		SetEnabled(true).
		Save(ctx); err != nil {
		return fmt.Errorf("failed to restore credential refs: %w", err)
	}

	return nil
}

func (svc *UpstreamCredentialService) RotateUpstreamCredentialSecret(ctx context.Context, id int, input RotateUpstreamCredentialSecretInput) (*ent.UpstreamCredential, error) {
	var credential *ent.UpstreamCredential
	if err := svc.RunInTransaction(ctx, func(ctx context.Context) error {
		client := svc.entFromContext(ctx)
		existing, err := client.UpstreamCredential.Query().
			Where(upstreamcredential.ID(id)).
			WithChannelRefs().
			Only(ctx)
		if err != nil {
			return fmt.Errorf("failed to get upstream credential: %w", err)
		}

		secretKind := existing.SecretKind
		issuerScope := existing.IssuerScope
		if strings.TrimSpace(issuerScope) == "" {
			issuerScope = CredentialIssuerScope(existing.ProviderType, existing.BaseURL)
		}
		fingerprint := CredentialFingerprintForSecret(issuerScope, secretKind.String(), input.Secret)
		secretFingerprint := CredentialSecretFingerprintForSecret(secretKind.String(), input.Secret)
		if fingerprint == "" {
			return fmt.Errorf("credential secret is empty or unsupported")
		}

		if credentialMatchesSecret(existing, fingerprint, secretFingerprint) {
			credential, err = client.UpstreamCredential.UpdateOneID(id).
				SetSecretPayload(input.Secret).
				SetFingerprint(fingerprint).
				SetSecretFingerprint(secretFingerprint).
				SetSecretKind(secretKind).
				SetIssuerScope(issuerScope).
				SetKeyHint(CredentialKeyHintForSecret(secretKind.String(), input.Secret)).
				Save(ctx)
			if err != nil {
				return fmt.Errorf("failed to rotate upstream credential secret: %w", err)
			}
			return nil
		}

		target, err := client.UpstreamCredential.Query().
			Where(
				upstreamcredential.Or(
					upstreamcredential.SecretFingerprintEQ(secretFingerprint),
					upstreamcredential.Fingerprint(fingerprint),
				),
				upstreamcredential.IDNEQ(id),
			).
			First(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return fmt.Errorf("failed to check credential fingerprint: %w", err)
		}
		if ent.IsNotFound(err) {
			create := client.UpstreamCredential.Create().
				SetName(existing.Name).
				SetProviderType(existing.ProviderType).
				SetBaseURL(existing.BaseURL).
				SetAuthKind(existing.AuthKind).
				SetSecretKind(secretKind).
				SetIssuerScope(issuerScope).
				SetKeyHint(CredentialKeyHintForSecret(secretKind.String(), input.Secret)).
				SetSecretPayload(input.Secret).
				SetFingerprint(fingerprint).
				SetSecretFingerprint(secretFingerprint).
				SetStatus(existing.Status).
				SetWeight(normalizeCredentialWeight(existing.Weight)).
				SetQuotaStatus(existing.QuotaStatus).
				SetLastError(existing.LastError).
				SetRemark(existing.Remark)
			if existing.QuotaScopeID != nil {
				create.SetQuotaScopeID(*existing.QuotaScopeID)
			}

			target, err = create.Save(ctx)
			if err != nil {
				return fmt.Errorf("failed to create replacement upstream credential: %w", err)
			}
		}

		if _, err := svc.migrateCredentialRefsToTarget(ctx, existing, target); err != nil {
			return err
		}

		if existing.Status != upstreamcredential.StatusArchived {
			if _, err := client.UpstreamCredential.UpdateOneID(existing.ID).
				SetStatus(upstreamcredential.StatusArchived).
				Save(ctx); err != nil {
				return fmt.Errorf("failed to archive replaced upstream credential: %w", err)
			}
		}

		credential, err = client.UpstreamCredential.Query().
			Where(upstreamcredential.ID(target.ID)).
			WithChannelRefs().
			WithQuotaScope().
			Only(ctx)
		if err != nil {
			return fmt.Errorf("failed to load replacement upstream credential: %w", err)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	svc.reloadChannels()

	return credential, nil
}

func credentialMatchesSecret(credential *ent.UpstreamCredential, fingerprint string, secretFingerprint string) bool {
	if credential == nil {
		return false
	}
	if strings.TrimSpace(secretFingerprint) != "" && credential.SecretFingerprint != nil && *credential.SecretFingerprint == secretFingerprint {
		return true
	}

	return strings.TrimSpace(fingerprint) != "" && credential.Fingerprint == fingerprint
}

func (svc *UpstreamCredentialService) migrateCredentialRefsToTarget(ctx context.Context, source *ent.UpstreamCredential, target *ent.UpstreamCredential) (*credentialRefMigrationPayload, error) {
	payload := &credentialRefMigrationPayload{}
	if source == nil || target == nil || source.ID == target.ID {
		return payload, nil
	}

	client := svc.entFromContext(ctx)
	for _, ref := range source.Edges.ChannelRefs {
		if ref == nil {
			continue
		}

		existingTargetRef, err := client.ChannelCredentialRef.Query().
			Where(
				channelcredentialref.ChannelID(ref.ChannelID),
				channelcredentialref.CredentialID(target.ID),
			).
			First(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return payload, fmt.Errorf("failed to check replacement channel credential ref: %w", err)
		}

		if existingTargetRef != nil {
			update := client.ChannelCredentialRef.UpdateOneID(existingTargetRef.ID).
				SetEnabled(existingTargetRef.Enabled || ref.Enabled)
			if ref.WeightOverride != nil && existingTargetRef.WeightOverride == nil {
				update.SetWeightOverride(*ref.WeightOverride)
			}
			if _, err := update.Save(ctx); err != nil {
				return payload, fmt.Errorf("failed to update replacement channel credential ref: %w", err)
			}
			payload.MigratedRefs++
		} else {
			create := client.ChannelCredentialRef.Create().
				SetChannelID(ref.ChannelID).
				SetCredentialID(target.ID).
				SetEnabled(ref.Enabled)
			if ref.WeightOverride != nil {
				create.SetWeightOverride(*ref.WeightOverride)
			}
			if _, err := create.Save(ctx); err != nil {
				return payload, fmt.Errorf("failed to create replacement channel credential ref: %w", err)
			}
			payload.MigratedRefs++
		}

		if ref.Enabled {
			if _, err := client.ChannelCredentialRef.UpdateOneID(ref.ID).
				SetEnabled(false).
				Save(ctx); err != nil {
				return payload, fmt.Errorf("failed to disable replaced channel credential ref: %w", err)
			}
			payload.DisabledSourceRefs++
		}
	}

	return payload, nil
}

func (svc *UpstreamCredentialService) CreateCredentialQuotaScope(ctx context.Context, input CreateCredentialQuotaScopeInput) (*ent.CredentialQuotaScope, error) {
	quotaInput, err := normalizeCreateCredentialQuotaScopeInput(input, time.Now(), svc.credentialQuotaResetConfig(ctx))
	if err != nil {
		return nil, err
	}

	create := svc.entFromContext(ctx).CredentialQuotaScope.Create()
	applyCreateCredentialQuotaScopeInput(create, quotaInput)

	scope, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create credential quota scope: %w", err)
	}

	svc.reloadChannels()

	return scope, nil
}

func (svc *UpstreamCredentialService) UpdateCredentialQuotaScope(ctx context.Context, id int, input UpdateCredentialQuotaScopeInput) (*ent.CredentialQuotaScope, error) {
	client := svc.entFromContext(ctx)
	current, err := client.CredentialQuotaScope.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get credential quota scope: %w", err)
	}

	quotaInput, err := normalizeUpdateCredentialQuotaScopeInput(current, input, time.Now(), svc.credentialQuotaResetConfig(ctx))
	if err != nil {
		return nil, err
	}

	update := svc.entFromContext(ctx).CredentialQuotaScope.UpdateOneID(id)
	applyUpdateCredentialQuotaScopeInput(update, quotaInput)

	scope, err := update.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update credential quota scope: %w", err)
	}

	svc.reloadChannels()

	return scope, nil
}

type credentialQuotaResetConfig struct {
	Location       *time.Location
	DailyResetTime string
}

func (svc *UpstreamCredentialService) credentialQuotaResetConfig(ctx context.Context) credentialQuotaResetConfig {
	return credentialQuotaResetConfigFromSystem(ctx, svc.systemService)
}

func credentialQuotaResetConfigFromSystem(ctx context.Context, systemService *SystemService) credentialQuotaResetConfig {
	cfg := credentialQuotaResetConfig{
		Location:       time.UTC,
		DailyResetTime: defaultGeneralSettings.CredentialQuotaDailyResetTime,
	}
	if systemService == nil {
		return cfg
	}

	settings, err := systemService.GeneralSettings(ctx)
	if err != nil {
		log.Warn(ctx, "failed to load system general settings for credential quota reset", log.Cause(err))
		return cfg
	}
	if settings == nil {
		return cfg
	}

	if settings.Timezone != "" {
		if loc, err := time.LoadLocation(settings.Timezone); err == nil {
			cfg.Location = loc
		}
	}
	if strings.TrimSpace(settings.CredentialQuotaDailyResetTime) != "" {
		cfg.DailyResetTime = strings.TrimSpace(settings.CredentialQuotaDailyResetTime)
	}

	return cfg
}

func normalizeCreateCredentialQuotaScopeInput(
	input CreateCredentialQuotaScopeInput,
	now time.Time,
	resetConfig credentialQuotaResetConfig,
) (CreateCredentialQuotaScopeInput, error) {
	policy := credentialquotascope.ResetPolicyNone
	if input.ResetPolicy != nil {
		policy = *input.ResetPolicy
	}

	switch policy {
	case credentialquotascope.ResetPolicyDaily:
		if input.ResetAt == nil {
			resetAt, err := nextDailyCredentialQuotaResetAt(now, resetConfig)
			if err != nil {
				return input, err
			}
			input.ResetAt = &resetAt
		}
		if input.WindowStartedAt == nil {
			windowStartedAt := now.UTC()
			input.WindowStartedAt = &windowStartedAt
		}
	case credentialquotascope.ResetPolicyMonthly:
		if input.ResetAt == nil {
			resetAt := nextMonthlyCredentialQuotaResetAt(now, resetConfig)
			input.ResetAt = &resetAt
		}
		if input.WindowStartedAt == nil {
			windowStartedAt := now.UTC()
			input.WindowStartedAt = &windowStartedAt
		}
	case credentialquotascope.ResetPolicyCustom:
		if input.WindowStartedAt == nil || input.ResetAt == nil {
			return input, fmt.Errorf("custom credential quota reset policy requires windowStartedAt and resetAt")
		}
		if !input.ResetAt.After(*input.WindowStartedAt) {
			return input, fmt.Errorf("custom credential quota resetAt must be after windowStartedAt")
		}
	}

	return input, nil
}

func normalizeUpdateCredentialQuotaScopeInput(
	scope *ent.CredentialQuotaScope,
	input UpdateCredentialQuotaScopeInput,
	now time.Time,
	resetConfig credentialQuotaResetConfig,
) (UpdateCredentialQuotaScopeInput, error) {
	if scope == nil {
		return input, fmt.Errorf("credential quota scope is required")
	}

	policy := scope.ResetPolicy
	if input.ResetPolicy != nil {
		policy = *input.ResetPolicy
	}

	var resetAt *time.Time
	switch {
	case input.ClearResetAt:
		resetAt = nil
	case input.ResetAt != nil:
		resetAt = input.ResetAt
	default:
		resetAt = scope.ResetAt
	}

	var windowStartedAt *time.Time
	switch {
	case input.ClearWindowStartedAt:
		windowStartedAt = nil
	case input.WindowStartedAt != nil:
		windowStartedAt = input.WindowStartedAt
	default:
		windowStartedAt = scope.WindowStartedAt
	}

	switch policy {
	case credentialquotascope.ResetPolicyDaily:
		if resetAt == nil {
			next, err := nextDailyCredentialQuotaResetAt(now, resetConfig)
			if err != nil {
				return input, err
			}
			input.ResetAt = &next
			input.ClearResetAt = false
			resetAt = &next
		}
		if windowStartedAt == nil {
			startedAt := now.UTC()
			input.WindowStartedAt = &startedAt
			input.ClearWindowStartedAt = false
		}
	case credentialquotascope.ResetPolicyMonthly:
		if resetAt == nil {
			next := nextMonthlyCredentialQuotaResetAt(now, resetConfig)
			input.ResetAt = &next
			input.ClearResetAt = false
			resetAt = &next
		}
		if windowStartedAt == nil {
			startedAt := now.UTC()
			input.WindowStartedAt = &startedAt
			input.ClearWindowStartedAt = false
		}
	case credentialquotascope.ResetPolicyCustom:
		if windowStartedAt == nil || resetAt == nil {
			return input, fmt.Errorf("custom credential quota reset policy requires windowStartedAt and resetAt")
		}
		if !resetAt.After(*windowStartedAt) {
			return input, fmt.Errorf("custom credential quota resetAt must be after windowStartedAt")
		}
	}

	return input, nil
}

func updateCredentialQuotaScopeCreateInput(
	input UpdateCredentialQuotaScopeInput,
	now time.Time,
	resetConfig credentialQuotaResetConfig,
) (CreateCredentialQuotaScopeInput, error) {
	createInput := CreateCredentialQuotaScopeInput{
		Name:                    input.Name,
		Status:                  input.Status,
		Unit:                    input.Unit,
		LimitAmount:             input.LimitAmount,
		UsedAmount:              input.UsedAmount,
		WarningThresholdPercent: input.WarningThresholdPercent,
		ResetPolicy:             input.ResetPolicy,
		ResetAt:                 input.ResetAt,
		WindowStartedAt:         input.WindowStartedAt,
		OverLimitAction:         input.OverLimitAction,
		PauseUntil:              input.PauseUntil,
		Source:                  input.Source,
		LastError:               input.LastError,
		Remark:                  input.Remark,
	}

	return normalizeCreateCredentialQuotaScopeInput(createInput, now, resetConfig)
}

func nextDailyCredentialQuotaResetAt(now time.Time, resetConfig credentialQuotaResetConfig) (time.Time, error) {
	loc := resetConfig.Location
	if loc == nil {
		loc = time.UTC
	}
	hour, minute, err := parseCredentialQuotaDailyResetTime(resetConfig.DailyResetTime)
	if err != nil {
		return time.Time{}, err
	}
	localNow := now.In(loc)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
	if !next.After(localNow) {
		next = next.AddDate(0, 0, 1)
	}
	return next.UTC(), nil
}

func nextMonthlyCredentialQuotaResetAt(now time.Time, resetConfig credentialQuotaResetConfig) time.Time {
	loc := resetConfig.Location
	if loc == nil {
		loc = time.UTC
	}
	localNow := now.In(loc)
	return time.Date(localNow.Year(), localNow.Month()+1, 1, 0, 0, 0, 0, localNow.Location()).UTC()
}

func applyCreateCredentialQuotaScopeInput(create *ent.CredentialQuotaScopeCreate, input CreateCredentialQuotaScopeInput) {
	if input.Name != nil {
		create.SetName(strings.TrimSpace(*input.Name))
	}
	if input.Status != nil {
		create.SetStatus(*input.Status)
	} else {
		create.SetStatus(credentialquotascope.StatusAvailable)
	}
	if input.Unit != nil {
		create.SetUnit(*input.Unit)
	}
	if input.LimitAmount != nil {
		create.SetLimitAmount(strings.TrimSpace(*input.LimitAmount))
	}
	if input.UsedAmount != nil {
		create.SetUsedAmount(strings.TrimSpace(*input.UsedAmount))
	}
	if input.WarningThresholdPercent != nil {
		create.SetWarningThresholdPercent(*input.WarningThresholdPercent)
	}
	if input.ResetPolicy != nil {
		create.SetResetPolicy(*input.ResetPolicy)
	}
	if input.ResetAt != nil {
		create.SetResetAt(*input.ResetAt)
	}
	if input.WindowStartedAt != nil {
		create.SetWindowStartedAt(*input.WindowStartedAt)
	}
	if input.OverLimitAction != nil {
		create.SetOverLimitAction(*input.OverLimitAction)
	}
	if input.PauseUntil != nil {
		create.SetPauseUntil(*input.PauseUntil)
	}
	if input.Source != nil {
		create.SetSource(*input.Source)
	} else {
		create.SetSource(credentialquotascope.SourceLocalBudget)
	}
	if input.LastError != nil {
		create.SetLastError(strings.TrimSpace(*input.LastError))
	}
	if input.Remark != nil {
		create.SetRemark(strings.TrimSpace(*input.Remark))
	}
}

func applyUpdateCredentialQuotaScopeCreateInput(create *ent.CredentialQuotaScopeCreate, input UpdateCredentialQuotaScopeInput) {
	applyCreateCredentialQuotaScopeInput(create, CreateCredentialQuotaScopeInput{
		Name:                    input.Name,
		Status:                  input.Status,
		Unit:                    input.Unit,
		LimitAmount:             input.LimitAmount,
		UsedAmount:              input.UsedAmount,
		WarningThresholdPercent: input.WarningThresholdPercent,
		ResetPolicy:             input.ResetPolicy,
		ResetAt:                 input.ResetAt,
		WindowStartedAt:         input.WindowStartedAt,
		OverLimitAction:         input.OverLimitAction,
		PauseUntil:              input.PauseUntil,
		Source:                  input.Source,
		LastError:               input.LastError,
		Remark:                  input.Remark,
	})
}

func applyUpdateCredentialQuotaScopeInput(update *ent.CredentialQuotaScopeUpdateOne, input UpdateCredentialQuotaScopeInput) {
	if input.Name != nil {
		update.SetName(strings.TrimSpace(*input.Name))
	}
	if input.Status != nil {
		update.SetStatus(*input.Status)
	}
	if input.Unit != nil {
		update.SetUnit(*input.Unit)
	}
	if input.LimitAmount != nil {
		update.SetLimitAmount(strings.TrimSpace(*input.LimitAmount))
	}
	if input.UsedAmount != nil {
		update.SetUsedAmount(strings.TrimSpace(*input.UsedAmount))
	}
	if input.ClearWarningThreshold {
		update.ClearWarningThresholdPercent()
	} else if input.WarningThresholdPercent != nil {
		update.SetWarningThresholdPercent(*input.WarningThresholdPercent)
	}
	if input.ResetPolicy != nil {
		update.SetResetPolicy(*input.ResetPolicy)
	}
	if input.ClearResetAt {
		update.ClearResetAt()
	} else if input.ResetAt != nil {
		update.SetResetAt(*input.ResetAt)
	}
	if input.ClearWindowStartedAt {
		update.ClearWindowStartedAt()
	} else if input.WindowStartedAt != nil {
		update.SetWindowStartedAt(*input.WindowStartedAt)
	}
	if input.OverLimitAction != nil {
		update.SetOverLimitAction(*input.OverLimitAction)
	}
	if input.ClearPauseUntil {
		update.ClearPauseUntil()
	} else if input.PauseUntil != nil {
		update.SetPauseUntil(*input.PauseUntil)
	}
	if input.Source != nil {
		update.SetSource(*input.Source)
	}
	if input.LastError != nil {
		update.SetLastError(strings.TrimSpace(*input.LastError))
	}
	if input.Remark != nil {
		update.SetRemark(strings.TrimSpace(*input.Remark))
	}
}

func (svc *UpstreamCredentialService) AttachCredentialToChannel(ctx context.Context, input AttachCredentialToChannelInput) (*ent.ChannelCredentialRef, error) {
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	existing, err := svc.entFromContext(ctx).ChannelCredentialRef.Query().
		Where(
			channelcredentialref.ChannelID(input.ChannelID.ID),
			channelcredentialref.CredentialID(input.CredentialID.ID),
		).
		WithCredential().
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check channel credential ref: %w", err)
	}
	if existing != nil {
		update := svc.entFromContext(ctx).ChannelCredentialRef.UpdateOneID(existing.ID).
			SetEnabled(enabled)
		if input.WeightOverride != nil {
			update.SetWeightOverride(normalizeCredentialWeight(*input.WeightOverride))
		}

		ref, err := update.Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to update channel credential ref: %w", err)
		}
		svc.reloadChannels()
		return ref, nil
	}

	create := svc.entFromContext(ctx).ChannelCredentialRef.Create().
		SetChannelID(input.ChannelID.ID).
		SetCredentialID(input.CredentialID.ID).
		SetEnabled(enabled)
	if input.WeightOverride != nil {
		create.SetWeightOverride(normalizeCredentialWeight(*input.WeightOverride))
	}

	ref, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to attach upstream credential to channel: %w", err)
	}

	svc.reloadChannels()

	return ref, nil
}

func (svc *UpstreamCredentialService) UpdateChannelCredentialRef(ctx context.Context, id int, input UpdateChannelCredentialRefInput) (*ent.ChannelCredentialRef, error) {
	update := svc.entFromContext(ctx).ChannelCredentialRef.UpdateOneID(id)

	if input.Enabled != nil {
		update.SetEnabled(*input.Enabled)
	}
	if input.ClearWeightOverride {
		update.ClearWeightOverride()
	} else if input.WeightOverride != nil {
		update.SetWeightOverride(normalizeCredentialWeight(*input.WeightOverride))
	}

	ref, err := update.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update channel credential ref: %w", err)
	}

	svc.reloadChannels()

	return ref, nil
}

func (svc *UpstreamCredentialService) DetachCredentialFromChannel(ctx context.Context, channelID int, credentialID int) (bool, error) {
	_, err := svc.entFromContext(ctx).ChannelCredentialRef.Delete().
		Where(
			channelcredentialref.ChannelID(channelID),
			channelcredentialref.CredentialID(credentialID),
		).
		Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to detach upstream credential from channel: %w", err)
	}

	svc.reloadChannels()

	return true, nil
}

func (svc *UpstreamCredentialService) RunStartupMigration(ctx context.Context) {
	if svc == nil {
		return
	}

	ctx = ent.NewContext(ctx, svc.db)
	ctx = authz.WithSystemBypass(ctx, "upstream-credential-startup-migration")

	backfillPayload, err := svc.BackfillCredentialSecretFingerprints(ctx)
	if err != nil {
		log.Warn(ctx, "failed to backfill upstream credential secret fingerprints",
			log.Cause(err),
		)
	} else if backfillPayload != nil && (backfillPayload.UpdatedCredentials > 0 || backfillPayload.MergedCredentials > 0 || backfillPayload.MigratedRefs > 0 || backfillPayload.ArchivedCredentials > 0) {
		log.Info(ctx, "backfilled upstream credential secret fingerprints",
			log.Int("scanned_credentials", backfillPayload.ScannedCredentials),
			log.Int("updated_credentials", backfillPayload.UpdatedCredentials),
			log.Int("merged_credentials", backfillPayload.MergedCredentials),
			log.Int("migrated_refs", backfillPayload.MigratedRefs),
			log.Int("disabled_source_refs", backfillPayload.DisabledSourceRefs),
			log.Int("archived_credentials", backfillPayload.ArchivedCredentials),
		)
	}

}

func (svc *UpstreamCredentialService) BackfillCredentialSecretFingerprints(ctx context.Context) (*BackfillCredentialSecretFingerprintsPayload, error) {
	credentials, err := svc.entFromContext(ctx).UpstreamCredential.Query().
		WithChannelRefs().
		Order(ent.Asc(upstreamcredential.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query upstream credentials: %w", err)
	}

	payload := &BackfillCredentialSecretFingerprintsPayload{ScannedCredentials: len(credentials)}
	targetsBySecretFingerprint := make(map[string]*ent.UpstreamCredential, len(credentials))
	archivedBySecretFingerprint := make(map[string]*ent.UpstreamCredential)

	for _, credential := range credentials {
		if credential == nil {
			continue
		}

		secretFingerprint := strings.TrimSpace(stringValuePtr(credential.SecretFingerprint))
		if secretFingerprint == "" {
			continue
		}

		if credential.Status == upstreamcredential.StatusArchived {
			if _, ok := archivedBySecretFingerprint[secretFingerprint]; !ok {
				archivedBySecretFingerprint[secretFingerprint] = credential
			}
			continue
		}

		if _, ok := targetsBySecretFingerprint[secretFingerprint]; !ok {
			targetsBySecretFingerprint[secretFingerprint] = credential
		}
	}

	for _, credential := range credentials {
		if credential == nil {
			payload.SkippedCredentials++
			continue
		}
		if strings.TrimSpace(stringValuePtr(credential.SecretFingerprint)) != "" {
			continue
		}
		if credential.Status == upstreamcredential.StatusArchived {
			payload.SkippedCredentials++
			continue
		}

		secretFingerprint := CredentialSecretFingerprintForSecret(credential.SecretKind.String(), credential.SecretPayload)
		if strings.TrimSpace(secretFingerprint) == "" {
			payload.SkippedCredentials++
			continue
		}

		if target := targetsBySecretFingerprint[secretFingerprint]; target != nil && target.ID != credential.ID {
			refPayload, err := svc.migrateCredentialRefsToTarget(ctx, credential, target)
			if err != nil {
				return payload, err
			}
			payload.MigratedRefs += refPayload.MigratedRefs
			payload.DisabledSourceRefs += refPayload.DisabledSourceRefs
			payload.MergedCredentials++
			if credential.Status != upstreamcredential.StatusArchived {
				if _, err := svc.entFromContext(ctx).UpstreamCredential.UpdateOneID(credential.ID).
					SetStatus(upstreamcredential.StatusArchived).
					Save(ctx); err != nil {
					return payload, fmt.Errorf("failed to archive duplicate upstream credential: %w", err)
				}
				payload.ArchivedCredentials++
			}
			continue
		}
		if archived := archivedBySecretFingerprint[secretFingerprint]; archived != nil {
			if _, err := svc.entFromContext(ctx).UpstreamCredential.UpdateOneID(archived.ID).
				ClearSecretFingerprint().
				Save(ctx); err != nil {
				return payload, fmt.Errorf("failed to release archived upstream credential secret fingerprint: %w", err)
			}
			delete(archivedBySecretFingerprint, secretFingerprint)
		}
		updatedCredential, err := svc.entFromContext(ctx).UpstreamCredential.UpdateOneID(credential.ID).
			SetSecretFingerprint(secretFingerprint).
			Save(ctx)
		if err != nil {
			return payload, fmt.Errorf("failed to backfill upstream credential secret fingerprint: %w", err)
		}
		targetsBySecretFingerprint[secretFingerprint] = updatedCredential
		payload.UpdatedCredentials++
	}

	if payload.UpdatedCredentials > 0 || payload.MergedCredentials > 0 || payload.MigratedRefs > 0 || payload.ArchivedCredentials > 0 {
		svc.reloadChannels()
	}

	return payload, nil
}

func (svc *UpstreamCredentialService) reloadChannels() {
	if svc != nil && svc.channelService != nil {
		svc.channelService.asyncReloadChannels()
	}
}

func normalizeCredentialWeight(weight int) int {
	if weight <= 0 {
		return 1
	}

	return weight
}

func stringValuePtr(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func resolveCredentialSecretKind(secretKind *upstreamcredential.SecretKind, authKind *upstreamcredential.AuthKind, secret objects.UpstreamCredentialSecret) upstreamcredential.SecretKind {
	if secretKind != nil && upstreamcredential.SecretKindValidator(*secretKind) == nil {
		return *secretKind
	}
	if authKind != nil {
		candidate := upstreamcredential.SecretKind(authKind.String())
		if upstreamcredential.SecretKindValidator(candidate) == nil {
			return candidate
		}
	}

	candidate := upstreamcredential.SecretKind(CredentialSecretKindForSecret(secret))
	if upstreamcredential.SecretKindValidator(candidate) == nil {
		return candidate
	}

	return upstreamcredential.SecretKindOther
}

func resolveCredentialIssuerScope(issuerScope *string, providerType *string, baseURL *string, secret objects.UpstreamCredentialSecret) string {
	if value := strings.TrimSpace(stringValuePtr(issuerScope)); value != "" {
		return normalizeCredentialFingerprintPart(value)
	}
	if value := inferCredentialIssuerScopeFromSecret(secret); value != "" {
		return value
	}

	return CredentialIssuerScope(stringValuePtr(providerType), stringValuePtr(baseURL))
}

func inferCredentialIssuerScopeFromSecret(secret objects.UpstreamCredentialSecret) string {
	key := strings.TrimSpace(secret.APIKey)
	switch {
	case strings.HasPrefix(key, "sk-ant-"):
		return "anthropic"
	case strings.HasPrefix(key, "sk-proj-"), strings.HasPrefix(key, "sk-admin-"), strings.HasPrefix(key, "sk-"):
		return "openai"
	case secret.GCP != nil:
		if projectID := strings.TrimSpace(secret.GCP.ProjectID); projectID != "" {
			return "gcp:" + normalizeCredentialFingerprintPart(projectID)
		}
		return "google"
	case secret.OAuth != nil:
		return "openai"
	}

	return ""
}

func authKindFromSecretKind(secretKind upstreamcredential.SecretKind) upstreamcredential.AuthKind {
	candidate := upstreamcredential.AuthKind(secretKind.String())
	if upstreamcredential.AuthKindValidator(candidate) == nil {
		return candidate
	}

	return upstreamcredential.AuthKindOther
}

func providerTypeForChannel(ch *ent.Channel) string {
	if ch == nil {
		return ""
	}

	return ch.Type.String()
}

func channelCredentialAuthKindForSecret(secret objects.UpstreamCredentialSecret) upstreamcredential.AuthKind {
	switch {
	case strings.TrimSpace(secret.APIKey) != "":
		return upstreamcredential.AuthKindAPIKey
	case secret.OAuth != nil:
		return upstreamcredential.AuthKindOauth
	case secret.GCP != nil:
		return upstreamcredential.AuthKindGcp
	case secret.Azure != nil:
		return upstreamcredential.AuthKindAzure
	default:
		return upstreamcredential.AuthKindOther
	}
}
