package biz

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
	"go.uber.org/fx"
	"golang.org/x/sync/errgroup"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/internal/server/scheduler"
	"github.com/looplj/axonhub/llm/httpclient"
)

const (
	maxConcurrentQuotaChecks           = 8
	providerQuotaLegacyChannelScopeKey = "channel"
	providerQuotaChannelScopePrefix    = "channel:"
)

type QuotaChannelStatus struct {
	Status                providerquotastatus.Status
	Ready                 bool
	CredentialID          int
	CredentialFingerprint string
	SecretFingerprint     string
	ResourceScopeKey      string
	QuotaScopeID          int
	Limits                []provider_quota.QuotaLimitStatus
}

type BackfillProviderQuotaStatusScopeKeysPayload struct {
	ScannedStatuses int
	UpdatedStatuses int
	SkippedStatuses int
}

// EffectiveStatus returns the effective quota status for the given limit type.
//
// If the channel-level status is Exhausted, it short-circuits regardless of
// per-limit data — a channel marked exhausted at the top level is treated as
// fully unavailable. This means if a future provider sets channel-level
// "exhausted" for a single limit type (e.g., images), token-limit queries
// would also return "exhausted" even if tokens remain.
func (s *QuotaChannelStatus) EffectiveStatus(limitType provider_quota.QuotaLimitType) (providerquotastatus.Status, bool) {
	if s.Status == providerquotastatus.StatusExhausted {
		return providerquotastatus.StatusExhausted, false
	}

	if len(s.Limits) == 0 {
		return s.Status, s.Ready
	}

	var worstStatus providerquotastatus.Status
	worstReady := true
	found := false

	for _, l := range s.Limits {
		if l.Type != limitType {
			continue
		}

		ls := providerquotastatus.Status(l.Status)
		if !found {
			worstStatus = ls
			worstReady = l.Ready
			found = true
			continue
		}

		if quotaStatusRank(ls) > quotaStatusRank(worstStatus) {
			worstStatus = ls
			worstReady = l.Ready
		} else if quotaStatusRank(ls) == quotaStatusRank(worstStatus) {
			worstReady = worstReady && l.Ready
		}
	}

	if !found {
		// No matching limit type: return Unknown with ready=true so the channel
		// is not filtered out. This differs from a per-limit "unknown" status
		// (where ready=false) because missing data should not block routing.
		return providerquotastatus.StatusUnknown, true
	}

	return worstStatus, worstReady
}

func quotaStatusRank(s providerquotastatus.Status) int {
	switch s {
	case providerquotastatus.StatusAvailable:
		return 0
	case providerquotastatus.StatusWarning:
		return 1
	case providerquotastatus.StatusExhausted:
		return 2
	case providerquotastatus.StatusUnknown:
		return -1
	default:
		return -1
	}
}

// HOW TO ADD A NEW PROVIDER QUOTA CHECKER
// ========================================
//
// There are two patterns depending on whether the provider has its own
// channel type or shares an existing OpenAI-compatible channel type:
//
// ── PATTERN A: Dedicated channel type (e.g. claudecode, codex, nanogpt) ──
//
// 1. Create the checker in internal/server/biz/provider_quota/
//
//    Implement the QuotaChecker interface:
//      - CheckQuota(ctx, ch) -> makes the API request and parses the response internally
//      - Returns normalized QuotaData with:
//        * Status: "available", "warning", "exhausted", or "unknown"
//        * Ready: true for available/warning, false for exhausted/unknown
//        * NextResetAt: optional timestamp of next quota reset
//        * RawData: provider-specific data (stored in JSON format)
//
// 2. Add the provider type to the database schema
//
//    In internal/ent/schema/channel.go:
//      - Add new value to the channel.Type enum (e.g., "myprovider")
//
//    In internal/ent/schema/provider_quota_status.go:
//      - Add new value to the provider_type enum (e.g., "myprovider")
//
// 3. Register the provider in ProviderQuotaService
//
//    a. Create a registration function (e.g., registerMyProviderSupport())
//    b. Add it to NewProviderQuotaService()
//    c. Update getProviderType() to map channel.TypeMyprovider -> "myprovider"
//    d. Update runQuotaCheck() to include channel.TypeMyprovider in TypeIn filter
//
//    Example:
//
//      func (svc *ProviderQuotaService) registerMyProviderSupport() {
//        svc.checkers["myprovider"] = provider_quota.NewMyProviderQuotaChecker(svc.httpClient)
//      }
//
// ── PATTERN B: URL-based detection for OpenAI-compatible providers ──
//
//    Use this pattern when the provider reuses channel.TypeOpenai or
//    channel.TypeOpenaiResponses but has its own quota API.
//
// 1. Create the checker in internal/server/biz/provider_quota/
//
//    Same QuotaChecker interface as Pattern A, plus:
//      - SupportsChannel(ch) must check URL (e.g., strings.HasSuffix(host, ".wafer.ai"))
//
// 2. Add the provider type to the database schema
//
//    In internal/ent/schema/provider_quota_status.go:
//      - Add new value to the provider_type enum (e.g., "wafer")
//
//    Do NOT modify channel.Type enum — the provider reuses TypeOpenai.
//
// 3. Add URL detection in internal/server/biz/provider_quota/url_detection.go
//
//    a. Add the URL pattern to urlProviderMap (e.g., "wafer.ai": "wafer")
//    b. The DetectProviderFromURL() function handles the mapping
//
// 4. Register the provider in ProviderQuotaService
//
//    a. Create a registration function (e.g., registerWaferSupport())
//    b. Add it to NewProviderQuotaService()
//    c. getProviderType() already handles URL-based detection for TypeOpenai
//    d. hasCredentialsForProvider() already handles API-key-only auth for URL-detected providers
//
//    Example:
//
//      func (svc *ProviderQuotaService) registerWaferSupport() {
//        svc.checkers["wafer"] = provider_quota.NewWaferQuotaChecker(svc.httpClient)
//      }
//
// 5. Regenerate Ent schema
//
//    make generate
//
// 6. Implement the frontend display (optional)
//
//    Add provider-specific display logic in frontend/src/components/quota-badges.tsx:
//      - Update QuotaData type to include provider-specific fields
//      - Add display logic for the provider type in QuotaRow component
//
// EXAMPLE: CLAUDE CODE PROVIDER (Pattern A)
// =========================================
//
// Checker: internal/server/biz/provider_quota/claudecode_checker.go
//   - Makes minimal request to Claude Code API
//   - Internally parses rate limit headers (anthropic-ratelimit-unified-status, etc.)
//   - Normalizes status (allowed -> available, throttled -> exhausted)
//   - Detects warning state (utilization >= 80%)
//   - Maps representative claim to reset time
//
// EXAMPLE: CODEX PROVIDER (Pattern A)
// ===================================
//
// Checker: internal/server/biz/provider_quota/codex_checker.go
//   - Makes request to ChatGPT usage endpoint (/backend-api/wham/usage)
//   - Internally parses JSON response (plan_type, rate_limit)
//   - Normalizes status based on limit_reached and allowed flags
//   - Detects warning state (primary_window.used_percent >= 80)
//
// EXAMPLE: NANO GPT PROVIDER (Pattern A, simple API key, non-OAuth)
// ================================================================
//
// Checker: internal/server/biz/provider_quota/nanogpt_checker.go
//   - Makes request to NanoGPT subscription usage endpoint (/api/subscription/v1/usage)
//   - Uses simple API key authentication (no OAuth required)
//   - Internally parses JSON response (state, windows, percentUsed)
//   - Normalizes status: active→available, grace→warning, inactive→exhausted
//   - Detects high-usage warning state (any window percentUsed >= 0.8)
//
// EXAMPLE: WAFER PROVIDER (Pattern B, URL-based detection)
// ========================================================
//
// Checker: internal/server/biz/provider_quota/wafer_checker.go
//   - Reuses channel.TypeOpenai / TypeOpenaiResponses
//   - URL detection: host ending in ".wafer.ai" → provider_type "wafer"
//   - Makes request to /v1/inference/quota endpoint
//   - Uses simple API key authentication (no OAuth required)
//   - Internally parses JSON response (current_period_used_percent, remaining_included_requests)
//   - Normalizes status: percent < 80 → available, >= 80 → warning, no remaining → exhausted
//
// EXAMPLE: SYNTHETIC PROVIDER (Pattern B, URL-based detection)
// =============================================================
//
// Checker: internal/server/biz/provider_quota/synthetic_checker.go
//   - Reuses channel.TypeOpenai / TypeOpenaiResponses
//   - URL detection: host ending in ".api.synthetic.new" → provider_type "synthetic"
//   - Makes request to /v2/quotas endpoint
//   - Uses simple API key authentication (no OAuth required)
//   - Internally parses nested JSON (subscription, weeklyTokenLimit, rollingFiveHourLimit)
//   - Normalizes status: limited=true → exhausted, percentRemaining < 20 → warning, else → available
//
// EXAMPLE: NEURALWATT PROVIDER (Pattern B, URL-based detection)
// ==============================================================
//
// Checker: internal/server/biz/provider_quota/neuralwatt_checker.go
//   - Reuses channel.TypeOpenai / TypeOpenaiResponses
//   - URL detection: host ending in ".api.neuralwatt.com" → provider_type "neuralwatt"
//   - Makes request to /v1/quota endpoint
//   - Uses simple API key authentication (no OAuth required)
//   - Internally parses JSON (kwh_included, kwh_remaining, in_overage)
//   - Normalizes status: in_overage → exhausted, remaining < 20% → warning, else → available
//

type ProviderQuotaServiceParams struct {
	fx.In

	Ent                       *ent.Client
	SystemService             *SystemService
	HttpClient                *httpclient.HttpClient
	CheckInterval             time.Duration `name:"provider_quota_check_interval" optional:"true"`
	WarningCheckIntervalRatio int           `name:"provider_quota_warning_check_interval_ratio" optional:"true"`
}

type ProviderQuotaService struct {
	*AbstractService

	SystemService             *SystemService
	checkInterval             time.Duration
	warningCheckIntervalRatio int
	httpClient                *httpclient.HttpClient

	// Registry
	checkers map[string]provider_quota.QuotaChecker

	mu                   sync.Mutex
	quotaCache           sync.Map
	credentialIDCache    sync.Map
	credentialQuotaCache sync.Map
	resourceScopeCache   sync.Map
}

func NewProviderQuotaService(params ProviderQuotaServiceParams) *ProviderQuotaService {
	svc := &ProviderQuotaService{
		AbstractService:           &AbstractService{db: params.Ent},
		SystemService:             params.SystemService,
		checkers:                  make(map[string]provider_quota.QuotaChecker),
		checkInterval:             params.CheckInterval,
		warningCheckIntervalRatio: params.WarningCheckIntervalRatio,
		httpClient:                params.HttpClient,
	}

	svc.registerClaudeCodeSupport()
	svc.registerCodexSupport()
	svc.registerGithubCopilotSupport()
	svc.registerNanoGPTSupport()
	svc.registerWaferSupport()
	svc.registerSyntheticSupport()
	svc.registerNeuralWattSupport()

	return svc
}

func (svc *ProviderQuotaService) RegisterScheduledTasks(ctx context.Context, s *scheduler.Scheduler) error {
	svc.RunStartupMigration(ctx)

	cronExpr := svc.intervalToCronExpr(svc.getCheckInterval())
	return s.Register(ctx, scheduler.TaskSpec{
		Name:        "provider-quota-check",
		Description: "Check provider quota usage periodically",
		CronExpr:    cronExpr,
		Timezone:    "UTC",
	}, svc.runQuotaCheckScheduled)
}

func (svc *ProviderQuotaService) registerClaudeCodeSupport() {
	svc.checkers["claudecode"] = provider_quota.NewClaudeCodeQuotaChecker(svc.httpClient)
}

func (svc *ProviderQuotaService) registerCodexSupport() {
	svc.checkers["codex"] = provider_quota.NewCodexQuotaChecker(svc.httpClient)
}

func (svc *ProviderQuotaService) registerGithubCopilotSupport() {
	svc.checkers["github_copilot"] = provider_quota.NewGithubCopilotQuotaChecker(svc.httpClient)
}

func (svc *ProviderQuotaService) registerNanoGPTSupport() {
	svc.checkers["nanogpt"] = provider_quota.NewNanoGPTQuotaChecker(svc.httpClient)
}

func (svc *ProviderQuotaService) registerWaferSupport() {
	svc.checkers["wafer"] = provider_quota.NewWaferQuotaChecker(svc.httpClient)
}

func (svc *ProviderQuotaService) registerSyntheticSupport() {
	svc.checkers["synthetic"] = provider_quota.NewSyntheticQuotaChecker(svc.httpClient)
}

func (svc *ProviderQuotaService) registerNeuralWattSupport() {
	svc.checkers["neuralwatt"] = provider_quota.NewNeuralWattQuotaChecker(svc.httpClient)
}

func (svc *ProviderQuotaService) intervalToCronExpr(interval time.Duration) string {
	minutes := int(interval.Minutes())
	hours := int(interval.Hours())

	// Hourly or longer intervals
	if hours >= 1 && minutes%60 == 0 {
		if hours == 1 {
			return "0 * * * *" // Every hour
		}

		return fmt.Sprintf("0 */%d * * *", hours) // Every N hours
	}

	// Minute intervals that divide evenly into 60
	if minutes > 0 && 60%minutes == 0 {
		return fmt.Sprintf("*/%d * * * *", minutes)
	}

	// Round down to nearest supported interval (1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30, 60)
	supportedIntervals := []int{1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30, 60}
	filtered := lo.Filter(supportedIntervals, func(si int, _ int) bool {
		return si <= minutes
	})

	rounded := 60
	if len(filtered) > 0 {
		rounded = lo.Max(filtered)
	}

	log.Warn(context.Background(), "Quota check interval does not divide evenly into 60 minutes, rounding to nearest supported interval",
		log.Int("requested_minutes", minutes),
		log.Int("rounded_minutes", rounded))

	return fmt.Sprintf("*/%d * * * *", rounded)
}

func (svc *ProviderQuotaService) getWarningCheckInterval() time.Duration {
	ratio := svc.warningCheckIntervalRatio
	if ratio <= 0 {
		ratio = 4
	}

	return svc.getCheckInterval() * time.Duration(ratio)
}

func (svc *ProviderQuotaService) nextCheckIntervalForStatus(status providerquotastatus.Status) time.Duration {
	if status == providerquotastatus.StatusWarning {
		return svc.getWarningCheckInterval()
	}
	return svc.getCheckInterval()
}

func (svc *ProviderQuotaService) getCheckInterval() time.Duration {
	if svc.checkInterval > 0 {
		return svc.checkInterval
	}

	return 5 * time.Minute
}

func (svc *ProviderQuotaService) RunStartupMigration(ctx context.Context) {
	if svc == nil {
		return
	}

	ctx = ent.NewContext(ctx, svc.db)
	ctx = authz.WithSystemBypass(ctx, "provider-quota-startup-migration")

	payload, err := svc.BackfillProviderQuotaStatusScopeKeys(ctx)
	if err != nil {
		log.Warn(ctx, "failed to backfill provider quota scope keys", log.Cause(err))
	} else if payload != nil && payload.UpdatedStatuses > 0 {
		log.Info(ctx, "backfilled provider quota scope keys",
			log.Int("scanned_statuses", payload.ScannedStatuses),
			log.Int("updated_statuses", payload.UpdatedStatuses),
			log.Int("skipped_statuses", payload.SkippedStatuses),
		)
	}

	svc.loadQuotaCache(ctx)
}

func (svc *ProviderQuotaService) BackfillProviderQuotaStatusScopeKeys(ctx context.Context) (*BackfillProviderQuotaStatusScopeKeysPayload, error) {
	db := svc.entFromContext(ctx)
	statuses, err := db.ProviderQuotaStatus.Query().
		Where(
			providerquotastatus.ScopeKey(providerQuotaLegacyChannelScopeKey),
			providerquotastatus.ChannelIDNotNil(),
		).
		Order(ent.Asc(providerquotastatus.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query legacy provider quota statuses: %w", err)
	}

	payload := &BackfillProviderQuotaStatusScopeKeysPayload{ScannedStatuses: len(statuses)}
	for _, status := range statuses {
		if status == nil || status.ChannelID <= 0 {
			payload.SkippedStatuses++
			continue
		}

		scopeKey := providerQuotaChannelScopeKey(status.ChannelID)
		if scopeKey == providerQuotaLegacyChannelScopeKey {
			payload.SkippedStatuses++
			continue
		}

		if _, err := db.ProviderQuotaStatus.UpdateOne(status).
			SetScopeKey(scopeKey).
			SetUpdatedAt(status.UpdatedAt).
			Save(ctx); err != nil {
			return payload, fmt.Errorf("failed to update provider quota status %d scope key: %w", status.ID, err)
		}
		payload.UpdatedStatuses++
	}

	return payload, nil
}

func (svc *ProviderQuotaService) loadQuotaCache(ctx context.Context) {
	records, err := svc.db.ProviderQuotaStatus.Query().
		Order(ent.Asc(providerquotastatus.FieldUpdatedAt), ent.Asc(providerquotastatus.FieldID)).
		All(ctx)
	if err != nil {
		log.Error(ctx, "Failed to load quota cache from DB", log.Cause(err))
		return
	}

	for _, r := range records {
		status := &QuotaChannelStatus{
			Status:                r.Status,
			Ready:                 r.Ready,
			CredentialID:          r.CredentialID,
			CredentialFingerprint: r.CredentialFingerprint,
			SecretFingerprint:     r.SecretFingerprint,
			ResourceScopeKey:      r.ResourceScopeKey,
			QuotaScopeID:          r.QuotaScopeID,
			Limits:                extractLimitsFromQuotaData(r.QuotaData),
		}
		if channelID, ok := providerQuotaChannelIDForStatus(r); ok {
			svc.quotaCache.Store(channelID, status)
		}
		if r.CredentialID > 0 {
			svc.credentialIDCache.Store(r.CredentialID, status)
		}
		if r.CredentialFingerprint != "" {
			svc.credentialQuotaCache.Store(r.CredentialFingerprint, status)
		}
		if r.ResourceScopeKey != "" {
			svc.resourceScopeCache.Store(r.ResourceScopeKey, status)
		}
		for _, credentialStatus := range extractCredentialQuotaStatusesFromQuotaData(r.QuotaData) {
			if credentialStatus.CredentialID > 0 {
				svc.credentialIDCache.Store(credentialStatus.CredentialID, credentialStatus)
			}
			if credentialStatus.CredentialFingerprint != "" {
				svc.credentialQuotaCache.Store(credentialStatus.CredentialFingerprint, credentialStatus)
			}
			if credentialStatus.ResourceScopeKey != "" {
				svc.resourceScopeCache.Store(credentialStatus.ResourceScopeKey, credentialStatus)
			}
		}
	}

	log.Debug(ctx, "Loaded quota cache from DB", log.Int("records", len(records)))
}

func (svc *ProviderQuotaService) GetQuotaStatus(channelID int) *QuotaChannelStatus {
	val, ok := svc.quotaCache.Load(channelID)
	if !ok {
		return nil
	}

	status, ok := val.(*QuotaChannelStatus)
	if !ok {
		return nil
	}

	return status
}

func (svc *ProviderQuotaService) GetCredentialQuotaStatusByID(credentialID int) *QuotaChannelStatus {
	if credentialID <= 0 {
		return nil
	}

	val, ok := svc.credentialIDCache.Load(credentialID)
	if !ok {
		return nil
	}

	status, ok := val.(*QuotaChannelStatus)
	if !ok {
		return nil
	}

	return status
}

func (svc *ProviderQuotaService) GetCredentialQuotaStatus(fingerprint string) *QuotaChannelStatus {
	if fingerprint == "" {
		return nil
	}

	val, ok := svc.credentialQuotaCache.Load(fingerprint)
	if !ok {
		return nil
	}

	status, ok := val.(*QuotaChannelStatus)
	if !ok {
		return nil
	}

	return status
}

func (svc *ProviderQuotaService) GetResourceScopeQuotaStatus(resourceScopeKey string) *QuotaChannelStatus {
	if resourceScopeKey == "" {
		return nil
	}

	val, ok := svc.resourceScopeCache.Load(resourceScopeKey)
	if !ok {
		return nil
	}

	status, ok := val.(*QuotaChannelStatus)
	if !ok {
		return nil
	}

	return status
}

func (svc *ProviderQuotaService) updateQuotaCache(channelID int, status providerquotastatus.Status, ready bool, limits []provider_quota.QuotaLimitStatus) {
	svc.updateQuotaCacheForCredentialTarget(channelID, quotaCredentialTarget{}, status, ready, limits)
}

func (svc *ProviderQuotaService) updateQuotaCacheForCredential(channelID int, fingerprint string, status providerquotastatus.Status, ready bool, limits []provider_quota.QuotaLimitStatus) {
	svc.updateQuotaCacheForCredentialTarget(channelID, quotaCredentialTarget{
		CredentialFingerprint: fingerprint,
	}, status, ready, limits)
}

func (svc *ProviderQuotaService) updateQuotaCacheForCredentialIdentity(channelID int, credentialID int, fingerprint string, status providerquotastatus.Status, ready bool, limits []provider_quota.QuotaLimitStatus) {
	svc.updateQuotaCacheForCredentialTarget(channelID, quotaCredentialTarget{
		CredentialID:          credentialID,
		CredentialFingerprint: fingerprint,
	}, status, ready, limits)
}

func (svc *ProviderQuotaService) storeCredentialQuotaStatus(fingerprint string, status providerquotastatus.Status, ready bool, limits []provider_quota.QuotaLimitStatus) {
	svc.storeCredentialQuotaStatusForIdentity(0, fingerprint, status, ready, limits)
}

func (svc *ProviderQuotaService) storeCredentialQuotaStatusForIdentity(credentialID int, fingerprint string, status providerquotastatus.Status, ready bool, limits []provider_quota.QuotaLimitStatus) {
	svc.storeCredentialQuotaStatusForTarget(quotaCredentialTarget{
		CredentialID:          credentialID,
		CredentialFingerprint: fingerprint,
	}, status, ready, limits)
}

func (svc *ProviderQuotaService) updateQuotaCacheForCredentialTarget(channelID int, target quotaCredentialTarget, status providerquotastatus.Status, ready bool, limits []provider_quota.QuotaLimitStatus) {
	quotaStatus := quotaChannelStatusForTarget(target, status, ready, limits)
	if !target.hasIdentity() {
		svc.quotaCache.Store(channelID, quotaStatus)
	}
	svc.storeCredentialQuotaStatusForTarget(target, status, ready, limits)
}

func (svc *ProviderQuotaService) storeCredentialQuotaStatusForTarget(target quotaCredentialTarget, status providerquotastatus.Status, ready bool, limits []provider_quota.QuotaLimitStatus) {
	if !target.hasIdentity() {
		return
	}
	quotaStatus := quotaChannelStatusForTarget(target, status, ready, limits)
	if target.CredentialID > 0 {
		svc.credentialIDCache.Store(target.CredentialID, quotaStatus)
	}
	if target.CredentialFingerprint != "" {
		svc.credentialQuotaCache.Store(target.CredentialFingerprint, quotaStatus)
	}
	if target.ResourceScopeKey != "" {
		svc.resourceScopeCache.Store(target.ResourceScopeKey, quotaStatus)
	}
}

func quotaChannelStatusForTarget(target quotaCredentialTarget, status providerquotastatus.Status, ready bool, limits []provider_quota.QuotaLimitStatus) *QuotaChannelStatus {
	return &QuotaChannelStatus{
		Status:                status,
		Ready:                 ready,
		CredentialID:          target.CredentialID,
		CredentialFingerprint: target.CredentialFingerprint,
		SecretFingerprint:     target.SecretFingerprint,
		ResourceScopeKey:      target.ResourceScopeKey,
		QuotaScopeID:          target.QuotaScopeID,
		Limits:                limits,
	}
}

// ManualCheck forces an immediate quota check for all relevant channels.
func (svc *ProviderQuotaService) ManualCheck(ctx context.Context) {
	svc.runQuotaCheckForce(ctx)
}

func (svc *ProviderQuotaService) runQuotaCheckForce(ctx context.Context) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	svc.runQuotaCheck(ctx, true)
}

func (svc *ProviderQuotaService) runQuotaCheck(ctx context.Context, force bool) {
	ctx = ent.NewContext(ctx, svc.db)

	now := time.Now()
	log.Debug(ctx, "Checking for channels to poll",
		log.Time("now", now),
		log.String("now_formatted", now.Format(time.RFC3339)),
		log.Bool("force", force),
	)

	q := svc.db.Channel.Query().
		Where(
			channel.StatusEQ(channel.StatusEnabled),
			channel.TypeIn(channel.TypeClaudecode, channel.TypeCodex, channel.TypeGithubCopilot, channel.TypeNanogpt, channel.TypeNanogptResponses, channel.TypeOpenai, channel.TypeOpenaiResponses),
		)

	if !force {
		q = q.Where(
			channel.Or(
				channel.Not(channel.HasProviderQuotaStatuses()),
				channel.HasProviderQuotaStatusesWith(
					providerquotastatus.NextCheckAtLTE(now),
				),
			),
		)
	}

	channelsToCheck, err := q.
		WithProviderQuotaStatuses().
		WithCredentialRefs(func(q *ent.ChannelCredentialRefQuery) {
			q.WithCredential(func(q *ent.UpstreamCredentialQuery) {
				q.WithQuotaScope()
			})
		}).
		All(ctx)
	if err != nil {
		log.Error(ctx, "Failed to query channels for quota check", log.Cause(err))
		return
	}

	if len(channelsToCheck) == 0 {
		log.Debug(ctx, "No channels need quota check at this time")
		return
	}

	log.Info(ctx, "Running quota check",
		log.Int("channels", len(channelsToCheck)),
		log.Bool("force", force),
	)

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(min(maxConcurrentQuotaChecks, len(channelsToCheck)))
	for _, ch := range channelsToCheck {
		ch := ch
		eg.Go(func() error {
			svc.checkChannelQuota(egCtx, ch, now)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		log.Info(ctx, "quota check group interrupted", log.Cause(err))
	}
}

func (svc *ProviderQuotaService) checkChannelQuota(ctx context.Context, ch *ent.Channel, now time.Time) {
	providerType := svc.getProviderType(ch)
	if providerType == "" {
		return
	}

	if !hasCredentialsForProvider(ch) {
		log.Debug(ctx, "channel does not support check quota", log.Int("channel_id", ch.ID), log.String("channel_name", ch.Name))
		return
	}

	checker, ok := svc.checkers[providerType]
	if !ok {
		log.Error(ctx, "No checker for provider",
			log.String("provider", providerType),
			log.Int("channel_id", ch.ID))

		return
	}

	apiKeyCredentialViews := quotaCheckAPIKeyCredentialViews(ch, providerType)
	if len(apiKeyCredentialViews) > 1 {
		quotaData, err := svc.checkAPIKeyCredentialQuotas(ctx, checker, ch, apiKeyCredentialViews, providerType, now)
		if err != nil {
			log.Error(ctx, "Credential quota check failed",
				log.Int("channel_id", ch.ID),
				log.String("channel_name", ch.Name),
				log.String("provider", providerType),
				log.Cause(err))

			svc.saveQuotaError(ctx, ch, providerType, err, now)
			return
		}

		svc.saveQuotaStatus(ctx, ch.ID, quotaCredentialTarget{}, providerType, quotaData, now)

		log.Debug(ctx, "Updated credential-derived quota status",
			log.Int("channel_id", ch.ID),
			log.String("provider", providerType),
			log.String("status", quotaData.Status),
			log.Bool("ready", quotaData.Ready))

		return
	}

	// Make quota check request
	quotaChannel := ch
	target := quotaCredentialTarget{}
	if len(apiKeyCredentialViews) == 1 {
		view := apiKeyCredentialViews[0]
		quotaChannel = quotaChannelForCredentialView(ch, view)
		target = quotaCredentialTargetFromView(view)
	} else {
		target = quotaCredentialIdentity(ch)
		if target.hasIdentity() {
			if view, ok := onlyEnabledCredentialView(ch); ok {
				quotaChannel = quotaChannelForCredentialView(ch, view)
			}
		}
	}

	// Make quota check request
	quotaData, err := checker.CheckQuota(ctx, quotaChannel)
	if err != nil {
		log.Error(ctx, "Quota check failed",
			log.Int("channel_id", ch.ID),
			log.String("channel_name", ch.Name),
			log.String("provider", providerType),
			log.Cause(err))

		svc.saveQuotaError(ctx, ch, providerType, err, now)
		return
	}

	// Save quota status
	svc.saveQuotaStatus(ctx, ch.ID, target, providerType, quotaData, now)

	log.Debug(ctx, "Updated quota status",
		log.Int("channel_id", ch.ID),
		log.String("credential_fingerprint", target.CredentialFingerprint),
		log.String("resource_scope_key", target.ResourceScopeKey),
		log.String("provider", providerType),
		log.String("status", quotaData.Status),
		log.Bool("ready", quotaData.Ready))
}

type credentialQuotaResult struct {
	target quotaCredentialTarget
	data   provider_quota.QuotaData
	err    error
}

type quotaCredentialTarget struct {
	CredentialID          int
	CredentialFingerprint string
	SecretFingerprint     string
	ResourceScopeKey      string
	QuotaScopeID          int
}

func (t quotaCredentialTarget) hasIdentity() bool {
	return t.CredentialID > 0 ||
		t.CredentialFingerprint != "" ||
		t.SecretFingerprint != "" ||
		t.ResourceScopeKey != ""
}

func providerQuotaScopeKey(channelID int, target quotaCredentialTarget) string {
	switch {
	case strings.TrimSpace(target.ResourceScopeKey) != "":
		return "resource:" + strings.TrimSpace(target.ResourceScopeKey)
	case target.CredentialID > 0:
		return fmt.Sprintf("credential_id:%d", target.CredentialID)
	case strings.TrimSpace(target.SecretFingerprint) != "":
		return "secret:" + strings.TrimSpace(target.SecretFingerprint)
	case strings.TrimSpace(target.CredentialFingerprint) != "":
		return "credential:" + strings.TrimSpace(target.CredentialFingerprint)
	default:
		return providerQuotaChannelScopeKey(channelID)
	}
}

func providerQuotaChannelScopeKey(channelID int) string {
	if channelID <= 0 {
		return providerQuotaLegacyChannelScopeKey
	}

	return providerQuotaChannelScopePrefix + strconv.Itoa(channelID)
}

func ProviderQuotaChannelScopeKey(channelID int) string {
	return providerQuotaChannelScopeKey(channelID)
}

func providerQuotaChannelIDForStatus(status *ent.ProviderQuotaStatus) (int, bool) {
	if status == nil {
		return 0, false
	}
	if strings.TrimSpace(status.ScopeKey) == providerQuotaLegacyChannelScopeKey {
		return status.ChannelID, status.ChannelID > 0
	}
	if !strings.HasPrefix(status.ScopeKey, providerQuotaChannelScopePrefix) {
		return 0, false
	}

	id, err := strconv.Atoi(strings.TrimPrefix(status.ScopeKey, providerQuotaChannelScopePrefix))
	if err != nil || id <= 0 {
		return 0, false
	}

	return id, true
}

func quotaCredentialTargetFromView(view ChannelCredentialView) quotaCredentialTarget {
	return quotaCredentialTarget{
		CredentialID:          view.CredentialID,
		CredentialFingerprint: view.Fingerprint,
		SecretFingerprint:     view.SecretFingerprint,
		ResourceScopeKey:      view.ResourceScopeKey,
		QuotaScopeID:          view.QuotaScopeID,
	}
}

func (svc *ProviderQuotaService) checkAPIKeyCredentialQuotas(
	ctx context.Context,
	checker provider_quota.QuotaChecker,
	ch *ent.Channel,
	views []ChannelCredentialView,
	providerType string,
	now time.Time,
) (provider_quota.QuotaData, error) {
	results := make([]credentialQuotaResult, 0, len(views))
	successes := 0

	for _, view := range views {
		credentialChannel := quotaChannelForCredentialView(ch, view)

		quotaData, err := checker.CheckQuota(ctx, credentialChannel)
		target := quotaCredentialTargetFromView(view)
		result := credentialQuotaResult{
			target: target,
			data:   quotaData,
			err:    err,
		}
		results = append(results, result)

		if err != nil {
			errorQuotaData := provider_quota.QuotaData{
				Status: "unknown",
				Ready:  false,
				RawData: map[string]any{
					"error": err.Error(),
				},
			}
			svc.saveQuotaStatus(ctx, ch.ID, target, providerType, errorQuotaData, now)
			svc.persistCredentialQuotaObservation(ctx, target, errorQuotaData, err)
			log.Warn(ctx, "Credential quota check failed",
				log.Int("channel_id", ch.ID),
				log.Int("credential_id", view.CredentialID),
				log.String("credential_fingerprint", view.Fingerprint),
				log.Cause(err))
			continue
		}

		successes++
		svc.saveQuotaStatus(ctx, ch.ID, target, providerType, quotaData, now)
	}

	if successes == 0 {
		return provider_quota.QuotaData{}, fmt.Errorf("all credential quota checks failed")
	}

	return aggregateCredentialQuotaData(results), nil
}

func quotaChannelForCredentialView(ch *ent.Channel, view ChannelCredentialView) *ent.Channel {
	clone := *ch
	clone.Credentials = view.Secret.ToChannelCredentials()
	clone.DisabledAPIKeys = nil
	return &clone
}

func quotaCheckAPIKeyCredentialViews(ch *ent.Channel, providerType string) []ChannelCredentialView {
	if ch == nil || ch.Credentials.IsOAuth() {
		return nil
	}

	switch providerType {
	case "nanogpt", "wafer", "synthetic", "neuralwatt":
	default:
		return nil
	}

	views := enabledAPIKeyCredentialViews(channelCredentialViews(ch))
	if len(views) <= 1 {
		return views
	}

	seen := make(map[string]struct{}, len(views))
	deduped := make([]ChannelCredentialView, 0, len(views))
	for _, view := range views {
		key := view.Fingerprint
		if view.CredentialID > 0 {
			key = fmt.Sprintf("id:%d", view.CredentialID)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, view)
	}

	return deduped
}

func aggregateCredentialQuotaData(results []credentialQuotaResult) provider_quota.QuotaData {
	counts := map[string]int{}
	var limitBuckets = map[provider_quota.QuotaLimitType][]provider_quota.QuotaLimitStatus{}
	var nextResetAt *time.Time
	credentialStatuses := make([]map[string]any, 0, len(results))

	for _, result := range results {
		status := "unknown"
		ready := false
		if result.err == nil {
			status = result.data.Status
			ready = result.data.Ready
		}

		counts[status]++

		credentialStatus := map[string]any{
			"fingerprint": result.target.CredentialFingerprint,
			"status":      status,
			"ready":       ready,
		}
		if result.target.CredentialID > 0 {
			credentialStatus["credential_id"] = result.target.CredentialID
		}
		if result.target.SecretFingerprint != "" {
			credentialStatus["secret_fingerprint"] = result.target.SecretFingerprint
		}
		if result.target.ResourceScopeKey != "" {
			credentialStatus["resource_scope_key"] = result.target.ResourceScopeKey
		}
		if result.target.QuotaScopeID > 0 {
			credentialStatus["quota_scope_id"] = result.target.QuotaScopeID
		}
		if result.err != nil {
			credentialStatus["error"] = result.err.Error()
		} else if len(result.data.Limits) > 0 {
			credentialStatus["limits"] = quotaLimitStatusMaps(result.data.Limits)
		}
		credentialStatuses = append(credentialStatuses, credentialStatus)

		if result.err == nil {
			for _, limit := range result.data.Limits {
				limitBuckets[limit.Type] = append(limitBuckets[limit.Type], limit)
			}
			if result.data.NextResetAt != nil && (nextResetAt == nil || result.data.NextResetAt.Before(*nextResetAt)) {
				nextResetAt = result.data.NextResetAt
			}
		}
	}

	status := aggregateQuotaStatus(counts)
	limits := make([]provider_quota.QuotaLimitStatus, 0, len(limitBuckets))
	for limitType, bucket := range limitBuckets {
		limits = append(limits, aggregateQuotaLimitStatus(limitType, bucket))
	}

	return provider_quota.QuotaData{
		Status:      status,
		Ready:       provider_quota.IsReadyStatus(status),
		NextResetAt: nextResetAt,
		Limits:      limits,
		RawData: map[string]any{
			"credential_count":         len(results),
			"credential_status_counts": counts,
			"credential_statuses":      credentialStatuses,
		},
	}
}

func aggregateQuotaStatus(counts map[string]int) string {
	for _, status := range []string{"warning", "available", "unknown", "exhausted"} {
		if counts[status] > 0 {
			return status
		}
	}
	return "unknown"
}

func aggregateQuotaLimitStatus(limitType provider_quota.QuotaLimitType, bucket []provider_quota.QuotaLimitStatus) provider_quota.QuotaLimitStatus {
	counts := map[string]int{}
	var usageRatio float64
	var nextResetAt *time.Time

	for _, limit := range bucket {
		counts[limit.Status]++
		if limit.UsageRatio > usageRatio {
			usageRatio = limit.UsageRatio
		}
		if limit.NextResetAt != nil && (nextResetAt == nil || limit.NextResetAt.Before(*nextResetAt)) {
			nextResetAt = limit.NextResetAt
		}
	}

	status := aggregateQuotaStatus(counts)
	return provider_quota.QuotaLimitStatus{
		Type:        limitType,
		Status:      status,
		UsageRatio:  usageRatio,
		Ready:       provider_quota.IsReadyStatus(status),
		NextResetAt: nextResetAt,
	}
}

func (svc *ProviderQuotaService) persistCredentialQuotaObservation(ctx context.Context, target quotaCredentialTarget, quotaData provider_quota.QuotaData, quotaErr error) {
	status := strings.TrimSpace(quotaData.Status)
	if status == "" {
		status = string(providerquotastatus.StatusUnknown)
	}

	lastError := ""
	if quotaErr != nil {
		lastError = quotaErr.Error()
	}

	if target.CredentialID > 0 {
		update := svc.db.UpstreamCredential.UpdateOneID(target.CredentialID).
			SetQuotaStatus(status)
		if lastError != "" {
			update.SetLastError(lastError)
		} else {
			update.SetLastError("")
		}
		if _, err := update.Save(ctx); err != nil {
			log.Warn(ctx, "Failed to update credential quota observation",
				log.Int("credential_id", target.CredentialID),
				log.Cause(err))
		}
	}

}

func (svc *ProviderQuotaService) saveQuotaStatus(
	ctx context.Context,
	channelID int,
	target quotaCredentialTarget,
	providerType string,
	quotaData provider_quota.QuotaData,
	now time.Time,
) {
	nextCheck := now.Add(svc.nextCheckIntervalForStatus(providerquotastatus.Status(quotaData.Status)))
	pt := providerquotastatus.ProviderType(providerType)
	scopeKey := providerQuotaScopeKey(channelID, target)

	existing, err := svc.db.ProviderQuotaStatus.Query().
		Where(
			providerquotastatus.ProviderTypeEQ(pt),
			providerquotastatus.ScopeKey(scopeKey),
		).
		Order(ent.Desc(providerquotastatus.FieldUpdatedAt)).
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		log.Error(ctx, "Failed to query quota status",
			log.Int("channel_id", channelID),
			log.String("scope_key", scopeKey),
			log.Cause(err))
		return
	}

	if existing != nil {
		update := svc.db.ProviderQuotaStatus.UpdateOne(existing).
			SetChannelID(channelID).
			SetStatus(providerquotastatus.Status(quotaData.Status)).
			SetQuotaData(svc.mergeLimitsIntoQuotaData(quotaData)).
			SetNextCheckAt(nextCheck).
			SetReady(quotaData.Ready)
		if quotaData.NextResetAt != nil {
			update.SetNextResetAt(*quotaData.NextResetAt)
		} else {
			update.ClearNextResetAt()
		}
		applyProviderQuotaStatusTargetUpdate(update, target)
		if _, err := update.Save(ctx); err != nil {
			log.Error(ctx, "Failed to update quota status",
				log.Int("channel_id", channelID),
				log.String("scope_key", scopeKey),
				log.Cause(err))
			return
		}
	} else {
		create := svc.db.ProviderQuotaStatus.Create().
			SetChannelID(channelID).
			SetScopeKey(scopeKey).
			SetProviderType(pt).
			SetStatus(providerquotastatus.Status(quotaData.Status)).
			SetQuotaData(svc.mergeLimitsIntoQuotaData(quotaData)).
			SetNextCheckAt(nextCheck).
			SetReady(quotaData.Ready)
		if quotaData.NextResetAt != nil {
			create.SetNextResetAt(*quotaData.NextResetAt)
		}
		applyProviderQuotaStatusTargetCreate(create, target)
		if err := create.Exec(ctx); err != nil {
			log.Error(ctx, "Failed to save quota status",
				log.Int("channel_id", channelID),
				log.String("scope_key", scopeKey),
				log.Cause(err))
			return
		}
	}

	limits := quotaData.Limits
	if len(limits) == 0 {
		limits = extractLimitsFromQuotaData(svc.mergeLimitsIntoQuotaData(quotaData))
	}
	svc.updateQuotaCacheForCredentialTarget(channelID, target, providerquotastatus.Status(quotaData.Status), quotaData.Ready, limits)
	svc.persistCredentialQuotaObservation(ctx, target, quotaData, nil)
}

func applyProviderQuotaStatusTargetCreate(create *ent.ProviderQuotaStatusCreate, target quotaCredentialTarget) {
	if target.CredentialID > 0 {
		create.SetCredentialID(target.CredentialID)
	}
	if target.CredentialFingerprint != "" {
		create.SetCredentialFingerprint(target.CredentialFingerprint)
	}
	if target.SecretFingerprint != "" {
		create.SetSecretFingerprint(target.SecretFingerprint)
	}
	if target.ResourceScopeKey != "" {
		create.SetResourceScopeKey(target.ResourceScopeKey)
	}
	if target.QuotaScopeID > 0 {
		create.SetQuotaScopeID(target.QuotaScopeID)
	}
}

func applyProviderQuotaStatusTargetUpdate(update *ent.ProviderQuotaStatusUpdateOne, target quotaCredentialTarget) {
	if target.CredentialID > 0 {
		update.SetCredentialID(target.CredentialID)
	} else {
		update.ClearCredentialID()
	}
	if target.CredentialFingerprint != "" {
		update.SetCredentialFingerprint(target.CredentialFingerprint)
	} else {
		update.ClearCredentialFingerprint()
	}
	if target.SecretFingerprint != "" {
		update.SetSecretFingerprint(target.SecretFingerprint)
	} else {
		update.ClearSecretFingerprint()
	}
	if target.ResourceScopeKey != "" {
		update.SetResourceScopeKey(target.ResourceScopeKey)
	} else {
		update.ClearResourceScopeKey()
	}
	if target.QuotaScopeID > 0 {
		update.SetQuotaScopeID(target.QuotaScopeID)
	} else {
		update.ClearQuotaScopeID()
	}
}

func (svc *ProviderQuotaService) saveQuotaError(
	ctx context.Context,
	ch *ent.Channel,
	providerType string,
	quotaErr error,
	now time.Time,
) {
	pt := providerquotastatus.ProviderType(providerType)
	nextCheck := now.Add(svc.getCheckInterval())
	target := quotaCredentialIdentity(ch)
	scopeKey := providerQuotaScopeKey(ch.ID, target)

	existing, err := svc.db.ProviderQuotaStatus.Query().
		Where(
			providerquotastatus.ProviderTypeEQ(pt),
			providerquotastatus.ScopeKey(scopeKey),
		).
		Order(ent.Desc(providerquotastatus.FieldUpdatedAt)).
		First(ctx)
	if err == nil && existing != nil {
		existingData := existing.QuotaData
		if existingData == nil {
			existingData = map[string]any{}
		}

		merged := lo.Assign(existingData, map[string]any{
			"error": quotaErr.Error(),
		})

		update := svc.db.ProviderQuotaStatus.UpdateOne(existing).
			SetChannelID(ch.ID).
			SetQuotaData(merged).
			SetStatus(providerquotastatus.StatusUnknown).
			SetReady(false).
			SetNextCheckAt(nextCheck)
		if target.CredentialID > 0 {
			update.SetCredentialID(target.CredentialID)
		} else {
			update.ClearCredentialID()
		}
		if target.CredentialFingerprint != "" {
			update.SetCredentialFingerprint(target.CredentialFingerprint)
		} else {
			update.ClearCredentialFingerprint()
		}
		if target.SecretFingerprint != "" {
			update.SetSecretFingerprint(target.SecretFingerprint)
		} else {
			update.ClearSecretFingerprint()
		}
		if target.ResourceScopeKey != "" {
			update.SetResourceScopeKey(target.ResourceScopeKey)
		} else {
			update.ClearResourceScopeKey()
		}
		if target.QuotaScopeID > 0 {
			update.SetQuotaScopeID(target.QuotaScopeID)
		} else {
			update.ClearQuotaScopeID()
		}

		err := update.Exec(ctx)
		if err != nil {
			log.Error(ctx, "Failed to save quota error",
				log.Int("channel_id", ch.ID),
				log.String("scope_key", scopeKey),
				log.Cause(err))
			return
		}

		existingLimits := extractLimitsFromQuotaData(existing.QuotaData)
		svc.updateQuotaCacheForCredentialTarget(ch.ID, target, providerquotastatus.StatusUnknown, false, existingLimits)
		svc.persistCredentialQuotaObservation(ctx, target, provider_quota.QuotaData{
			Status: string(providerquotastatus.StatusUnknown),
			Ready:  false,
		}, quotaErr)

		return
	}
	if err != nil && !ent.IsNotFound(err) {
		log.Error(ctx, "Failed to query quota error row",
			log.Int("channel_id", ch.ID),
			log.String("scope_key", scopeKey),
			log.Cause(err))
		return
	}

	create := svc.db.ProviderQuotaStatus.Create().
		SetChannelID(ch.ID).
		SetScopeKey(scopeKey).
		SetProviderType(pt).
		SetStatus(providerquotastatus.StatusUnknown).
		SetReady(false).
		SetQuotaData(map[string]any{
			"error": quotaErr.Error(),
		}).
		SetNextCheckAt(nextCheck)
	if target.CredentialID > 0 {
		create.SetCredentialID(target.CredentialID)
	}
	if target.CredentialFingerprint != "" {
		create.SetCredentialFingerprint(target.CredentialFingerprint)
	}
	if target.SecretFingerprint != "" {
		create.SetSecretFingerprint(target.SecretFingerprint)
	}
	if target.ResourceScopeKey != "" {
		create.SetResourceScopeKey(target.ResourceScopeKey)
	}
	if target.QuotaScopeID > 0 {
		create.SetQuotaScopeID(target.QuotaScopeID)
	}

	err = create.Exec(ctx)
	if err != nil {
		log.Error(ctx, "Failed to save quota error",
			log.Int("channel_id", ch.ID),
			log.String("scope_key", scopeKey),
			log.Cause(err))
		return
	}

	svc.updateQuotaCacheForCredentialTarget(ch.ID, target, providerquotastatus.StatusUnknown, false, nil)
	svc.persistCredentialQuotaObservation(ctx, target, provider_quota.QuotaData{
		Status: string(providerquotastatus.StatusUnknown),
		Ready:  false,
	}, quotaErr)
}

func quotaCredentialIdentity(ch *ent.Channel) quotaCredentialTarget {
	if ch == nil {
		return quotaCredentialTarget{}
	}

	if view, ok := onlyEnabledCredentialView(ch); ok {
		return quotaCredentialTargetFromView(view)
	}

	return quotaCredentialTarget{}
}

func onlyEnabledCredentialView(ch *ent.Channel) (ChannelCredentialView, bool) {
	views := channelCredentialViews(ch)
	enabled := make([]ChannelCredentialView, 0, len(views))
	for _, view := range views {
		if view.Enabled {
			enabled = append(enabled, view)
		}
	}
	if len(enabled) != 1 {
		return ChannelCredentialView{}, false
	}

	return enabled[0], true
}

func (svc *ProviderQuotaService) getProviderType(ch *ent.Channel) string {
	switch ch.Type { //nolint:exhaustive
	case channel.TypeClaudecode:
		return "claudecode"
	case channel.TypeCodex:
		return "codex"
	case channel.TypeGithubCopilot:
		return "github_copilot"
	case channel.TypeNanogpt, channel.TypeNanogptResponses:
		return "nanogpt"
	case channel.TypeOpenai, channel.TypeOpenaiResponses:
		return provider_quota.DetectProviderFromURL(ch.BaseURL)
	default:
		return ""
	}
}

func hasCredentialsForProvider(ch *ent.Channel) bool {
	views := channelCredentialViews(ch)
	if len(views) == 0 {
		return false
	}

	if ch.Type == channel.TypeOpenai || ch.Type == channel.TypeOpenaiResponses {
		providerType := provider_quota.DetectProviderFromURL(ch.BaseURL)
		if _, ok := provider_quota.URLDetectedProviders()[providerType]; ok {
			return credentialViewsContainAPIKey(views)
		}
	}

	if ch.Type == channel.TypeCodex || ch.Type == channel.TypeClaudecode {
		return credentialViewsContainOAuth(views)
	}

	return credentialViewsContainAnySecret(views)
}

func credentialViewsContainAPIKey(views []ChannelCredentialView) bool {
	for _, view := range views {
		if !view.Enabled {
			continue
		}
		if normalizeCredentialFingerprintPart(view.AuthKind) != channelCredentialAuthKindAPIKey {
			continue
		}
		if strings.TrimSpace(view.Secret.APIKey) != "" {
			return true
		}
	}

	return false
}

func credentialViewsContainOAuth(views []ChannelCredentialView) bool {
	for _, view := range views {
		if !view.Enabled {
			continue
		}
		if normalizeCredentialFingerprintPart(view.AuthKind) != channelCredentialAuthKindOAuth {
			continue
		}
		if view.Secret.OAuth != nil || isOAuthJSON(view.Secret.APIKey) {
			return true
		}
	}

	return false
}

func credentialViewsContainAnySecret(views []ChannelCredentialView) bool {
	for _, view := range views {
		if !view.Enabled {
			continue
		}
		if strings.TrimSpace(view.Secret.APIKey) != "" || view.Secret.OAuth != nil || view.Secret.GCP != nil || view.Secret.Azure != nil {
			return true
		}
	}

	return false
}

func (svc *ProviderQuotaService) mergeLimitsIntoQuotaData(quotaData provider_quota.QuotaData) map[string]any {
	data := lo.Assign(map[string]any{}, quotaData.RawData)

	if len(quotaData.Limits) > 0 {
		data["_limits"] = quotaLimitStatusMaps(quotaData.Limits)
	}

	return data
}

func quotaLimitStatusMaps(limits []provider_quota.QuotaLimitStatus) []map[string]any {
	limitMaps := make([]map[string]any, 0, len(limits))
	for _, l := range limits {
		m := map[string]any{
			"type":       string(l.Type),
			"status":     l.Status,
			"usageRatio": l.UsageRatio,
			"ready":      l.Ready,
		}
		if l.NextResetAt != nil {
			m["nextResetAt"] = l.NextResetAt.Format(time.RFC3339)
		}
		limitMaps = append(limitMaps, m)
	}
	return limitMaps
}

func extractLimitsFromQuotaData(data map[string]any) []provider_quota.QuotaLimitStatus {
	rawLimits, ok := data["_limits"]
	if !ok {
		return nil
	}

	return extractLimitsFromRaw(rawLimits)
}

func extractLimitsFromRaw(rawLimits any) []provider_quota.QuotaLimitStatus {
	// Handle both []map[string]any (from mergeLimitsIntoQuotaData) and []any (from JSON unmarshaling)
	var limitMaps []map[string]any
	if directMaps, ok := rawLimits.([]map[string]any); ok {
		limitMaps = directMaps
	} else if anySlice, ok := rawLimits.([]any); ok {
		limitMaps = make([]map[string]any, 0, len(anySlice))
		for _, raw := range anySlice {
			if m, ok := raw.(map[string]any); ok {
				limitMaps = append(limitMaps, m)
			}
		}
	} else {
		return nil
	}

	var limits []provider_quota.QuotaLimitStatus

	for _, m := range limitMaps {
		ls := provider_quota.QuotaLimitStatus{}

		if t, ok := m["type"].(string); ok {
			ls.Type = provider_quota.QuotaLimitType(t)
		}

		if s, ok := m["status"].(string); ok {
			ls.Status = s
		}

		if u, ok := m["usageRatio"].(float64); ok {
			ls.UsageRatio = u
		}

		if r, ok := m["ready"].(bool); ok {
			ls.Ready = r
		}

		if ts, ok := m["nextResetAt"].(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				ls.NextResetAt = &t
			}
		}

		limits = append(limits, ls)
	}

	return limits
}

func extractCredentialQuotaStatusesFromQuotaData(data map[string]any) []*QuotaChannelStatus {
	rawStatuses, ok := data["credential_statuses"]
	if !ok {
		return nil
	}

	var statusMaps []map[string]any
	if directMaps, ok := rawStatuses.([]map[string]any); ok {
		statusMaps = directMaps
	} else if anySlice, ok := rawStatuses.([]any); ok {
		statusMaps = make([]map[string]any, 0, len(anySlice))
		for _, raw := range anySlice {
			if m, ok := raw.(map[string]any); ok {
				statusMaps = append(statusMaps, m)
			}
		}
	} else {
		return nil
	}

	result := make([]*QuotaChannelStatus, 0, len(statusMaps))
	for _, statusMap := range statusMaps {
		fingerprint, _ := statusMap["fingerprint"].(string)
		statusText, _ := statusMap["status"].(string)
		ready, _ := statusMap["ready"].(bool)
		credentialID := intFromAny(statusMap["credential_id"])
		secretFingerprint, _ := statusMap["secret_fingerprint"].(string)
		resourceScopeKey, _ := statusMap["resource_scope_key"].(string)
		quotaScopeID := intFromAny(statusMap["quota_scope_id"])
		if (credentialID <= 0 && fingerprint == "" && secretFingerprint == "" && resourceScopeKey == "") || statusText == "" {
			continue
		}

		result = append(result, &QuotaChannelStatus{
			Status:                providerquotastatus.Status(statusText),
			Ready:                 ready,
			CredentialID:          credentialID,
			CredentialFingerprint: fingerprint,
			SecretFingerprint:     secretFingerprint,
			ResourceScopeKey:      resourceScopeKey,
			QuotaScopeID:          quotaScopeID,
			Limits:                extractLimitsFromRaw(statusMap["limits"]),
		})
	}

	return result
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
