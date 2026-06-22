package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/pipeline"
)

// selectCandidates creates a middleware that selects available channel model candidates for the model.
// This is the second step in the inbound pipeline, moved from outbound transformer.
// If no valid candidates are found, it returns ErrInvalidModel to fail fast.
func selectCandidates(inbound *PersistentInboundTransformer) pipeline.Middleware {
	return pipeline.OnLlmRequest("select-candidates", func(ctx context.Context, llmRequest *llm.Request) (*llm.Request, error) {
		// Only select candidates once
		if len(inbound.state.ChannelModelsCandidates) > 0 {
			return llmRequest, nil
		}

		selector := inbound.state.CandidateSelector

		// Project-level profile filtering (upper boundary)
		if inbound.state.APIKey != nil {
			if project := inbound.state.APIKey.Edges.Project; project != nil {
				if projectProfile := project.GetActiveProfile(); projectProfile != nil {
					if len(projectProfile.ChannelIDs) > 0 {
						selector = WithSelectedChannelsSelector(selector, projectProfile.ChannelIDs)
					}

					if len(projectProfile.ChannelTags) > 0 {
						selector = WithChannelTagsFilterSelector(selector, projectProfile.ChannelTags, projectProfile.ChannelTagsMatchMode)
					}
				}
			}
		}

		// Apply Google native tools filter (only for Gemini native API format)
		if llmRequest.APIFormat == llm.APIFormatGeminiContents {
			selector = WithGoogleNativeToolsSelector(selector)
		}

		// Apply Anthropic native tools filter (only for Anthropic message API format)
		if llmRequest.APIFormat == llm.APIFormatAnthropicMessage {
			selector = WithAnthropicNativeToolsSelector(selector)
		}

		selector = WithStreamPolicySelector(selector)

		candidates, err := selector.Select(ctx, llmRequest)
		if err != nil {
			return nil, err
		}
		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "selected candidates",
				log.Int("candidate_count", len(candidates)),
				log.String("model", llmRequest.Model),
				log.Any("candidates", lo.Map(candidates, func(candidate *ChannelModelsCandidate, _ int) map[string]any {
					return map[string]any{
						"channel_name": candidate.Channel.Name,
						"channel_id":   candidate.Channel.ID,
						"priority":     candidate.Priority,
						"models": lo.Map(candidate.Models, func(entry biz.ChannelModelEntry, _ int) map[string]any {
							return map[string]any{
								"request_model": entry.RequestModel,
								"actual_model":  entry.ActualModel,
								"source":        entry.Source,
							}
						}),
					}
				})),
			)
		}

		if len(candidates) == 0 {
			return nil, fmt.Errorf("%w: %s", biz.ErrInvalidModel, llmRequest.Model)
		}

		ordered, err := routeProfileCandidates(inbound.state, candidates, llmRequest)
		if err != nil {
			return nil, err
		}
		if len(ordered) == 0 {
			return nil, fmt.Errorf("%w: no route-tier candidates for model %s", biz.ErrInvalidModel, llmRequest.Model)
		}

		inbound.state.ChannelModelsCandidates = ordered

		return llmRequest, nil
	})
}

func orderCandidates(
	inbound *PersistentInboundTransformer,
	stickyEnabled bool,
	stickyRouter *StickySessionRouter,
) pipeline.Middleware {
	return pipeline.OnLlmRequest("order-candidates", func(ctx context.Context, llmRequest *llm.Request) (*llm.Request, error) {
		candidates := inbound.state.ChannelModelsCandidates
		if len(candidates) == 0 {
			return llmRequest, nil
		}

		if stickyEnabled && stickyRouter != nil {
			ordered := stickyRouter.Order(ctx, StickySessionOrderRequest{
				Request:    llmRequest,
				State:      inbound.state,
				Candidates: candidates,
			})
			if len(ordered) > 0 {
				inbound.state.ChannelModelsCandidates = ordered
			}
		}

		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "ordered candidates",
				log.Int("candidate_count", len(inbound.state.ChannelModelsCandidates)),
				log.String("model", llmRequest.Model),
				log.String("route_strategy", "api-key-route-tiers"),
				log.Bool("sticky_session_enabled", stickyEnabled),
				log.Any("candidates", lo.Map(inbound.state.ChannelModelsCandidates, func(candidate *ChannelModelsCandidate, _ int) map[string]any {
					return map[string]any{
						"channel_name": candidate.Channel.Name,
						"channel_id":   candidate.Channel.ID,
						"route_tier":   candidate.Priority,
					}
				})))
		}

		return llmRequest, nil
	})
}

func routeProfileCandidates(
	state *PersistenceState,
	candidates []*ChannelModelsCandidate,
	req *llm.Request,
) ([]*ChannelModelsCandidate, error) {
	if state == nil || state.APIKey == nil {
		return nil, fmt.Errorf("%w: api key route profile is required", biz.ErrInvalidAPIKey)
	}

	profile := state.APIKey.GetActiveProfile()
	if profile == nil {
		return nil, fmt.Errorf("%w: active api key route profile is required", biz.ErrInvalidAPIKey)
	}
	if len(profile.RouteTiers) == 0 {
		return nil, fmt.Errorf("%w: api key route profile '%s' has no route tiers", biz.ErrInvalidModel, profile.Name)
	}

	candidatesByChannelID := make(map[int][]*ChannelModelsCandidate, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.Channel == nil {
			continue
		}
		candidatesByChannelID[candidate.Channel.ID] = append(candidatesByChannelID[candidate.Channel.ID], candidate)
	}

	ordered := make([]*ChannelModelsCandidate, 0, len(candidates))
	for tierIndex, tier := range profile.RouteTiers {
		tierCandidates := routeTierCandidates(candidatesByChannelID, tier.ChannelIDs, tierIndex)
		if len(tierCandidates) == 0 {
			continue
		}

		tierCandidates = orderRouteTierCandidates(state, profile, req, tierCandidates)
		ordered = append(ordered, tierCandidates...)
	}
	if len(ordered) == 0 {
		return nil, fmt.Errorf("%w: api key route profile '%s' has no feasible route for model %s", biz.ErrInvalidModel, profile.Name, req.Model)
	}

	return ordered, nil
}

func routeTierCandidates(
	candidatesByChannelID map[int][]*ChannelModelsCandidate,
	channelIDs []int,
	tierIndex int,
) []*ChannelModelsCandidate {
	result := make([]*ChannelModelsCandidate, 0, len(channelIDs))
	seen := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		if _, ok := seen[channelID]; ok {
			continue
		}
		seen[channelID] = struct{}{}

		channelCandidates := candidatesByChannelID[channelID]
		if len(channelCandidates) == 0 {
			continue
		}
		for _, candidate := range channelCandidates {
			if candidate == nil {
				continue
			}
			cloned := *candidate
			// From here on Priority means route tier index. Model association priority
			// has already done feasibility resolution and is not a runtime ordering source.
			cloned.Priority = tierIndex
			result = append(result, &cloned)
		}
	}

	return result
}

func orderRouteTierCandidates(
	state *PersistenceState,
	profile *objects.APIKeyProfile,
	req *llm.Request,
	candidates []*ChannelModelsCandidate,
) []*ChannelModelsCandidate {
	if len(candidates) <= 1 {
		return candidates
	}

	primaryChannelID := routePreferredChannelID(profile, candidates)
	if primaryChannelID <= 0 {
		primaryChannelID = routeAffinityChannelID(state, profile, req, candidates)
	}
	if primaryChannelID <= 0 || firstCandidateChannelID(candidates) == primaryChannelID {
		return candidates
	}

	ordered := make([]*ChannelModelsCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidateChannelID(candidate) == primaryChannelID {
			ordered = append(ordered, candidate)
		}
	}
	for _, candidate := range candidates {
		if candidateChannelID(candidate) == primaryChannelID {
			continue
		}
		ordered = append(ordered, candidate)
	}

	return ordered
}

func routePreferredChannelID(profile *objects.APIKeyProfile, candidates []*ChannelModelsCandidate) int {
	if profile == nil || profile.PreferredChannelID == nil {
		return 0
	}

	for _, candidate := range candidates {
		if candidateChannelID(candidate) == *profile.PreferredChannelID {
			return *profile.PreferredChannelID
		}
	}

	return 0
}

func routeAffinityChannelID(
	state *PersistenceState,
	profile *objects.APIKeyProfile,
	req *llm.Request,
	candidates []*ChannelModelsCandidate,
) int {
	channelIDs := uniqueCandidateChannelIDs(candidates)
	if len(channelIDs) == 0 {
		return 0
	}
	if len(channelIDs) == 1 {
		return channelIDs[0]
	}

	seed := routeAffinitySeed(state, profile, req)
	hash := sha256.Sum256([]byte(seed))
	index := int(binary.BigEndian.Uint64(hash[:8]) % uint64(len(channelIDs)))
	if index < 0 || index >= len(channelIDs) {
		return 0
	}

	return channelIDs[index]
}

func uniqueCandidateChannelIDs(candidates []*ChannelModelsCandidate) []int {
	seen := make(map[int]struct{}, len(candidates))
	result := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		channelID := candidateChannelID(candidate)
		if channelID <= 0 {
			continue
		}
		if _, ok := seen[channelID]; ok {
			continue
		}
		seen[channelID] = struct{}{}
		result = append(result, channelID)
	}

	return result
}

func firstCandidateChannelID(candidates []*ChannelModelsCandidate) int {
	for _, candidate := range candidates {
		if channelID := candidateChannelID(candidate); channelID > 0 {
			return channelID
		}
	}

	return 0
}

func candidateChannelID(candidate *ChannelModelsCandidate) int {
	if candidate == nil || candidate.Channel == nil {
		return 0
	}

	return candidate.Channel.ID
}

func routeAffinitySeed(state *PersistenceState, profile *objects.APIKeyProfile, req *llm.Request) string {
	model := ""
	apiFormat := ""
	if req != nil {
		model = req.Model
		apiFormat = string(req.APIFormat)
	}

	apiKeyID := 0
	projectID := 0
	if state != nil && state.APIKey != nil {
		apiKeyID = state.APIKey.ID
		projectID = state.APIKey.ProjectID
	}

	profileName := ""
	if profile != nil {
		profileName = profile.Name
	}

	return fmt.Sprintf("api-key:%d:project:%d:profile:%s:model:%s:format:%s", apiKeyID, projectID, profileName, model, apiFormat)
}
