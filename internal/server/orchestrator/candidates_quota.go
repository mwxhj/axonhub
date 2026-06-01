package orchestrator

import (
	"context"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/llm"
)

type ProviderQuotaSelector struct {
	wrapped       CandidateSelector
	provider      ProviderQuotaStatusProvider
	systemService QuotaEnforcementSettingsProvider
	// FilteredCount holds the number of candidates removed by the last Select() call.
	// Read after Select() to distinguish "no candidates due to quota exhaustion"
	// from "no candidates at all".
	FilteredCount int
}

func WithProviderQuotaSelector(wrapped CandidateSelector, provider ProviderQuotaStatusProvider, systemService QuotaEnforcementSettingsProvider) *ProviderQuotaSelector {
	return &ProviderQuotaSelector{
		wrapped:       wrapped,
		provider:      provider,
		systemService: systemService,
	}
}

func (s *ProviderQuotaSelector) Select(ctx context.Context, req *llm.Request) ([]*ChannelModelsCandidate, error) {
	candidates, err := s.wrapped.Select(ctx, req)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return candidates, nil
	}

	var localFilteredCount int
	candidates, localFilteredCount = filterCandidatesWithExecutableCredentialViews(candidates)
	s.FilteredCount = localFilteredCount
	if len(candidates) == 0 {
		return candidates, nil
	}

	if s.provider == nil {
		return candidates, nil
	}

	settings := s.systemService.QuotaEnforcementSettingsOrDefault(ctx)

	if !settings.Enabled {
		return candidates, nil
	}

	limitType := provider_quota.RequestModality(req.Image != nil)
	hardFilter := settings.Mode != biz.QuotaEnforcementModeDePrioritize

	filtered := make([]*ChannelModelsCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		narrowed, ok := s.selectableCandidateByQuota(candidate, limitType, hardFilter)
		if !ok {
			continue
		}
		filtered = append(filtered, narrowed)
	}

	s.FilteredCount = localFilteredCount + len(candidates) - len(filtered)

	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "ProviderQuotaSelector: filtered candidates",
			log.String("model", req.Model),
			log.String("mode", string(settings.Mode)),
			log.String("limit_type", string(limitType)),
			log.Int("before", len(candidates)),
			log.Int("after", len(filtered)),
		)
	}

	return filtered, nil
}

func filterCandidatesWithExecutableCredentialViews(candidates []*ChannelModelsCandidate) ([]*ChannelModelsCandidate, int) {
	filtered := make([]*ChannelModelsCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.Channel == nil {
			filtered = append(filtered, candidate)
			continue
		}

		views := candidate.Channel.CredentialViews()
		if len(views) == 0 {
			filtered = append(filtered, candidate)
			continue
		}

		for _, view := range views {
			if view.Enabled {
				filtered = append(filtered, candidate)
				break
			}
		}
	}

	return filtered, len(candidates) - len(filtered)
}

func (s *ProviderQuotaSelector) selectableCandidateByQuota(
	candidate *ChannelModelsCandidate,
	limitType provider_quota.QuotaLimitType,
	hardFilter bool,
) (*ChannelModelsCandidate, bool) {
	if candidate == nil || candidate.Channel == nil {
		return candidate, true
	}

	views := candidate.Channel.CredentialViews()
	if len(views) == 0 {
		if !hardFilter {
			return candidate, true
		}
		return candidate, quotaStatusSelectable(s.provider.GetQuotaStatus(candidate.Channel.ID), limitType)
	}

	selectableViews := make([]biz.ChannelCredentialView, 0, len(views))
	statusSeen := false
	for _, view := range views {
		if !view.Enabled {
			continue
		}
		status := quotaStatusForCredentialView(s.provider, view)
		if status != nil {
			statusSeen = true
		}
		if quotaStatusSelectable(status, limitType) {
			selectableViews = append(selectableViews, view)
		}
	}

	if !statusSeen {
		if !hardFilter {
			return candidate, true
		}
		return candidate, quotaStatusSelectable(s.provider.GetQuotaStatus(candidate.Channel.ID), limitType)
	}
	if len(selectableViews) == 0 {
		return candidate, !hardFilter
	}
	if len(selectableViews) == len(views) {
		return candidate, true
	}

	narrowed := *candidate
	narrowed.Channel = candidate.Channel.WithCredentialViewsForSelection(selectableViews)

	return &narrowed, true
}
