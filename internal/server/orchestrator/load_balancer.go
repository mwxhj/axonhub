package orchestrator

import (
	"context"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
)

// ChannelMetricsProvider provides channel performance metrics.
type ChannelMetricsProvider interface {
	GetChannelMetrics(ctx context.Context, channelID int) (*biz.AggregatedMetrics, error)
}

// ChannelSelectionTracker tracks channel selections for load balancing.
// This is used to increment request count at selection time rather than completion time,
// ensuring concurrent/burst requests don't all select the same channel.
type ChannelSelectionTracker interface {
	IncrementChannelSelection(channelID int)
}

// LoadBalanceStrategy defines the interface for load balancing strategies.
// Each strategy can score and sort channels based on different criteria.
type LoadBalanceStrategy interface {
	// Score calculates a score for a channel. Higher scores indicate higher priority.
	// Returns a score between 0 and 1000.
	// This is the production path with minimal overhead.
	Score(ctx context.Context, channel *biz.Channel) float64

	// ScoreWithDebug calculates a score with detailed debug information.
	// Returns the score and a StrategyScore with debug details.
	// This should have identical logic to Score() except for debug logging.
	ScoreWithDebug(ctx context.Context, channel *biz.Channel) (float64, StrategyScore)

	// Name returns the strategy name for debugging and logging.
	Name() string
}

// StrategyScore holds the detailed scoring information from a single strategy.
type StrategyScore struct {
	// StrategyName is the name of the strategy
	StrategyName string
	// Score is the score calculated by this strategy
	Score float64
	// Details contains strategy-specific information
	Details map[string]any
	// Duration is the time spent on scoring
	Duration time.Duration
}

// ChannelDecision holds detailed scoring information for a single channel.
type ChannelDecision struct {
	// Candidate is the execution candidate this decision ranks.
	Candidate *ChannelModelsCandidate
	// Channel is the channel object
	Channel *biz.Channel
	// TotalScore is the sum of all strategy scores
	TotalScore float64
	// StrategyScores contains scores from each strategy
	StrategyScores []StrategyScore
	// FinalRank is the final ranking (1 = highest priority)
	FinalRank int
}

// DecisionLog represents a complete load balancing decision.
type DecisionLog struct {
	// Timestamp when the decision was made
	Timestamp time.Time
	// ChannelCount is the number of channels considered
	ChannelCount int
	// TotalDuration is the time spent on load balancing
	TotalDuration time.Duration
	// Channels contains detailed information for each channel
	Channels []ChannelDecision
}

// RetryPolicyProvider interface defines the methods needed from RetryPolicyProvider.
type RetryPolicyProvider interface {
	RetryPolicyOrDefault(ctx context.Context) *biz.RetryPolicy
}

// Save the requested model ID in the context, to let model aware strategy use it, e.g. circuit breaker.
type modelContextKey struct{}
type streamContextKey struct{}

// contextWithRequestedModel adds the requested model ID to the context.
func contextWithRequestedModel(ctx context.Context, modelID string) context.Context {
	return context.WithValue(ctx, modelContextKey{}, modelID)
}

// requestedModelFromContext extracts the requested model ID from the context.
func requestedModelFromContext(ctx context.Context) string {
	if model, ok := ctx.Value(modelContextKey{}).(string); ok {
		return model
	}

	return ""
}

func contextWithRequestStream(ctx context.Context, stream bool) context.Context {
	return context.WithValue(ctx, streamContextKey{}, stream)
}

func requestStreamFromContext(ctx context.Context) bool {
	stream, _ := ctx.Value(streamContextKey{}).(bool)

	return stream
}

// LoadBalancer applies multiple strategies to sort channels by priority.
type LoadBalancer struct {
	strategies       []LoadBalanceStrategy
	systemService    RetryPolicyProvider
	selectionTracker ChannelSelectionTracker
	debug            bool
}

// NewLoadBalancer creates a new load balancer with the given strategies.
// All strategy scores are summed with equal weight; registration order does not affect scoring.
func NewLoadBalancer(systemService RetryPolicyProvider, selectionTracker ChannelSelectionTracker, strategies ...LoadBalanceStrategy) *LoadBalancer {
	debug := strings.EqualFold(os.Getenv("AXONHUB_DEBUG_LOAD_BALANCER_ENABLED"), "true")

	return &LoadBalancer{
		strategies:       strategies,
		systemService:    systemService,
		selectionTracker: selectionTracker,
		debug:            debug,
	}
}

// candidateScore holds a candidate and its calculated score.
type candidateScore struct {
	candidate *ChannelModelsCandidate
	score     float64
}

// Sort sorts all candidates according to the configured strategies.
// Selection must keep the full feasible candidate set; retry/fallback policy
// decides after an attempt fails, not during pre-attempt ordering.
func (lb *LoadBalancer) Sort(ctx context.Context, candidates []*ChannelModelsCandidate, model string, stream bool) []*ChannelModelsCandidate {
	return lb.sort(ctx, candidates, model, stream, true)
}

func (lb *LoadBalancer) SortWithoutTracking(ctx context.Context, candidates []*ChannelModelsCandidate, model string, stream bool) []*ChannelModelsCandidate {
	return lb.sort(ctx, candidates, model, stream, false)
}

func (lb *LoadBalancer) sort(ctx context.Context, candidates []*ChannelModelsCandidate, model string, stream bool, trackSelection bool) []*ChannelModelsCandidate {
	if len(candidates) <= 1 {
		return candidates
	}

	// Add model information to context for circuit-breaker strategy
	ctx = contextWithRequestedModel(ctx, model)
	ctx = contextWithRequestStream(ctx, stream)

	// Use debug path if debug mode is enabled
	debugEnabled := IsDebugEnabled(ctx)
	if lb.debug || debugEnabled {
		return lb.sortWithDebug(ctx, candidates, model, trackSelection)
	}

	// Production path - minimal overhead
	return lb.sortProduction(ctx, candidates, trackSelection)
}

func (lb *LoadBalancer) RequiredCandidateCount(ctx context.Context, candidates []*ChannelModelsCandidate) int {
	_ = ctx
	if lb == nil {
		return len(candidates)
	}

	return len(candidates)
}

func (lb *LoadBalancer) TrackSelection(candidates []*ChannelModelsCandidate) {
	if lb == nil {
		return
	}

	lb.trackSelection(candidates)
}

func (lb *LoadBalancer) IsStickyPrimaryEligible(ctx context.Context, candidate *ChannelModelsCandidate, model string, stream bool) bool {
	if lb == nil || candidate == nil || candidate.Channel == nil {
		return false
	}

	ctx = contextWithRequestedModel(ctx, model)
	ctx = contextWithRequestStream(ctx, stream)

	for _, strategy := range lb.strategies {
		score := strategy.Score(ctx, candidate.Channel)
		if score <= rateLimitExhaustedScore {
			return false
		}
	}

	return true
}

func (lb *LoadBalancer) trackSelection(candidates []*ChannelModelsCandidate) {
	if len(candidates) > 0 && candidates[0] != nil && candidates[0].Channel != nil && lb.selectionTracker != nil {
		lb.selectionTracker.IncrementChannelSelection(candidates[0].Channel.ID)
	}
}

// sortProduction is the fast path without debug overhead.
func (lb *LoadBalancer) sortProduction(ctx context.Context, candidates []*ChannelModelsCandidate, shouldTrackSelection bool) []*ChannelModelsCandidate {
	scored := make([]candidateScore, len(candidates))
	for i, c := range candidates {
		totalScore := 0.0
		// Apply all strategies
		for _, strategy := range lb.strategies {
			totalScore += strategy.Score(ctx, c.Channel)
		}

		scored[i] = candidateScore{
			candidate: c,
			score:     totalScore,
		}
	}

	// Sort by total score descending (higher score = higher priority)
	// When scores are equal, use OrderingWeight as tie-breaker (higher weight = higher priority)
	// Do NOT use channel ID as tie-breaker to avoid deterministic ordering that causes uneven distribution
	slices.SortStableFunc(scored, func(a, b candidateScore) int {
		if a.score > b.score {
			return -1
		} else if a.score < b.score {
			return 1
		}

		if a.candidate != nil && b.candidate != nil && a.candidate.Channel != nil && b.candidate.Channel != nil {
			if a.candidate.Channel.OrderingWeight > b.candidate.Channel.OrderingWeight {
				return -1
			} else if a.candidate.Channel.OrderingWeight < b.candidate.Channel.OrderingWeight {
				return 1
			}
		}

		// When score and weight are equal, return 0 to preserve original order (non-deterministic)
		return 0
	})

	result := lo.Map(scored, func(ch candidateScore, _ int) *ChannelModelsCandidate { return ch.candidate })

	// Increment selection count for the top candidate to ensure subsequent
	// concurrent requests see the updated count and select different channels
	if shouldTrackSelection {
		lb.trackSelection(result)
	}

	return result
}

// sortWithDebug is the debug path with detailed logging.
func (lb *LoadBalancer) sortWithDebug(ctx context.Context, candidates []*ChannelModelsCandidate, model string, shouldTrackSelection bool) []*ChannelModelsCandidate {
	startTime := time.Now()

	// Calculate detailed scores for each candidate
	decisions := make([]ChannelDecision, len(candidates))
	for i, c := range candidates {
		totalScore := 0.0
		strategyScores := make([]StrategyScore, 0, len(lb.strategies))

		// Apply all strategies and collect detailed scores
		for _, strategy := range lb.strategies {
			scoreStart := time.Now()
			score, strategyScore := strategy.ScoreWithDebug(ctx, c.Channel)
			strategyScore.Duration = time.Since(scoreStart)
			strategyScores = append(strategyScores, strategyScore)
			totalScore += score
		}

		decisions[i] = ChannelDecision{
			Candidate:      c,
			Channel:        c.Channel,
			TotalScore:     totalScore,
			StrategyScores: strategyScores,
			FinalRank:      0, // Will be set after sorting
		}
	}

	// Sort by total score descending (higher score = higher priority)
	// When scores are equal, use OrderingWeight as tie-breaker (higher weight = higher priority)
	// Do NOT use channel ID as tie-breaker to avoid deterministic ordering that causes uneven distribution
	slices.SortStableFunc(decisions, func(a, b ChannelDecision) int {
		if a.TotalScore > b.TotalScore {
			return -1
		} else if a.TotalScore < b.TotalScore {
			return 1
		}

		if a.Channel != nil && b.Channel != nil {
			if a.Channel.OrderingWeight > b.Channel.OrderingWeight {
				return -1
			} else if a.Channel.OrderingWeight < b.Channel.OrderingWeight {
				return 1
			}
		}

		// When score and weight are equal, return 0 to preserve original order (non-deterministic)
		return 0
	})

	for i := range decisions {
		decisions[i].FinalRank = i + 1
	}

	lb.logDecision(ctx, candidates, model, decisions, time.Since(startTime))

	result := lo.Map(decisions, func(decision ChannelDecision, _ int) *ChannelModelsCandidate {
		return decision.Candidate
	})

	// Increment selection count for the top candidate to ensure subsequent
	// concurrent requests see the updated count and select different channels
	if shouldTrackSelection {
		lb.trackSelection(result)
	}

	return result
}

// logDecision logs the complete load balancing decision.
func (lb *LoadBalancer) logDecision(ctx context.Context, candidates []*ChannelModelsCandidate, model string, decisions []ChannelDecision, totalDuration time.Duration) {
	// Log summary
	if len(decisions) > 0 {
		topChannel := decisions[0]
		retryEnabled := false
		maxChannelRetries := 0
		if lb.systemService != nil {
			retryPolicy := lb.systemService.RetryPolicyOrDefault(ctx)
			retryEnabled = retryPolicy.Enabled
			maxChannelRetries = retryPolicy.MaxChannelRetries
		}
		log.Info(ctx, "Load balancing decision completed",
			log.Int("total_channels", len(candidates)),
			log.Int("ordered_channels", len(decisions)),
			log.Bool("retry_enabled", retryEnabled),
			log.Int("max_channel_retries", maxChannelRetries),
			log.Duration("duration", totalDuration),
			log.Int("top_channel_id", topChannel.Channel.ID),
			log.String("top_channel_name", topChannel.Channel.Name),
			log.Float64("top_channel_score", topChannel.TotalScore),
			log.String("model", model),
		)
	}

	// Log individual channel details
	for _, info := range decisions {
		// Create a simplified log entry with strategy breakdown
		strategySummary := make(map[string]any)
		for _, s := range info.StrategyScores {
			strategySummary[s.StrategyName] = map[string]any{
				"score":    s.Score,
				"duration": s.Duration,
			}
		}

		log.Info(ctx, "Channel load balancing details",
			log.Int("channel_id", info.Channel.ID),
			log.String("channel_name", info.Channel.Name),
			log.Float64("total_score", info.TotalScore),
			log.Int("final_rank", info.FinalRank),
			log.Any("strategy_breakdown", strategySummary),
			log.String("model", model),
		)
	}
}
