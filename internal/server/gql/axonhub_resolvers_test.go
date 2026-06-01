package gql

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
)

func setupTestQueryResolver(t *testing.T) (*queryResolver, context.Context, *ent.Client) {
	t.Helper()

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	resolver := &queryResolver{&Resolver{client: client}}

	return resolver, ctx, client
}

func TestQueryResolver_AllChannelSummarys_ProjectProfileUsesIntersection(t *testing.T) {
	resolver, ctx, client := setupTestQueryResolver(t)
	defer client.Close()

	idOnlyChannel, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("ID Only").
		SetCredentials(objects.ChannelCredentials{APIKey: "key-1"}).
		SetSupportedModels([]string{"id-only-model"}).
		SetDefaultTestModel("id-only-model").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	matchingChannel, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Matching").
		SetCredentials(objects.ChannelCredentials{APIKey: "key-2"}).
		SetSupportedModels([]string{"matching-model"}).
		SetDefaultTestModel("matching-model").
		SetStatus(channel.StatusEnabled).
		SetTags([]string{"allowed"}).
		Save(ctx)
	require.NoError(t, err)

	projectEntity, err := client.Project.Create().
		SetName("Project A").
		SetDescription("test project").
		SetProfiles(&objects.ProjectProfiles{
			ActiveProfile: "production",
			Profiles: []objects.ProjectProfile{
				{
					Name:        "production",
					ChannelIDs:  []int{idOnlyChannel.ID, matchingChannel.ID},
					ChannelTags: []string{"allowed"},
				},
			},
		}).
		Save(ctx)
	require.NoError(t, err)

	projectCtx := contexts.WithProjectID(ctx, projectEntity.ID)

	channels, err := resolver.AllChannelSummarys(projectCtx, nil)
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Equal(t, matchingChannel.ID, channels[0].ID)
}

func TestQueryResolver_UpstreamCredentialsIncludesChannelRefs(t *testing.T) {
	resolver, ctx, client := setupTestQueryResolver(t)
	defer client.Close()

	ch, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Credential Channel").
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	fingerprint := biz.ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), ch.BaseURL, "test-upstream-key")
	credential, err := client.UpstreamCredential.Create().
		SetName("test credential").
		SetProviderType(channel.TypeOpenai.String()).
		SetBaseURL(ch.BaseURL).
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretPayload(objects.UpstreamCredentialSecretFromAPIKey("test-upstream-key")).
		SetFingerprint(fingerprint).
		SetStatus(upstreamcredential.StatusEnabled).
		SetWeight(100).
		Save(ctx)
	require.NoError(t, err)

	ref, err := client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	first := 10
	conn, err := resolver.UpstreamCredentials(ctx, nil, &first, nil, nil, nil, &ent.UpstreamCredentialWhereInput{})
	require.NoError(t, err)
	require.Equal(t, 1, conn.TotalCount)
	require.Len(t, conn.Edges, 1)

	node := conn.Edges[0].Node
	require.Equal(t, credential.ID, node.ID)

	nodeID, err := (&upstreamCredentialResolver{resolver.Resolver}).ID(ctx, node)
	require.NoError(t, err)
	require.Equal(t, ent.TypeUpstreamCredential, nodeID.Type)
	require.Equal(t, credential.ID, nodeID.ID)

	loadedRef, err := client.ChannelCredentialRef.Get(ctx, ref.ID)
	require.NoError(t, err)

	refResolver := &channelCredentialRefResolver{resolver.Resolver}
	refID, err := refResolver.ID(ctx, loadedRef)
	require.NoError(t, err)
	require.Equal(t, ent.TypeChannelCredentialRef, refID.Type)
	require.Equal(t, ref.ID, refID.ID)

	refChannelID, err := refResolver.ChannelID(ctx, loadedRef)
	require.NoError(t, err)
	require.Equal(t, ent.TypeChannel, refChannelID.Type)
	require.Equal(t, ch.ID, refChannelID.ID)

	refCredentialID, err := refResolver.CredentialID(ctx, loadedRef)
	require.NoError(t, err)
	require.Equal(t, ent.TypeUpstreamCredential, refCredentialID.Type)
	require.Equal(t, credential.ID, refCredentialID.ID)

	refChannel, err := refResolver.Channel(ctx, loadedRef)
	require.NoError(t, err)
	require.Equal(t, ch.ID, refChannel.ID)

	refCredential, err := refResolver.Credential(ctx, loadedRef)
	require.NoError(t, err)
	require.Equal(t, credential.ID, refCredential.ID)

	fetchedCredential, err := resolver.Node(ctx, objects.GUID{Type: ent.TypeUpstreamCredential, ID: credential.ID})
	require.NoError(t, err)
	require.Equal(t, credential.ID, fetchedCredential.(*ent.UpstreamCredential).ID)

	fetchedRef, err := resolver.Node(ctx, objects.GUID{Type: ent.TypeChannelCredentialRef, ID: ref.ID})
	require.NoError(t, err)
	require.Equal(t, ref.ID, fetchedRef.(*ent.ChannelCredentialRef).ID)

	refConn, err := resolver.ChannelCredentialRefs(ctx, nil, &first, nil, nil, nil, &ent.ChannelCredentialRefWhereInput{})
	require.NoError(t, err)
	require.Equal(t, 1, refConn.TotalCount)
}

func TestQueryResolver_CredentialQuotaScopeResolvers(t *testing.T) {
	resolver, ctx, client := setupTestQueryResolver(t)
	defer client.Close()

	scope, err := client.CredentialQuotaScope.Create().
		SetName("shared quota").
		SetStatus(credentialquotascope.StatusAvailable).
		SetUnit(credentialquotascope.UnitToken).
		Save(ctx)
	require.NoError(t, err)

	first := 10
	conn, err := resolver.CredentialQuotaScopes(ctx, nil, &first, nil, nil, nil, &ent.CredentialQuotaScopeWhereInput{})
	require.NoError(t, err)
	require.Equal(t, 1, conn.TotalCount)
	require.Len(t, conn.Edges, 1)
	require.Equal(t, scope.ID, conn.Edges[0].Node.ID)

	scopeResolver := &credentialQuotaScopeResolver{resolver.Resolver}
	scopeGUID, err := scopeResolver.ID(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, ent.TypeCredentialQuotaScope, scopeGUID.Type)
	require.Equal(t, scope.ID, scopeGUID.ID)

	fetchedScope, err := resolver.Node(ctx, objects.GUID{Type: ent.TypeCredentialQuotaScope, ID: scope.ID})
	require.NoError(t, err)
	require.Equal(t, scope.ID, fetchedScope.(*ent.CredentialQuotaScope).ID)

	scopeID := scope.ID
	credentialResolver := &upstreamCredentialResolver{resolver.Resolver}
	credentialScopeGUID, err := credentialResolver.QuotaScopeID(ctx, &ent.UpstreamCredential{QuotaScopeID: &scopeID})
	require.NoError(t, err)
	require.Equal(t, ent.TypeCredentialQuotaScope, credentialScopeGUID.Type)
	require.Equal(t, scope.ID, credentialScopeGUID.ID)

	credentialScope, err := credentialResolver.QuotaScope(ctx, &ent.UpstreamCredential{QuotaScopeID: &scopeID})
	require.NoError(t, err)
	require.Equal(t, scope.ID, credentialScope.ID)

	executionResolver := &requestExecutionResolver{resolver.Resolver}
	executionScopeGUID, err := executionResolver.QuotaScopeID(ctx, &ent.RequestExecution{QuotaScopeID: scope.ID})
	require.NoError(t, err)
	require.Equal(t, ent.TypeCredentialQuotaScope, executionScopeGUID.Type)
	require.Equal(t, scope.ID, executionScopeGUID.ID)

	executionScope, err := executionResolver.QuotaScope(ctx, &ent.RequestExecution{QuotaScopeID: scope.ID})
	require.NoError(t, err)
	require.Equal(t, scope.ID, executionScope.ID)

	usageResolver := &usageLogResolver{resolver.Resolver}
	usageScopeGUID, err := usageResolver.QuotaScopeID(ctx, &ent.UsageLog{QuotaScopeID: scope.ID})
	require.NoError(t, err)
	require.Equal(t, ent.TypeCredentialQuotaScope, usageScopeGUID.Type)
	require.Equal(t, scope.ID, usageScopeGUID.ID)

	usageScope, err := usageResolver.QuotaScope(ctx, &ent.UsageLog{QuotaScopeID: scope.ID})
	require.NoError(t, err)
	require.Equal(t, scope.ID, usageScope.ID)

	providerQuotaResolver := &providerQuotaStatusResolver{resolver.Resolver}
	providerQuotaScopeGUID, err := providerQuotaResolver.QuotaScopeID(ctx, &ent.ProviderQuotaStatus{QuotaScopeID: scope.ID})
	require.NoError(t, err)
	require.Equal(t, ent.TypeCredentialQuotaScope, providerQuotaScopeGUID.Type)
	require.Equal(t, scope.ID, providerQuotaScopeGUID.ID)
}

func TestQueryResolver_AllChannelTags_ProjectProfileFiltersVisibleTags(t *testing.T) {
	resolver, ctx, client := setupTestQueryResolver(t)
	defer client.Close()

	_, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Visible Channel").
		SetCredentials(objects.ChannelCredentials{APIKey: "key-visible"}).
		SetSupportedModels([]string{"visible-model"}).
		SetDefaultTestModel("visible-model").
		SetStatus(channel.StatusEnabled).
		SetTags([]string{"shared", "visible"}).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Hidden Channel").
		SetCredentials(objects.ChannelCredentials{APIKey: "key-hidden"}).
		SetSupportedModels([]string{"hidden-model"}).
		SetDefaultTestModel("hidden-model").
		SetStatus(channel.StatusEnabled).
		SetTags([]string{"shared", "hidden"}).
		Save(ctx)
	require.NoError(t, err)

	projectEntity, err := client.Project.Create().
		SetName("Project B").
		SetDescription("test project").
		SetProfiles(&objects.ProjectProfiles{
			ActiveProfile: "production",
			Profiles: []objects.ProjectProfile{
				{
					Name:        "production",
					ChannelTags: []string{"visible"},
				},
			},
		}).
		Save(ctx)
	require.NoError(t, err)

	projectCtx := contexts.WithProjectID(ctx, projectEntity.ID)

	tags, err := resolver.AllChannelTags(projectCtx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"shared", "visible"}, lo.Uniq(tags))
}
