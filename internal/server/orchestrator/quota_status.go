package orchestrator

import (
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
)

func quotaStatusForChannel(provider ProviderQuotaStatusProvider, channel *biz.Channel, limitType provider_quota.QuotaLimitType) *biz.QuotaChannelStatus {
	if provider == nil || channel == nil {
		return nil
	}

	views := channel.CredentialViews()
	if len(views) == 0 {
		return provider.GetQuotaStatus(channel.ID)
	}

	credentialStatuses := make([]*biz.QuotaChannelStatus, 0, len(views))
	seenResourceScopes := map[string]struct{}{}
	seenIDs := map[int]struct{}{}
	seenFingerprints := map[string]struct{}{}
	noDataViews := 0

	for _, view := range views {
		if !view.Enabled {
			continue
		}

		var status *biz.QuotaChannelStatus
		if view.ResourceScopeKey != "" {
			if _, ok := seenResourceScopes[view.ResourceScopeKey]; !ok {
				seenResourceScopes[view.ResourceScopeKey] = struct{}{}
				status = provider.GetResourceScopeQuotaStatus(view.ResourceScopeKey)
			}
		}
		if status == nil && view.CredentialID > 0 {
			if _, ok := seenIDs[view.CredentialID]; ok {
				continue
			}
			seenIDs[view.CredentialID] = struct{}{}
			status = provider.GetCredentialQuotaStatusByID(view.CredentialID)
		}
		if status == nil && view.Fingerprint != "" {
			if _, ok := seenFingerprints[view.Fingerprint]; ok {
				continue
			}
			seenFingerprints[view.Fingerprint] = struct{}{}
			status = provider.GetCredentialQuotaStatus(view.Fingerprint)
		}
		if status != nil {
			credentialStatuses = append(credentialStatuses, status)
			continue
		}
		noDataViews++
	}
	if len(credentialStatuses) == 0 {
		return provider.GetQuotaStatus(channel.ID)
	}
	for range noDataViews {
		credentialStatuses = append(credentialStatuses, &biz.QuotaChannelStatus{
			Status: providerquotastatus.StatusUnknown,
			Ready:  true,
		})
	}

	return aggregateCredentialStatuses(credentialStatuses, limitType)
}

func quotaStatusForCredentialView(provider ProviderQuotaStatusProvider, view biz.ChannelCredentialView) *biz.QuotaChannelStatus {
	if provider == nil {
		return nil
	}

	if view.ResourceScopeKey != "" {
		if status := provider.GetResourceScopeQuotaStatus(view.ResourceScopeKey); status != nil {
			return status
		}
	}
	if view.CredentialID > 0 {
		if status := provider.GetCredentialQuotaStatusByID(view.CredentialID); status != nil {
			return status
		}
	}
	if view.Fingerprint != "" {
		if status := provider.GetCredentialQuotaStatus(view.Fingerprint); status != nil {
			return status
		}
	}

	return nil
}

func quotaStatusSelectable(status *biz.QuotaChannelStatus, limitType provider_quota.QuotaLimitType) bool {
	if status == nil {
		return true
	}

	effectiveStatus, _ := status.EffectiveStatus(limitType)
	switch effectiveStatus {
	case providerquotastatus.StatusAvailable,
		providerquotastatus.StatusWarning,
		providerquotastatus.StatusUnknown:
		return true
	case providerquotastatus.StatusExhausted:
		return false
	default:
		return true
	}
}

func aggregateCredentialStatuses(statuses []*biz.QuotaChannelStatus, limitType provider_quota.QuotaLimitType) *biz.QuotaChannelStatus {
	hasAvailable := false
	hasWarning := false
	hasUnknown := false
	hasExhausted := false
	limits := make([]provider_quota.QuotaLimitStatus, 0)

	for _, status := range statuses {
		if status == nil {
			continue
		}

		effectiveStatus, _ := status.EffectiveStatus(limitType)
		switch effectiveStatus {
		case providerquotastatus.StatusAvailable:
			hasAvailable = true
		case providerquotastatus.StatusWarning:
			hasWarning = true
		case providerquotastatus.StatusUnknown:
			hasUnknown = true
		case providerquotastatus.StatusExhausted:
			hasExhausted = true
		}

		limits = append(limits, status.Limits...)
	}

	status := providerquotastatus.StatusUnknown
	switch {
	case hasWarning:
		status = providerquotastatus.StatusWarning
	case hasAvailable:
		status = providerquotastatus.StatusAvailable
	case hasUnknown:
		status = providerquotastatus.StatusUnknown
	case hasExhausted:
		status = providerquotastatus.StatusExhausted
	}

	return &biz.QuotaChannelStatus{
		Status: status,
		Ready:  provider_quota.IsReadyStatus(string(status)),
		Limits: limits,
	}
}
