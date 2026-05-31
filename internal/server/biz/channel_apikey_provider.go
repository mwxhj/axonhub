package biz

import (
	"context"
	"hash/fnv"
	"math/rand/v2"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
)

// traceStickyLRUSize is the default LRU cache size for trace-to-key mappings.
const traceStickyLRUSize = 1024

// TraceStickyKeyProvider selects an API key credential deterministically per
// traceID or sticky-session seed, using cached credential views from the
// channel snapshot.
//
// An LRU cache remembers previous traceID→key selections so that, as long as
// the previously chosen key is still enabled, the same key is returned even when
// the enabled-key set changes (e.g. a new key is added). This improves sticky
// stability compared to pure rendezvous hashing alone.
//
//nolint:revive // exported for use in transformers via interface.
type TraceStickyKeyProvider struct {
	channel *Channel
	cache   *lru.Cache[string, string]
}

func NewTraceStickyKeyProvider(channel *Channel) *TraceStickyKeyProvider {
	cache, _ := lru.New[string, string](traceStickyLRUSize)

	return &TraceStickyKeyProvider{
		channel: channel,
		cache:   cache,
	}
}

func (p *TraceStickyKeyProvider) Get(ctx context.Context) string {
	enabled := p.enabledAPIKeyCredentialViews()
	if len(enabled) == 0 {
		allKeys := p.channel.Credentials.GetAllAPIKeys()
		if len(allKeys) == 0 {
			return ""
		}

		selectedKey := allKeys[0]
		p.storeSelectedLegacyKey(ctx, selectedKey)
		return selectedKey
	}

	if len(enabled) == 1 {
		p.storeSelectedCredential(ctx, enabled[0])
		return enabled[0].Secret.APIKey
	}

	var selected *ChannelCredentialView

	if preferredID, ok := contexts.GetPreferredCredentialID(ctx); ok && preferredID > 0 {
		selected = p.selectPreferredCredential(enabled, preferredID, "")
	}
	if selected == nil {
		if preferred, ok := contexts.GetPreferredCredentialFingerprint(ctx); ok && preferred != "" {
			selected = p.selectPreferredCredential(enabled, 0, preferred)
		}
	}
	if selected != nil && log.DebugEnabled(ctx) {
		log.Debug(ctx, "Preferred credential key selected",
			log.Int("credential_id", selected.CredentialID),
			log.String("credential_fingerprint", selected.Fingerprint),
		)
	}

	if selected == nil {
		selected = p.selectSeededCredential(ctx, enabled)
	}
	if selected == nil {
		//nolint:gosec // not a security issue, just a random selection.
		selected = &enabled[rand.IntN(len(enabled))]
		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "Random key selected",
				log.Int("credential_id", selected.CredentialID),
				log.String("credential_fingerprint", selected.Fingerprint),
			)
		}
	}

	p.storeSelectedCredential(ctx, *selected)

	return selected.Secret.APIKey
}

func (p *TraceStickyKeyProvider) enabledAPIKeyCredentialViews() []ChannelCredentialView {
	if p == nil || p.channel == nil {
		return nil
	}

	if len(p.channel.cachedCredentialViews) > 0 {
		return enabledAPIKeyCredentialViews(p.channel.cachedCredentialViews)
	}

	if p.channel.cachedEnabledAPIKeys != nil {
		return legacyCredentialViewsForAPIKeys(p.channel, p.channel.cachedEnabledAPIKeys)
	}

	return enabledAPIKeyCredentialViews(p.channel.enabledCredentialViews())
}

func (p *TraceStickyKeyProvider) selectSeededCredential(ctx context.Context, enabled []ChannelCredentialView) *ChannelCredentialView {
	var selected *ChannelCredentialView

	if seed, ok := contexts.GetCredentialSelectionSeed(ctx); ok && seed != "" {
		selected = p.selectStickyCredential(enabled, "sticky:"+seed)

		if selected != nil && log.DebugEnabled(ctx) {
			log.Debug(ctx, "Sticky credential key selected",
				log.Int("credential_id", selected.CredentialID),
				log.String("credential_fingerprint", selected.Fingerprint),
			)
		}
	} else if trace, ok := contexts.GetTrace(ctx); ok && trace != nil {
		selected = p.selectStickyCredential(enabled, "trace:"+trace.TraceID)

		if selected != nil && log.DebugEnabled(ctx) {
			log.Debug(ctx, "Trace sticky key selected",
				log.String("trace_id", trace.TraceID),
				log.Int("credential_id", selected.CredentialID),
				log.String("credential_fingerprint", selected.Fingerprint))
		}
	}

	return selected
}

func (p *TraceStickyKeyProvider) selectStickyCredential(enabled []ChannelCredentialView, seed string) *ChannelCredentialView {
	if cached, ok := p.cache.Get(seed); ok {
		for i := range enabled {
			if enabled[i].Fingerprint == cached {
				return &enabled[i]
			}
		}
	}

	selected := p.rendezvousSelectByCredential(enabled, seed)
	if selected == nil {
		return nil
	}
	p.cache.Add(seed, selected.Fingerprint)

	return selected
}

func (p *TraceStickyKeyProvider) rendezvousSelectByCredential(views []ChannelCredentialView, seed string) *ChannelCredentialView {
	if len(views) == 0 {
		return nil
	}

	bestIdx := 0
	bestScore := credentialWeightedScore(seed, views[0])

	for i := 1; i < len(views); i++ {
		s := credentialWeightedScore(seed, views[i])

		if s > bestScore {
			bestScore = s
			bestIdx = i
		}
	}

	return &views[bestIdx]
}

func (p *TraceStickyKeyProvider) selectPreferredCredential(enabled []ChannelCredentialView, credentialID int, fingerprint string) *ChannelCredentialView {
	for i := range enabled {
		if credentialID > 0 && enabled[i].CredentialID == credentialID {
			return &enabled[i]
		}
		if fingerprint != "" && enabled[i].Fingerprint == fingerprint {
			return &enabled[i]
		}
	}

	return nil
}

func (p *TraceStickyKeyProvider) storeSelectedCredential(ctx context.Context, selected ChannelCredentialView) {
	contexts.WithChannelCredential(ctx, selected.CredentialID, selected.Secret.APIKey, selected.Fingerprint)
}

func (p *TraceStickyKeyProvider) storeSelectedLegacyKey(ctx context.Context, selectedKey string) {
	contexts.WithChannelCredential(ctx, 0, selectedKey, p.channel.CredentialFingerprintForAPIKey(selectedKey))
}

// rendezvousSelect picks a key using Highest Random Weight (Rendezvous) hashing.
// This is stable when the key set changes (minimal remapping compared to modulo).
func rendezvousSelect(keys []string, seed string) string {
	bestKey := keys[0]
	bestScore := hashAPIKey(seed + "|" + bestKey)

	for i := 1; i < len(keys); i++ {
		k := keys[i]

		s := hashAPIKey(seed + "|" + k)
		if s > bestScore {
			bestScore = s
			bestKey = k
		}
	}

	return bestKey
}

func hashAPIKey(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))

	return h.Sum64()
}

func credentialWeightedScore(seed string, view ChannelCredentialView) float64 {
	weight := view.Weight
	if weight <= 0 {
		weight = 1
	}

	key := view.Fingerprint
	if key == "" {
		key = view.Secret.APIKey
	}

	return float64(hashAPIKey(seed+"|"+key)) * float64(weight)
}
