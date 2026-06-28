package backup

import (
	"encoding/json"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/objects"
)

type BackupData struct {
	Version               string                        `json:"version"`
	Timestamp             time.Time                     `json:"timestamp"`
	Projects              []*BackupProject              `json:"projects,omitempty"`
	Channels              []*BackupChannel              `json:"channels"`
	CredentialQuotaScopes []*BackupCredentialQuotaScope `json:"credential_quota_scopes,omitempty"`
	UpstreamCredentials   []*BackupUpstreamCredential   `json:"upstream_credentials,omitempty"`
	ChannelCredentialRefs []*BackupChannelCredentialRef `json:"channel_credential_refs,omitempty"`
	Models                []*BackupModel                `json:"models"`
	ChannelModelPrices    []*BackupChannelModelPrice    `json:"channel_model_prices,omitempty"`
	APIKeys               []*BackupAPIKey               `json:"api_keys,omitempty"`
	UsageRequests         []*BackupUsageRequest         `json:"usage_requests,omitempty"`
	RequestExecutions     []*BackupRequestExecution     `json:"request_executions,omitempty"`
	UsageLogs             []*BackupUsageLog             `json:"usage_logs,omitempty"`
}

type BackupProject struct {
	ent.Project
}

type BackupChannel struct {
	ent.Channel
}

type BackupUpstreamCredential struct {
	ent.UpstreamCredential

	SecretPayload objects.UpstreamCredentialSecret `json:"secret_payload,omitempty"`
}

type BackupCredentialQuotaScope struct {
	ent.CredentialQuotaScope
}

type BackupChannelCredentialRef struct {
	ent.ChannelCredentialRef

	ChannelName           string `json:"channel_name"`
	CredentialFingerprint string `json:"credential_fingerprint"`
}

type BackupModel struct {
	ent.Model
}

type BackupAPIKey struct {
	ent.APIKey

	ProjectName string `json:"project_name"`
}

type BackupChannelModelPrice struct {
	ChannelName string             `json:"channel_name"`
	ModelID     string             `json:"model_id"`
	Price       objects.ModelPrice `json:"price"`
	ReferenceID string             `json:"reference_id"`
}

type BackupUsageRequest struct {
	ent.Request

	ProjectName string `json:"project_name,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`
	APIKeyKey   string `json:"api_key_key,omitempty"`
}

func (r BackupUsageRequest) MarshalJSON() ([]byte, error) {
	type requestData struct {
		ID                         int                      `json:"id,omitempty"`
		CreatedAt                  time.Time                `json:"created_at,omitzero"`
		UpdatedAt                  time.Time                `json:"updated_at,omitzero"`
		ProjectID                  int                      `json:"project_id,omitempty"`
		Source                     request.Source           `json:"source,omitempty"`
		ModelID                    string                   `json:"model_id,omitempty"`
		ReasoningEffort            string                   `json:"reasoning_effort,omitempty"`
		Format                     string                   `json:"format,omitempty"`
		RequestHeaders             objects.JSONRawMessage   `json:"request_headers,omitempty"`
		RequestBody                objects.JSONRawMessage   `json:"request_body,omitempty"`
		ResponseBody               objects.JSONRawMessage   `json:"response_body,omitempty"`
		ResponseChunks             []objects.JSONRawMessage `json:"response_chunks,omitempty"`
		ChannelID                  int                      `json:"channel_id,omitempty"`
		ExternalID                 string                   `json:"external_id,omitempty"`
		Status                     request.Status           `json:"status,omitempty"`
		Stream                     bool                     `json:"stream,omitempty"`
		ClientIP                   string                   `json:"client_ip,omitempty"`
		MetricsLatencyMs           *int64                   `json:"metrics_latency_ms,omitempty"`
		MetricsFirstTokenLatencyMs *int64                   `json:"metrics_first_token_latency_ms,omitempty"`
		MetricsReasoningDurationMs *int64                   `json:"metrics_reasoning_duration_ms,omitempty"`
		ContentSaved               bool                     `json:"content_saved,omitempty"`
		ContentStorageID           *int                     `json:"content_storage_id,omitempty"`
		ContentStorageKey          *string                  `json:"content_storage_key,omitempty"`
		ContentSavedAt             *time.Time               `json:"content_saved_at,omitempty"`
		ProjectName                string                   `json:"project_name,omitempty"`
		ChannelName                string                   `json:"channel_name,omitempty"`
		APIKeyKey                  string                   `json:"api_key_key,omitempty"`
	}

	return json.Marshal(requestData{
		ID:                         r.ID,
		CreatedAt:                  r.CreatedAt,
		UpdatedAt:                  r.UpdatedAt,
		ProjectID:                  r.ProjectID,
		Source:                     r.Source,
		ModelID:                    r.ModelID,
		ReasoningEffort:            r.ReasoningEffort,
		Format:                     r.Format,
		RequestHeaders:             r.RequestHeaders,
		RequestBody:                r.RequestBody,
		ResponseBody:               r.ResponseBody,
		ResponseChunks:             r.ResponseChunks,
		ChannelID:                  r.ChannelID,
		ExternalID:                 r.ExternalID,
		Status:                     r.Status,
		Stream:                     r.Stream,
		ClientIP:                   r.ClientIP,
		MetricsLatencyMs:           r.MetricsLatencyMs,
		MetricsFirstTokenLatencyMs: r.MetricsFirstTokenLatencyMs,
		MetricsReasoningDurationMs: r.MetricsReasoningDurationMs,
		ContentSaved:               r.ContentSaved,
		ContentStorageID:           r.ContentStorageID,
		ContentStorageKey:          r.ContentStorageKey,
		ContentSavedAt:             r.ContentSavedAt,
		ProjectName:                r.ProjectName,
		ChannelName:                r.ChannelName,
		APIKeyKey:                  r.APIKeyKey,
	})
}

type BackupRequestExecution struct {
	ent.RequestExecution

	ProjectName string `json:"project_name,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`
}

func (e BackupRequestExecution) MarshalJSON() ([]byte, error) {
	type requestExecutionData struct {
		ID                            int                      `json:"id,omitempty"`
		CreatedAt                     time.Time                `json:"created_at,omitzero"`
		UpdatedAt                     time.Time                `json:"updated_at,omitzero"`
		ProjectID                     int                      `json:"project_id,omitempty"`
		RequestID                     int                      `json:"request_id,omitempty"`
		ChannelID                     int                      `json:"channel_id,omitempty"`
		CredentialID                  int                      `json:"credential_id,omitempty"`
		DataStorageID                 int                      `json:"data_storage_id,omitempty"`
		ExternalID                    string                   `json:"external_id,omitempty"`
		ModelID                       string                   `json:"model_id,omitempty"`
		CredentialFingerprint         string                   `json:"credential_fingerprint,omitempty"`
		SecretFingerprint             string                   `json:"secret_fingerprint,omitempty"`
		ResourceScopeKey              string                   `json:"resource_scope_key,omitempty"`
		QuotaScopeID                  int                      `json:"quota_scope_id,omitempty"`
		QuotaScopeNameSnapshot        string                   `json:"quota_scope_name_snapshot,omitempty"`
		QuotaScopeStatusSnapshot      string                   `json:"quota_scope_status_snapshot,omitempty"`
		CredentialNameSnapshot        string                   `json:"credential_name_snapshot,omitempty"`
		CredentialKeyHint             string                   `json:"credential_key_hint,omitempty"`
		CredentialSource              string                   `json:"credential_source,omitempty"`
		CredentialQuotaStatusSnapshot string                   `json:"credential_quota_status_snapshot,omitempty"`
		Format                        string                   `json:"format,omitempty"`
		RequestURL                    string                   `json:"request_url,omitempty"`
		PassThroughApplied            bool                     `json:"pass_through_applied,omitempty"`
		RequestHeaders                objects.JSONRawMessage   `json:"request_headers,omitempty"`
		RequestBody                   objects.JSONRawMessage   `json:"request_body,omitempty"`
		ResponseBody                  objects.JSONRawMessage   `json:"response_body,omitempty"`
		ResponseChunks                []objects.JSONRawMessage `json:"response_chunks,omitempty"`
		ResponseQualityGuardMatched   bool                     `json:"response_quality_guard_matched,omitempty"`
		ErrorMessage                  string                   `json:"error_message,omitempty"`
		ResponseStatusCode            *int                     `json:"response_status_code,omitempty"`
		Status                        requestexecution.Status  `json:"status,omitempty"`
		Stream                        bool                     `json:"stream,omitempty"`
		MetricsLatencyMs              *int64                   `json:"metrics_latency_ms,omitempty"`
		MetricsFirstTokenLatencyMs    *int64                   `json:"metrics_first_token_latency_ms,omitempty"`
		MetricsReasoningDurationMs    *int64                   `json:"metrics_reasoning_duration_ms,omitempty"`
		ProjectName                   string                   `json:"project_name,omitempty"`
		ChannelName                   string                   `json:"channel_name,omitempty"`
	}

	return json.Marshal(requestExecutionData{
		ID:                            e.ID,
		CreatedAt:                     e.CreatedAt,
		UpdatedAt:                     e.UpdatedAt,
		ProjectID:                     e.ProjectID,
		RequestID:                     e.RequestID,
		ChannelID:                     e.ChannelID,
		CredentialID:                  e.CredentialID,
		DataStorageID:                 e.DataStorageID,
		ExternalID:                    e.ExternalID,
		ModelID:                       e.ModelID,
		CredentialFingerprint:         e.CredentialFingerprint,
		SecretFingerprint:             e.SecretFingerprint,
		ResourceScopeKey:              e.ResourceScopeKey,
		QuotaScopeID:                  e.QuotaScopeID,
		QuotaScopeNameSnapshot:        e.QuotaScopeNameSnapshot,
		QuotaScopeStatusSnapshot:      e.QuotaScopeStatusSnapshot,
		CredentialNameSnapshot:        e.CredentialNameSnapshot,
		CredentialKeyHint:             e.CredentialKeyHint,
		CredentialSource:              e.CredentialSource,
		CredentialQuotaStatusSnapshot: e.CredentialQuotaStatusSnapshot,
		Format:                        e.Format,
		RequestURL:                    e.RequestURL,
		PassThroughApplied:            e.PassThroughApplied,
		RequestHeaders:                e.RequestHeaders,
		RequestBody:                   e.RequestBody,
		ResponseBody:                  e.ResponseBody,
		ResponseChunks:                e.ResponseChunks,
		ResponseQualityGuardMatched:   e.ResponseQualityGuardMatched,
		ErrorMessage:                  e.ErrorMessage,
		ResponseStatusCode:            e.ResponseStatusCode,
		Status:                        e.Status,
		Stream:                        e.Stream,
		MetricsLatencyMs:              e.MetricsLatencyMs,
		MetricsFirstTokenLatencyMs:    e.MetricsFirstTokenLatencyMs,
		MetricsReasoningDurationMs:    e.MetricsReasoningDurationMs,
		ProjectName:                   e.ProjectName,
		ChannelName:                   e.ChannelName,
	})
}

type BackupUsageLog struct {
	ent.UsageLog

	ProjectName string `json:"project_name,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`
	APIKeyKey   string `json:"api_key_key,omitempty"`
}

func (l BackupUsageLog) MarshalJSON() ([]byte, error) {
	type usageLogData struct {
		ID                                 int                `json:"id,omitempty"`
		CreatedAt                          time.Time          `json:"created_at,omitzero"`
		UpdatedAt                          time.Time          `json:"updated_at,omitzero"`
		RequestID                          int                `json:"request_id,omitempty"`
		ProjectID                          int                `json:"project_id,omitempty"`
		ChannelID                          int                `json:"channel_id,omitempty"`
		CredentialID                       int                `json:"credential_id,omitempty"`
		CredentialFingerprint              string             `json:"credential_fingerprint,omitempty"`
		SecretFingerprint                  string             `json:"secret_fingerprint,omitempty"`
		ResourceScopeKey                   string             `json:"resource_scope_key,omitempty"`
		QuotaScopeID                       int                `json:"quota_scope_id,omitempty"`
		QuotaScopeNameSnapshot             string             `json:"quota_scope_name_snapshot,omitempty"`
		QuotaScopeStatusSnapshot           string             `json:"quota_scope_status_snapshot,omitempty"`
		CredentialNameSnapshot             string             `json:"credential_name_snapshot,omitempty"`
		CredentialKeyHint                  string             `json:"credential_key_hint,omitempty"`
		CredentialSource                   string             `json:"credential_source,omitempty"`
		CredentialQuotaStatusSnapshot      string             `json:"credential_quota_status_snapshot,omitempty"`
		ModelID                            string             `json:"model_id,omitempty"`
		PromptTokens                       int64              `json:"prompt_tokens,omitempty"`
		CompletionTokens                   int64              `json:"completion_tokens,omitempty"`
		TotalTokens                        int64              `json:"total_tokens,omitempty"`
		PromptAudioTokens                  int64              `json:"prompt_audio_tokens,omitempty"`
		PromptCachedTokens                 int64              `json:"prompt_cached_tokens,omitempty"`
		PromptWriteCachedTokens            int64              `json:"prompt_write_cached_tokens,omitempty"`
		PromptWriteCachedTokens5m          int64              `json:"prompt_write_cached_tokens_5m,omitempty"`
		PromptWriteCachedTokens1h          int64              `json:"prompt_write_cached_tokens_1h,omitempty"`
		CompletionAudioTokens              int64              `json:"completion_audio_tokens,omitempty"`
		CompletionReasoningTokens          int64              `json:"completion_reasoning_tokens,omitempty"`
		CompletionAcceptedPredictionTokens int64              `json:"completion_accepted_prediction_tokens,omitempty"`
		CompletionRejectedPredictionTokens int64              `json:"completion_rejected_prediction_tokens,omitempty"`
		Source                             usagelog.Source    `json:"source,omitempty"`
		Format                             string             `json:"format,omitempty"`
		TotalCost                          *float64           `json:"total_cost,omitempty"`
		CostItems                          []objects.CostItem `json:"cost_items,omitempty"`
		CostPriceReferenceID               string             `json:"cost_price_reference_id,omitempty"`
		ProjectName                        string             `json:"project_name,omitempty"`
		ChannelName                        string             `json:"channel_name,omitempty"`
		APIKeyKey                          string             `json:"api_key_key,omitempty"`
	}

	return json.Marshal(usageLogData{
		ID:                                 l.ID,
		CreatedAt:                          l.CreatedAt,
		UpdatedAt:                          l.UpdatedAt,
		RequestID:                          l.RequestID,
		ProjectID:                          l.ProjectID,
		ChannelID:                          l.ChannelID,
		CredentialID:                       l.CredentialID,
		CredentialFingerprint:              l.CredentialFingerprint,
		SecretFingerprint:                  l.SecretFingerprint,
		ResourceScopeKey:                   l.ResourceScopeKey,
		QuotaScopeID:                       l.QuotaScopeID,
		QuotaScopeNameSnapshot:             l.QuotaScopeNameSnapshot,
		QuotaScopeStatusSnapshot:           l.QuotaScopeStatusSnapshot,
		CredentialNameSnapshot:             l.CredentialNameSnapshot,
		CredentialKeyHint:                  l.CredentialKeyHint,
		CredentialSource:                   l.CredentialSource,
		CredentialQuotaStatusSnapshot:      l.CredentialQuotaStatusSnapshot,
		ModelID:                            l.ModelID,
		PromptTokens:                       l.PromptTokens,
		CompletionTokens:                   l.CompletionTokens,
		TotalTokens:                        l.TotalTokens,
		PromptAudioTokens:                  l.PromptAudioTokens,
		PromptCachedTokens:                 l.PromptCachedTokens,
		PromptWriteCachedTokens:            l.PromptWriteCachedTokens,
		PromptWriteCachedTokens5m:          l.PromptWriteCachedTokens5m,
		PromptWriteCachedTokens1h:          l.PromptWriteCachedTokens1h,
		CompletionAudioTokens:              l.CompletionAudioTokens,
		CompletionReasoningTokens:          l.CompletionReasoningTokens,
		CompletionAcceptedPredictionTokens: l.CompletionAcceptedPredictionTokens,
		CompletionRejectedPredictionTokens: l.CompletionRejectedPredictionTokens,
		Source:                             l.Source,
		Format:                             l.Format,
		TotalCost:                          l.TotalCost,
		CostItems:                          l.CostItems,
		CostPriceReferenceID:               l.CostPriceReferenceID,
		ProjectName:                        l.ProjectName,
		ChannelName:                        l.ChannelName,
		APIKeyKey:                          l.APIKeyKey,
	})
}

const (
	BackupVersion   = "1.3"
	BackupVersionV1 = "1.0"
	BackupVersionV2 = "1.1"
	BackupVersionV3 = "1.2"
)

type BackupOptions struct {
	IncludeProjects    bool
	IncludeChannels    bool
	IncludeModels      bool
	IncludeAPIKeys     bool
	IncludeModelPrices bool
	IncludeUsageStats  bool
	IncludeRequestLogs bool
}

type ConflictStrategy string

const (
	ConflictStrategySkip      ConflictStrategy = "skip"
	ConflictStrategyOverwrite ConflictStrategy = "overwrite"
	ConflictStrategyError     ConflictStrategy = "error"
)

type RestoreOptions struct {
	IncludeProjects            bool
	IncludeChannels            bool
	IncludeModels              bool
	IncludeAPIKeys             bool
	IncludeModelPrices         bool
	IncludeUsageStats          bool
	IncludeRequestLogs         bool
	ProjectConflictStrategy    ConflictStrategy
	ChannelConflictStrategy    ConflictStrategy
	ModelConflictStrategy      ConflictStrategy
	ModelPriceConflictStrategy ConflictStrategy
	APIKeyConflictStrategy     ConflictStrategy
}
