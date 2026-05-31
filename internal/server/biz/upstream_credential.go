package biz

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channelcredentialref"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
)

type UpstreamCredentialServiceParams struct {
	fx.In

	Ent            *ent.Client
	ChannelService *ChannelService
}

type UpstreamCredentialService struct {
	*AbstractService

	channelService *ChannelService
}

func NewUpstreamCredentialService(params UpstreamCredentialServiceParams) *UpstreamCredentialService {
	return &UpstreamCredentialService{
		AbstractService: &AbstractService{db: params.Ent},
		channelService:  params.ChannelService,
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
	Remark       *string
}

type UpdateUpstreamCredentialInput struct {
	Name   *string
	Status *upstreamcredential.Status
	Weight *int
	Remark *string
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

type MigrateLegacyCredentialsPayload struct {
	MigratedChannels   int
	CreatedCredentials int
	CreatedRefs        int
	SkippedChannels    int
}

func (svc *UpstreamCredentialService) CreateUpstreamCredential(ctx context.Context, input CreateUpstreamCredentialInput) (*ent.UpstreamCredential, error) {
	secretKind := resolveCredentialSecretKind(input.SecretKind, input.AuthKind, input.Secret)
	issuerScope := resolveCredentialIssuerScope(input.IssuerScope, input.ProviderType, input.BaseURL, input.Secret)
	fingerprint := CredentialFingerprintForSecret(issuerScope, secretKind.String(), input.Secret)
	if fingerprint == "" {
		return nil, fmt.Errorf("credential secret is empty or unsupported")
	}

	existing, err := svc.entFromContext(ctx).UpstreamCredential.Query().
		Where(upstreamcredential.Fingerprint(fingerprint)).
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check credential fingerprint: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("upstream credential already exists with fingerprint %s", fingerprint)
	}

	create := svc.entFromContext(ctx).UpstreamCredential.Create().
		SetProviderType(strings.TrimSpace(stringValuePtr(input.ProviderType))).
		SetBaseURL(normalizeCredentialBaseURL(stringValuePtr(input.BaseURL))).
		SetAuthKind(authKindFromSecretKind(secretKind)).
		SetSecretKind(secretKind).
		SetIssuerScope(issuerScope).
		SetKeyHint(CredentialKeyHintForSecret(secretKind.String(), input.Secret)).
		SetSecretPayload(input.Secret).
		SetFingerprint(fingerprint)

	if input.Name != nil {
		create.SetName(strings.TrimSpace(*input.Name))
	}
	if input.Status != nil {
		create.SetStatus(*input.Status)
	}
	if input.Weight != nil {
		create.SetWeight(normalizeCredentialWeight(*input.Weight))
	}
	if input.Remark != nil {
		create.SetRemark(strings.TrimSpace(*input.Remark))
	}

	credential, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create upstream credential: %w", err)
	}

	svc.reloadChannels()

	return credential, nil
}

func (svc *UpstreamCredentialService) UpdateUpstreamCredential(ctx context.Context, id int, input UpdateUpstreamCredentialInput) (*ent.UpstreamCredential, error) {
	update := svc.entFromContext(ctx).UpstreamCredential.UpdateOneID(id)

	if input.Name != nil {
		update.SetName(strings.TrimSpace(*input.Name))
	}
	if input.Status != nil {
		update.SetStatus(*input.Status)
	}
	if input.Weight != nil {
		update.SetWeight(normalizeCredentialWeight(*input.Weight))
	}
	if input.Remark != nil {
		update.SetRemark(strings.TrimSpace(*input.Remark))
	}

	credential, err := update.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update upstream credential: %w", err)
	}

	svc.reloadChannels()

	return credential, nil
}

func (svc *UpstreamCredentialService) RotateUpstreamCredentialSecret(ctx context.Context, id int, input RotateUpstreamCredentialSecretInput) (*ent.UpstreamCredential, error) {
	existing, err := svc.entFromContext(ctx).UpstreamCredential.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get upstream credential: %w", err)
	}

	secretKind := existing.SecretKind
	issuerScope := existing.IssuerScope
	if strings.TrimSpace(issuerScope) == "" {
		issuerScope = CredentialIssuerScope(existing.ProviderType, existing.BaseURL)
	}
	fingerprint := CredentialFingerprintForSecret(issuerScope, secretKind.String(), input.Secret)
	if fingerprint == "" {
		return nil, fmt.Errorf("credential secret is empty or unsupported")
	}

	conflict, err := svc.entFromContext(ctx).UpstreamCredential.Query().
		Where(
			upstreamcredential.Fingerprint(fingerprint),
			upstreamcredential.IDNEQ(id),
		).
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check credential fingerprint: %w", err)
	}
	if conflict != nil {
		return nil, fmt.Errorf("another upstream credential already uses this secret fingerprint")
	}

	credential, err := svc.entFromContext(ctx).UpstreamCredential.UpdateOneID(id).
		SetSecretPayload(input.Secret).
		SetFingerprint(fingerprint).
		SetSecretKind(secretKind).
		SetIssuerScope(issuerScope).
		SetKeyHint(CredentialKeyHintForSecret(secretKind.String(), input.Secret)).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to rotate upstream credential secret: %w", err)
	}

	svc.reloadChannels()

	return credential, nil
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

func (svc *UpstreamCredentialService) MigrateLegacyChannelCredentials(ctx context.Context) (*MigrateLegacyCredentialsPayload, error) {
	channels, err := svc.entFromContext(ctx).Channel.Query().
		WithCredentialRefs().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query channels: %w", err)
	}

	payload := &MigrateLegacyCredentialsPayload{}

	for _, ch := range channels {
		if ch == nil {
			continue
		}

		views := legacyCredentialViews(ch)
		if len(views) == 0 {
			payload.SkippedChannels++
			continue
		}

		channelMigrated := false
		for idx, view := range views {
			credential, created, err := svc.findOrCreateCredentialForChannel(ctx, ch, view, idx)
			if err != nil {
				return payload, err
			}
			if created {
				payload.CreatedCredentials++
			}

			refCreated, err := svc.ensureChannelCredentialRef(ctx, ch.ID, credential.ID)
			if err != nil {
				return payload, err
			}
			if refCreated {
				payload.CreatedRefs++
			}

			channelMigrated = channelMigrated || created || refCreated
		}

		if channelMigrated {
			payload.MigratedChannels++
		}
	}

	if payload.MigratedChannels > 0 || payload.CreatedCredentials > 0 || payload.CreatedRefs > 0 {
		svc.reloadChannels()
	}

	return payload, nil
}

func (svc *UpstreamCredentialService) RunStartupMigration(ctx context.Context) {
	if svc == nil {
		return
	}

	ctx = ent.NewContext(ctx, svc.db)
	ctx = authz.WithSystemBypass(ctx, "upstream-credential-startup-migration")

	payload, err := svc.MigrateLegacyChannelCredentials(ctx)
	if err != nil {
		log.Warn(ctx, "failed to migrate legacy channel credentials",
			log.Cause(err),
		)
		return
	}
	if payload == nil || (payload.MigratedChannels == 0 && payload.CreatedCredentials == 0 && payload.CreatedRefs == 0) {
		return
	}

	log.Info(ctx, "migrated legacy channel credentials",
		log.Int("migrated_channels", payload.MigratedChannels),
		log.Int("created_credentials", payload.CreatedCredentials),
		log.Int("created_refs", payload.CreatedRefs),
	)
}

func (svc *UpstreamCredentialService) findOrCreateCredentialForChannel(ctx context.Context, ch *ent.Channel, view ChannelCredentialView, idx int) (*ent.UpstreamCredential, bool, error) {
	existing, err := svc.entFromContext(ctx).UpstreamCredential.Query().
		Where(upstreamcredential.Fingerprint(view.Fingerprint)).
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, false, fmt.Errorf("failed to query upstream credential: %w", err)
	}
	if existing != nil {
		return existing, false, nil
	}

	viewSecretKind := upstreamcredential.SecretKind(resolveViewSecretKind(view))
	if upstreamcredential.SecretKindValidator(viewSecretKind) != nil {
		viewSecretKind = upstreamcredential.SecretKindOther
	}

	create := svc.entFromContext(ctx).UpstreamCredential.Create().
		SetName(defaultCredentialName(ch, view, idx)).
		SetProviderType(ch.Type.String()).
		SetBaseURL(normalizeCredentialBaseURL(ch.BaseURL)).
		SetAuthKind(upstreamcredential.AuthKind(view.AuthKind)).
		SetSecretKind(viewSecretKind).
		SetIssuerScope(resolveViewIssuerScope(ch, view)).
		SetKeyHint(view.KeyHint).
		SetSecretPayload(view.Secret).
		SetFingerprint(view.Fingerprint).
		SetWeight(normalizeCredentialWeight(view.Weight)).
		SetStatus(legacyCredentialStatus(ch, view))

	credential, err := create.Save(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create upstream credential for channel %d: %w", ch.ID, err)
	}

	return credential, true, nil
}

func (svc *UpstreamCredentialService) ensureChannelCredentialRef(ctx context.Context, channelID int, credentialID int) (bool, error) {
	exists, err := svc.entFromContext(ctx).ChannelCredentialRef.Query().
		Where(
			channelcredentialref.ChannelID(channelID),
			channelcredentialref.CredentialID(credentialID),
		).
		Exist(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check channel credential ref: %w", err)
	}
	if exists {
		return false, nil
	}

	if _, err := svc.entFromContext(ctx).ChannelCredentialRef.Create().
		SetChannelID(channelID).
		SetCredentialID(credentialID).
		SetEnabled(true).
		Save(ctx); err != nil {
		return false, fmt.Errorf("failed to create channel credential ref: %w", err)
	}

	return true, nil
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

func defaultCredentialName(ch *ent.Channel, view ChannelCredentialView, idx int) string {
	if ch == nil {
		return fmt.Sprintf("credential %d", idx+1)
	}

	authKind := view.AuthKind
	if authKind == "" {
		authKind = upstreamcredential.AuthKindAPIKey.String()
	}

	if len(ch.Credentials.GetAllAPIKeys()) > 1 {
		return fmt.Sprintf("%s %s %d", ch.Name, authKind, idx+1)
	}

	return fmt.Sprintf("%s %s", ch.Name, authKind)
}

func legacyCredentialStatus(ch *ent.Channel, view ChannelCredentialView) upstreamcredential.Status {
	if ch == nil || view.Secret.APIKey == "" {
		return upstreamcredential.StatusEnabled
	}

	for _, disabled := range ch.DisabledAPIKeys {
		if disabled.Key != view.Secret.APIKey {
			continue
		}

		if isCredentialScopedAutoDisableStatus(disabled.ErrorCode) {
			return upstreamcredential.StatusDisabled
		}
	}

	return upstreamcredential.StatusEnabled
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

func resolveViewSecretKind(view ChannelCredentialView) string {
	if kind := normalizeCredentialFingerprintPart(view.SecretKind); kind != "" {
		return kind
	}
	if kind := normalizeCredentialFingerprintPart(view.AuthKind); kind != "" {
		return kind
	}

	return CredentialSecretKindForSecret(view.Secret)
}

func resolveViewIssuerScope(ch *ent.Channel, view ChannelCredentialView) string {
	if scope := strings.TrimSpace(view.IssuerScope); scope != "" {
		return scope
	}
	if ch == nil {
		return "unknown"
	}

	return CredentialIssuerScope(ch.Type.String(), ch.BaseURL)
}

func channelWithResolvedCredentials(ch *ent.Channel) *ent.Channel {
	if ch == nil {
		return nil
	}

	views := credentialViewsFromRefs(ch)
	if len(views) == 0 {
		return ch
	}

	clone := *ch
	clone.Credentials = credentialsFromViews(views, ch.Credentials)

	return &clone
}

func channelCredentialViews(ch *ent.Channel) []ChannelCredentialView {
	views := credentialViewsFromRefs(ch)
	if len(views) > 0 {
		return dedupeCredentialViews(views)
	}

	return legacyCredentialViews(ch)
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
