package orchestrator

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

func TestRouteProfileCandidates_OrdersByRouteTiersAndPreferredChannel(t *testing.T) {
	state := &PersistenceState{
		APIKey: &ent.APIKey{
			ID:        10,
			ProjectID: 20,
			Profiles: &objects.APIKeyProfiles{
				ActiveProfile: "human",
				Profiles: []objects.APIKeyProfile{{
					Name: "human",
					RouteTiers: []objects.APIKeyRouteTier{
						{Name: "primary", ChannelIDs: []int{2, 1}},
						{Name: "fallback", ChannelIDs: []int{3}},
					},
					PreferredChannelID: lo.ToPtr(1),
				}},
			},
		},
	}
	candidates := []*ChannelModelsCandidate{
		stickyTestCandidate(3, 99, 1000),
		stickyTestCandidate(2, 99, 1),
		stickyTestCandidate(1, 99, 1),
	}

	ordered, err := routeProfileCandidates(state, candidates, &llm.Request{
		Model:     "gpt-4",
		APIFormat: llm.APIFormatOpenAIChatCompletion,
	})

	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3}, stickyCandidateIDs(ordered))
	require.Equal(t, []int{0, 0, 1}, routeCandidateTiers(ordered))
}

func TestRouteProfileCandidates_RequiresExplicitRouteTiers(t *testing.T) {
	state := &PersistenceState{
		APIKey: &ent.APIKey{
			ID:        10,
			ProjectID: 20,
			Profiles: &objects.APIKeyProfiles{
				ActiveProfile: "legacy",
				Profiles: []objects.APIKeyProfile{{
					Name:       "legacy",
					ChannelIDs: []int{1, 2},
				}},
			},
		},
	}

	ordered, err := routeProfileCandidates(state, []*ChannelModelsCandidate{
		stickyTestCandidate(1, 0, 100),
		stickyTestCandidate(2, 0, 50),
	}, &llm.Request{Model: "gpt-4"})

	require.Nil(t, ordered)
	require.Error(t, err)
	require.Contains(t, err.Error(), "has no route tiers")
}

func TestRouteProfileCandidates_RequiresAPIKeyAndActiveProfile(t *testing.T) {
	candidates := []*ChannelModelsCandidate{stickyTestCandidate(1, 0, 100)}

	ordered, err := routeProfileCandidates(nil, candidates, &llm.Request{Model: "gpt-4"})
	require.Nil(t, ordered)
	require.Error(t, err)
	require.Contains(t, err.Error(), "api key route profile is required")

	ordered, err = routeProfileCandidates(&PersistenceState{APIKey: &ent.APIKey{}}, candidates, &llm.Request{Model: "gpt-4"})
	require.Nil(t, ordered)
	require.Error(t, err)
	require.Contains(t, err.Error(), "active api key route profile is required")
}

func TestRouteProfileCandidates_UsesStableAffinityWithoutOrderingWeight(t *testing.T) {
	state := &PersistenceState{
		APIKey: &ent.APIKey{
			ID:        10,
			ProjectID: 20,
			Profiles: &objects.APIKeyProfiles{
				ActiveProfile: "human",
				Profiles: []objects.APIKeyProfile{{
					Name: "human",
					RouteTiers: []objects.APIKeyRouteTier{{
						Name:       "primary",
						ChannelIDs: []int{1, 2, 3},
					}},
				}},
			},
		},
	}
	candidates := []*ChannelModelsCandidate{
		stickyTestCandidate(1, 99, 1),
		stickyTestCandidate(2, 99, 1000),
		stickyTestCandidate(3, 99, 500),
	}
	req := &llm.Request{Model: "gpt-4", APIFormat: llm.APIFormatOpenAIChatCompletion}

	first, err := routeProfileCandidates(state, candidates, req)
	require.NoError(t, err)
	second, err := routeProfileCandidates(state, candidates, req)
	require.NoError(t, err)

	require.Equal(t, stickyCandidateIDs(first), stickyCandidateIDs(second))
	require.ElementsMatch(t, []int{1, 2, 3}, stickyCandidateIDs(first))
	require.Equal(t, []int{0, 0, 0}, routeCandidateTiers(first))
}

func TestRouteProfileCandidates_SkipsInfeasibleTierAndKeepsTierPriority(t *testing.T) {
	state := &PersistenceState{
		APIKey: &ent.APIKey{
			ID:        10,
			ProjectID: 20,
			Profiles: &objects.APIKeyProfiles{
				ActiveProfile: "human",
				Profiles: []objects.APIKeyProfile{{
					Name: "human",
					RouteTiers: []objects.APIKeyRouteTier{
						{Name: "unavailable", ChannelIDs: []int{99}},
						{Name: "fallback", ChannelIDs: []int{2, 1}},
					},
				}},
			},
		},
	}
	candidates := []*ChannelModelsCandidate{
		stickyTestCandidate(1, 99, 1000),
		stickyTestCandidate(2, 99, 1),
	}

	ordered, err := routeProfileCandidates(state, candidates, &llm.Request{
		Model:     "gpt-4",
		APIFormat: llm.APIFormatOpenAIChatCompletion,
	})

	require.NoError(t, err)
	require.ElementsMatch(t, []int{1, 2}, stickyCandidateIDs(ordered))
	require.Equal(t, []int{1, 1}, routeCandidateTiers(ordered))
}

func TestRouteProfileCandidates_PreservesSameChannelModelFallbackCandidates(t *testing.T) {
	state := &PersistenceState{
		APIKey: &ent.APIKey{
			ID:        10,
			ProjectID: 20,
			Profiles: &objects.APIKeyProfiles{
				ActiveProfile: "human",
				Profiles: []objects.APIKeyProfile{{
					Name: "human",
					RouteTiers: []objects.APIKeyRouteTier{{
						Name:       "primary",
						ChannelIDs: []int{1},
					}},
				}},
			},
		},
	}
	candidates := []*ChannelModelsCandidate{
		stickyTestCandidate(1, 0, 100),
		stickyTestCandidate(1, 0, 100),
	}
	candidates[0].Models = []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}}
	candidates[1].Models = []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-3.5-turbo"}}

	ordered, err := routeProfileCandidates(state, candidates, &llm.Request{
		Model:     "gpt-4",
		APIFormat: llm.APIFormatOpenAIChatCompletion,
	})

	require.NoError(t, err)
	require.Len(t, ordered, 2)
	require.Equal(t, "gpt-4", ordered[0].Models[0].ActualModel)
	require.Equal(t, "gpt-3.5-turbo", ordered[1].Models[0].ActualModel)
	require.Equal(t, []int{0, 0}, routeCandidateTiers(ordered))
}

func routeCandidateTiers(candidates []*ChannelModelsCandidate) []int {
	tiers := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		tiers = append(tiers, candidate.Priority)
	}
	return tiers
}
