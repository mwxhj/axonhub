package biz

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

// UsageLogService handles usage log operations.
type UsageLogService struct {
	*AbstractService

	SystemService  *SystemService
	ChannelService *ChannelService

	// OnUsageLogCreated is called after a usage log is successfully created.
	// Used to invalidate caches that depend on usage log data.
	OnUsageLogCreated func()
}

func (s *UsageLogService) computeUsageCost(ctx context.Context, channelID int, modelID string, usage *llm.Usage) ([]objects.CostItem, *float64, string) {
	if usage == nil {
		return nil, nil, ""
	}

	ch := s.ChannelService.GetEnabledChannel(channelID)
	if ch == nil {
		log.Warn(ctx, "channel not enabled for cost calculation",
			log.Int("channel_id", channelID),
			log.String("model_id", modelID),
		)

		return nil, nil, ""
	}

	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "checking cached model price",
			log.Int("channel_id", channelID),
			log.String("model_id", modelID),
			log.Int("cached_price_count", len(ch.cachedModelPrices)),
		)
	}

	if modelPrice, ok := ch.cachedModelPrices[modelID]; ok {
		items, total := ComputeUsageCost(usage, modelPrice.Price)

		totalCost := total.InexactFloat64()
		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "computed usage cost from cache",
				log.Int("channel_id", channelID),
				log.String("model_id", modelID),
				log.Float64("total_cost", totalCost),
				log.Int64("total_tokens", usage.TotalTokens),
				log.String("price_reference_id", modelPrice.ReferenceID),
			)
		}

		return items, lo.ToPtr(totalCost), modelPrice.ReferenceID
	}

	return nil, nil, ""
}

// NewUsageLogService creates a new UsageLogService.
func NewUsageLogService(ent *ent.Client, systemService *SystemService, channelService *ChannelService) *UsageLogService {
	return &UsageLogService{
		AbstractService: &AbstractService{
			db: ent,
		},
		SystemService:  systemService,
		ChannelService: channelService,
	}
}

// CreateUsageLogParams represents the parameters for creating a usage log.
type CreateUsageLogParams struct {
	RequestID             int
	ProjectID             int
	ChannelID             int
	ActualModelID         string // The channel actual model ID, not the request model ID.
	CredentialID          int
	CredentialFingerprint string
	SecretFingerprint     string
	ResourceScopeKey      string
	CredentialName        string
	CredentialKeyHint     string
	CredentialSource      string
	CredentialQuotaStatus string
	QuotaScopeID          int
	QuotaScopeName        string
	QuotaScopeStatus      string
	Usage                 *llm.Usage
	Source                usagelog.Source
	Format                string
	APIKeyID              *int
}

// CreateUsageLog creates a new usage log record from LLM response usage data.
func (s *UsageLogService) CreateUsageLog(ctx context.Context, params CreateUsageLogParams) (*ent.UsageLog, error) {
	if params.Usage == nil {
		return nil, nil // No usage data to log
	}

	client := s.entFromContext(ctx)

	mut := client.UsageLog.Create().
		SetRequestID(params.RequestID).
		SetProjectID(params.ProjectID).
		SetModelID(params.ActualModelID).
		SetChannelID(params.ChannelID).
		SetPromptTokens(params.Usage.PromptTokens).
		SetCompletionTokens(params.Usage.CompletionTokens).
		SetTotalTokens(params.Usage.TotalTokens).
		SetSource(params.Source).
		SetFormat(params.Format)
	selectedQuotaScopeID := 0

	if params.APIKeyID != nil {
		mut = mut.SetAPIKeyID(*params.APIKeyID)
	} else if ctxAPIKey, ok := contexts.GetAPIKey(ctx); ok && ctxAPIKey != nil {
		mut = mut.SetAPIKeyID(ctxAPIKey.ID)
	}

	if params.CredentialFingerprint != "" {
		mut = mut.SetCredentialFingerprint(params.CredentialFingerprint)
	} else if fingerprint, ok := contexts.GetChannelCredentialFingerprint(ctx); ok && fingerprint != "" {
		mut = mut.SetCredentialFingerprint(fingerprint)
	}
	if params.SecretFingerprint != "" {
		mut = mut.SetSecretFingerprint(params.SecretFingerprint)
	} else if secretFingerprint, ok := contexts.GetChannelCredentialSecretFingerprint(ctx); ok && secretFingerprint != "" {
		mut = mut.SetSecretFingerprint(secretFingerprint)
	}
	if params.ResourceScopeKey != "" {
		mut = mut.SetResourceScopeKey(params.ResourceScopeKey)
	} else if resourceScopeKey, ok := contexts.GetChannelCredentialResourceScopeKey(ctx); ok && resourceScopeKey != "" {
		mut = mut.SetResourceScopeKey(resourceScopeKey)
	}
	if params.CredentialID > 0 {
		mut = mut.SetCredentialID(params.CredentialID)
	} else if credentialID, ok := contexts.GetChannelCredentialID(ctx); ok && credentialID > 0 {
		mut = mut.SetCredentialID(credentialID)
	}
	if params.CredentialName != "" {
		mut = mut.SetCredentialNameSnapshot(params.CredentialName)
	} else if name, ok := contexts.GetChannelCredentialName(ctx); ok && name != "" {
		mut = mut.SetCredentialNameSnapshot(name)
	}
	if params.CredentialKeyHint != "" {
		mut = mut.SetCredentialKeyHint(params.CredentialKeyHint)
	} else if keyHint, ok := contexts.GetChannelCredentialKeyHint(ctx); ok && keyHint != "" {
		mut = mut.SetCredentialKeyHint(keyHint)
	}
	if params.CredentialSource != "" {
		mut = mut.SetCredentialSource(params.CredentialSource)
	} else if source, ok := contexts.GetChannelCredentialSource(ctx); ok && source != "" {
		mut = mut.SetCredentialSource(source)
	}
	if params.CredentialQuotaStatus != "" {
		mut = mut.SetCredentialQuotaStatusSnapshot(params.CredentialQuotaStatus)
	} else if quotaStatus, ok := contexts.GetChannelCredentialQuotaStatus(ctx); ok && quotaStatus != "" {
		mut = mut.SetCredentialQuotaStatusSnapshot(quotaStatus)
	}
	if params.QuotaScopeID > 0 {
		mut = mut.SetQuotaScopeID(params.QuotaScopeID)
		selectedQuotaScopeID = params.QuotaScopeID
	} else if quotaScopeID, ok := contexts.GetChannelCredentialQuotaScopeID(ctx); ok && quotaScopeID > 0 {
		mut = mut.SetQuotaScopeID(quotaScopeID)
		selectedQuotaScopeID = quotaScopeID
	}
	if params.QuotaScopeName != "" {
		mut = mut.SetQuotaScopeNameSnapshot(params.QuotaScopeName)
	} else if quotaScopeName, ok := contexts.GetChannelCredentialQuotaScopeName(ctx); ok && quotaScopeName != "" {
		mut = mut.SetQuotaScopeNameSnapshot(quotaScopeName)
	}
	if params.QuotaScopeStatus != "" {
		mut = mut.SetQuotaScopeStatusSnapshot(params.QuotaScopeStatus)
	} else if quotaScopeStatus, ok := contexts.GetChannelCredentialQuotaScopeStatus(ctx); ok && quotaScopeStatus != "" {
		mut = mut.SetQuotaScopeStatusSnapshot(quotaScopeStatus)
	}

	// Set prompt tokens details if available
	if params.Usage.PromptTokensDetails != nil {
		mut = mut.
			SetPromptAudioTokens(params.Usage.PromptTokensDetails.AudioTokens).
			SetPromptCachedTokens(params.Usage.PromptTokensDetails.CachedTokens).
			SetPromptWriteCachedTokens(params.Usage.PromptTokensDetails.WriteCachedTokens).
			SetPromptWriteCachedTokens5m(params.Usage.PromptTokensDetails.WriteCached5MinTokens).
			SetPromptWriteCachedTokens1h(params.Usage.PromptTokensDetails.WriteCached1HourTokens)
	}

	// Set completion tokens details if available
	if params.Usage.CompletionTokensDetails != nil {
		mut = mut.
			SetCompletionAudioTokens(params.Usage.CompletionTokensDetails.AudioTokens).
			SetCompletionReasoningTokens(params.Usage.CompletionTokensDetails.ReasoningTokens).
			SetCompletionAcceptedPredictionTokens(params.Usage.CompletionTokensDetails.AcceptedPredictionTokens).
			SetCompletionRejectedPredictionTokens(params.Usage.CompletionTokensDetails.RejectedPredictionTokens)
	}

	// Calculate cost if price is configured
	var (
		totalCost        *float64
		costItems        []objects.CostItem
		priceReferenceID string
	)

	costItems, totalCost, priceReferenceID = s.computeUsageCost(ctx, params.ChannelID, params.ActualModelID, params.Usage)

	mut = mut.
		SetNillableTotalCost(totalCost).
		SetCostItems(costItems)

	if priceReferenceID != "" {
		mut = mut.SetCostPriceReferenceID(priceReferenceID)
	}

	usageLog, err := mut.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create usage log: %w", err)
	}

	if selectedQuotaScopeID > 0 {
		if err := s.incrementCredentialQuotaScopeUsage(ctx, selectedQuotaScopeID, params.Usage, totalCost); err != nil {
			log.Warn(ctx, "Failed to update credential quota scope usage",
				log.Int("quota_scope_id", selectedQuotaScopeID),
				log.Cause(err),
			)
		}
	}

	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "Created usage log",
			log.Int("usage_log_id", usageLog.ID),
			log.Int("request_id", params.RequestID),
			log.String("model_id", params.ActualModelID),
			log.Int64("total_tokens", params.Usage.TotalTokens),
		)
	}

	if s.OnUsageLogCreated != nil {
		s.OnUsageLogCreated()
	}

	return usageLog, nil
}

func (s *UsageLogService) incrementCredentialQuotaScopeUsage(ctx context.Context, quotaScopeID int, usage *llm.Usage, totalCost *float64) error {
	if usage == nil || quotaScopeID <= 0 {
		return nil
	}

	client := s.entFromContext(ctx)
	now := time.Now()
	scope, err := client.CredentialQuotaScope.Get(ctx, quotaScopeID)
	if err != nil {
		return fmt.Errorf("failed to get credential quota scope: %w", err)
	}

	increment := quotaScopeIncrement(scope.Unit, usage, totalCost)
	if increment.IsZero() {
		return nil
	}

	used, err := quotaScopeUsedAmount(scope)
	if err != nil {
		return err
	}

	var resetSnapshot *quotaScopeResetSnapshot
	if shouldResetQuotaScope(scope, now) {
		resetSnapshot = resetQuotaScopeWindow(scope, now)
		used = decimal.Zero
	}

	nextUsed := used.Add(increment)
	nextStatus := quotaScopeStatusAfterUsage(scope, nextUsed)
	update := client.CredentialQuotaScope.UpdateOneID(quotaScopeID).
		SetUsedAmount(nextUsed.String())
	if resetSnapshot != nil {
		update.SetWindowStartedAt(resetSnapshot.WindowStartedAt)
		if resetSnapshot.ResetAt != nil {
			update.SetResetAt(*resetSnapshot.ResetAt)
		} else {
			update.ClearResetAt()
		}
		if resetSnapshot.ClearPauseUntil {
			update.ClearPauseUntil()
		}
	}
	if nextStatus != "" && nextStatus != scope.Status {
		update.SetStatus(nextStatus)
	}

	if _, err := update.Save(ctx); err != nil {
		return fmt.Errorf("failed to update credential quota scope usage: %w", err)
	}

	if nextStatus != "" && nextStatus != scope.Status && s.ChannelService != nil {
		s.ChannelService.asyncReloadChannels()
	}

	return nil
}

func quotaScopeUsedAmount(scope *ent.CredentialQuotaScope) (decimal.Decimal, error) {
	if scope == nil {
		return decimal.Zero, nil
	}

	raw := strings.TrimSpace(scope.UsedAmount)
	if raw == "" {
		return decimal.Zero, nil
	}

	used, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to parse quota used amount: %w", err)
	}

	return used, nil
}

func shouldResetQuotaScope(scope *ent.CredentialQuotaScope, now time.Time) bool {
	if scope == nil || scope.ResetAt == nil || scope.ResetAt.After(now) {
		return false
	}

	switch scope.ResetPolicy {
	case credentialquotascope.ResetPolicyDaily,
		credentialquotascope.ResetPolicyMonthly,
		credentialquotascope.ResetPolicyCustom:
		return true
	default:
		return false
	}
}

type quotaScopeResetSnapshot struct {
	WindowStartedAt time.Time
	ResetAt         *time.Time
	ClearPauseUntil bool
}

func resetQuotaScopeWindow(scope *ent.CredentialQuotaScope, now time.Time) *quotaScopeResetSnapshot {
	snapshot := &quotaScopeResetSnapshot{
		WindowStartedAt: now,
		ClearPauseUntil: true,
	}
	if scope == nil {
		return snapshot
	}

	switch scope.ResetPolicy {
	case credentialquotascope.ResetPolicyDaily:
		next := nextQuotaResetAt(*scope.ResetAt, now, func(t time.Time) time.Time {
			return t.AddDate(0, 0, 1)
		})
		snapshot.ResetAt = &next
	case credentialquotascope.ResetPolicyMonthly:
		next := nextQuotaResetAt(*scope.ResetAt, now, func(t time.Time) time.Time {
			return t.AddDate(0, 1, 0)
		})
		snapshot.ResetAt = &next
	case credentialquotascope.ResetPolicyCustom:
		if scope.WindowStartedAt != nil && scope.ResetAt.After(*scope.WindowStartedAt) {
			window := scope.ResetAt.Sub(*scope.WindowStartedAt)
			next := *scope.ResetAt
			for !next.After(now) {
				next = next.Add(window)
			}
			snapshot.ResetAt = &next
		}
	}

	return snapshot
}

func nextQuotaResetAt(resetAt time.Time, now time.Time, step func(time.Time) time.Time) time.Time {
	next := resetAt
	for !next.After(now) {
		next = step(next)
	}

	return next
}

func quotaScopeIncrement(unit credentialquotascope.Unit, usage *llm.Usage, totalCost *float64) decimal.Decimal {
	switch unit {
	case credentialquotascope.UnitToken:
		return decimal.NewFromInt(usage.TotalTokens)
	case credentialquotascope.UnitRequest:
		return decimal.NewFromInt(1)
	case credentialquotascope.UnitUsd:
		if totalCost == nil {
			return decimal.Zero
		}
		return decimal.NewFromFloat(*totalCost)
	default:
		return decimal.Zero
	}
}

func quotaScopeStatusAfterUsage(scope *ent.CredentialQuotaScope, used decimal.Decimal) credentialquotascope.Status {
	if scope == nil {
		return ""
	}

	limitRaw := strings.TrimSpace(scope.LimitAmount)
	if limitRaw == "" {
		return credentialquotascope.StatusAvailable
	}

	limit, err := decimal.NewFromString(limitRaw)
	if err != nil || !limit.IsPositive() {
		return credentialquotascope.StatusAvailable
	}

	if used.GreaterThanOrEqual(limit) {
		switch scope.OverLimitAction {
		case credentialquotascope.OverLimitActionPause:
			return credentialquotascope.StatusPaused
		case credentialquotascope.OverLimitActionDisable:
			return credentialquotascope.StatusDisabled
		default:
			return credentialquotascope.StatusWarning
		}
	}

	if scope.WarningThresholdPercent != nil && *scope.WarningThresholdPercent > 0 {
		threshold := limit.Mul(decimal.NewFromInt(int64(*scope.WarningThresholdPercent))).Div(decimal.NewFromInt(100))
		if used.GreaterThanOrEqual(threshold) {
			return credentialquotascope.StatusWarning
		}
	}

	return credentialquotascope.StatusAvailable
}

// CreateUsageLogFromRequest creates a usage log from request and response data.
func (s *UsageLogService) CreateUsageLogFromRequest(
	ctx context.Context,
	request *ent.Request,
	requestExec *ent.RequestExecution,
	usage *llm.Usage,
) (*ent.UsageLog, error) {
	if request == nil || usage == nil {
		return nil, nil
	}

	return s.CreateUsageLog(ctx, CreateUsageLogParams{
		RequestID:             request.ID,
		ProjectID:             request.ProjectID,
		ChannelID:             requestExec.ChannelID,
		ActualModelID:         requestExec.ModelID,
		CredentialID:          requestExec.CredentialID,
		CredentialFingerprint: requestExec.CredentialFingerprint,
		SecretFingerprint:     requestExec.SecretFingerprint,
		ResourceScopeKey:      requestExec.ResourceScopeKey,
		CredentialName:        requestExec.CredentialNameSnapshot,
		CredentialKeyHint:     requestExec.CredentialKeyHint,
		CredentialSource:      requestExec.CredentialSource,
		CredentialQuotaStatus: requestExec.CredentialQuotaStatusSnapshot,
		QuotaScopeID:          requestExec.QuotaScopeID,
		QuotaScopeName:        requestExec.QuotaScopeNameSnapshot,
		QuotaScopeStatus:      requestExec.QuotaScopeStatusSnapshot,
		Usage:                 usage,
		Source:                usagelog.Source(request.Source),
		Format:                request.Format,
		APIKeyID:              lo.ToPtr(request.APIKeyID),
	})
}
