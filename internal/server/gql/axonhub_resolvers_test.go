package gql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/channelcredentialref"
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

}

func TestGraphQLArchiveUpstreamCredentialMutation(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	upstreamCredentialService := biz.NewUpstreamCredentialService(biz.UpstreamCredentialServiceParams{Ent: client})
	handler := NewGraphqlHandlers(Dependencies{
		Ent:                       client,
		UpstreamCredentialService: upstreamCredentialService,
	})

	credential, err := upstreamCredentialService.CreateUpstreamCredential(ctx, biz.CreateUpstreamCredentialInput{
		Name:   lo.ToPtr("archive graphql"),
		Secret: objects.UpstreamCredentialSecretFromAPIKey("sk-graphql-archive"),
		Status: lo.ToPtr(upstreamcredential.StatusEnabled),
	})
	require.NoError(t, err)

	ch, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Archive Channel").
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	ref, err := client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	query := `
		mutation ArchiveUpstreamCredential($id: ID!) {
			archiveUpstreamCredential(id: $id) {
				id
				status
				channelRefs(first: 100) {
					totalCount
					edges {
						node {
							id
							enabled
						}
					}
				}
				secretSummary {
					kind
					providerType
					issuerScope
				}
			}
		}
	`

	body, err := json.Marshal(map[string]any{
		"query":         query,
		"operationName": "ArchiveUpstreamCredential",
		"variables": map[string]any{
			"id": fmt.Sprintf("gid://axonhub/%s/%d", ent.TypeUpstreamCredential, credential.ID),
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/admin/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(authz.WithTestBypass(req.Context()))
	rec := httptest.NewRecorder()

	handler.Graphql.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var payload struct {
		Data struct {
			ArchiveUpstreamCredential struct {
				ID          string `json:"id"`
				Status      string `json:"status"`
				ChannelRefs struct {
					TotalCount int `json:"totalCount"`
					Edges      []struct {
						Node struct {
							ID      string `json:"id"`
							Enabled bool   `json:"enabled"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"channelRefs"`
			} `json:"archiveUpstreamCredential"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Empty(t, payload.Errors, rec.Body.String())
	require.NotEmpty(t, payload.Data.ArchiveUpstreamCredential.ID)
	require.Equal(t, "archived", payload.Data.ArchiveUpstreamCredential.Status)
	require.Equal(t, 1, payload.Data.ArchiveUpstreamCredential.ChannelRefs.TotalCount)
	require.Len(t, payload.Data.ArchiveUpstreamCredential.ChannelRefs.Edges, 1)
	require.True(t, payload.Data.ArchiveUpstreamCredential.ChannelRefs.Edges[0].Node.Enabled)

	reloadedRef, err := client.ChannelCredentialRef.Query().
		Where(channelcredentialref.ID(ref.ID)).
		Only(ctx)
	require.NoError(t, err)
	require.True(t, reloadedRef.Enabled)
}

func TestGraphQLCreateUpstreamCredentialReactivatesArchivedSameSecret(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	upstreamCredentialService := biz.NewUpstreamCredentialService(biz.UpstreamCredentialServiceParams{Ent: client})
	handler := NewGraphqlHandlers(Dependencies{
		Ent:                       client,
		UpstreamCredentialService: upstreamCredentialService,
	})

	secret := objects.UpstreamCredentialSecretFromAPIKey("sk-graphql-reactivate")
	credential, err := upstreamCredentialService.CreateUpstreamCredential(ctx, biz.CreateUpstreamCredentialInput{
		Name:   lo.ToPtr("archived graphql"),
		Secret: secret,
		Status: lo.ToPtr(upstreamcredential.StatusEnabled),
	})
	require.NoError(t, err)

	ch, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Reactivate Channel").
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	ref, err := client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	_, err = upstreamCredentialService.ArchiveUpstreamCredential(ctx, credential.ID)
	require.NoError(t, err)
	_, err = client.ChannelCredentialRef.UpdateOneID(ref.ID).SetEnabled(false).Save(ctx)
	require.NoError(t, err)

	query := `
		mutation CreateUpstreamCredential($input: CreateUpstreamCredentialInput!) {
			createUpstreamCredential(input: $input) {
				id
				name
				status
				remark
				channelRefs(first: 100) {
					totalCount
					edges {
						node {
							id
							enabled
						}
					}
				}
			}
		}
	`

	body, err := json.Marshal(map[string]any{
		"query":         query,
		"operationName": "CreateUpstreamCredential",
		"variables": map[string]any{
			"input": map[string]any{
				"name":   "reactivated graphql",
				"secret": map[string]any{"apiKey": "sk-graphql-reactivate"},
				"status": "enabled",
				"remark": "restored",
			},
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/admin/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(authz.WithTestBypass(req.Context()))
	rec := httptest.NewRecorder()

	handler.Graphql.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var payload struct {
		Data struct {
			CreateUpstreamCredential struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Status      string `json:"status"`
				Remark      string `json:"remark"`
				ChannelRefs struct {
					TotalCount int `json:"totalCount"`
					Edges      []struct {
						Node struct {
							ID      string `json:"id"`
							Enabled bool   `json:"enabled"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"channelRefs"`
			} `json:"createUpstreamCredential"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Empty(t, payload.Errors, rec.Body.String())
	require.Equal(t, fmt.Sprintf("gid://axonhub/%s/%d", ent.TypeUpstreamCredential, credential.ID), payload.Data.CreateUpstreamCredential.ID)
	require.Equal(t, "reactivated graphql", payload.Data.CreateUpstreamCredential.Name)
	require.Equal(t, "enabled", payload.Data.CreateUpstreamCredential.Status)
	require.Equal(t, "restored", payload.Data.CreateUpstreamCredential.Remark)
	require.Equal(t, 1, payload.Data.CreateUpstreamCredential.ChannelRefs.TotalCount)
	require.Len(t, payload.Data.CreateUpstreamCredential.ChannelRefs.Edges, 1)
	require.True(t, payload.Data.CreateUpstreamCredential.ChannelRefs.Edges[0].Node.Enabled)
}

func TestGraphQLDeleteUpstreamCredentialMutation(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	upstreamCredentialService := biz.NewUpstreamCredentialService(biz.UpstreamCredentialServiceParams{Ent: client})
	handler := NewGraphqlHandlers(Dependencies{
		Ent:                       client,
		UpstreamCredentialService: upstreamCredentialService,
	})

	credential, err := upstreamCredentialService.CreateUpstreamCredential(ctx, biz.CreateUpstreamCredentialInput{
		Name:   lo.ToPtr("delete graphql"),
		Secret: objects.UpstreamCredentialSecretFromAPIKey("sk-graphql-delete"),
		Status: lo.ToPtr(upstreamcredential.StatusEnabled),
	})
	require.NoError(t, err)

	ch, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Delete Channel").
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	query := `
		mutation DeleteUpstreamCredential($id: ID!) {
			deleteUpstreamCredential(id: $id)
		}
	`
	body, err := json.Marshal(map[string]any{
		"query":         query,
		"operationName": "DeleteUpstreamCredential",
		"variables": map[string]any{
			"id": fmt.Sprintf("gid://axonhub/%s/%d", ent.TypeUpstreamCredential, credential.ID),
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/admin/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(authz.WithTestBypass(req.Context()))
	rec := httptest.NewRecorder()

	handler.Graphql.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var payload struct {
		Data struct {
			DeleteUpstreamCredential bool `json:"deleteUpstreamCredential"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Empty(t, payload.Errors, rec.Body.String())
	require.True(t, payload.Data.DeleteUpstreamCredential)

	refCount, err := client.ChannelCredentialRef.Query().
		Where(channelcredentialref.CredentialID(credential.ID)).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, refCount)

	credentialCount, err := client.UpstreamCredential.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, credentialCount)
}

func TestGraphQLCreateUpstreamCredentialMutation(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()

	upstreamCredentialService := biz.NewUpstreamCredentialService(biz.UpstreamCredentialServiceParams{Ent: client})
	handler := NewGraphqlHandlers(Dependencies{
		Ent:                       client,
		UpstreamCredentialService: upstreamCredentialService,
	})

	query := `
		mutation CreateUpstreamCredential($input: CreateUpstreamCredentialInput!) {
			createUpstreamCredential(input: $input) {
				id
				name
				quotaScopeID
				quotaScope {
					id
					name
					status
					unit
					limitAmount
					usedAmount
					resetPolicy
					warningThresholdPercent
					overLimitAction
				}
				status
				remark
				createdAt
				updatedAt
				channelRefs(first: 100) {
					totalCount
					edges {
						node {
							id
							channelID
							credentialID
							enabled
						}
					}
				}
				secretSummary {
					kind
					providerType
					issuerScope
				}
			}
		}
	`

	tests := []struct {
		name           string
		input          map[string]any
		wantQuotaScope bool
		wantKind       string
		wantProvider   string
		wantIssuer     string
	}{
		{
			name: "no quota",
			input: map[string]any{
				"name":   "plain key",
				"secret": map[string]any{"apiKey": "sk-graphql-no-quota"},
				"status": "enabled",
			},
			wantKind:   "api_key",
			wantIssuer: "openai",
		},
		{
			name: "inline quota",
			input: map[string]any{
				"name":   "budgeted key",
				"secret": map[string]any{"apiKey": "sk-graphql-inline-quota"},
				"status": "enabled",
				"quota": map[string]any{
					"name":                    "graphql budget",
					"unit":                    "token",
					"limitAmount":             "1000",
					"usedAmount":              "0",
					"resetPolicy":             "monthly",
					"warningThresholdPercent": 80,
					"overLimitAction":         "warn",
				},
			},
			wantQuotaScope: true,
			wantKind:       "api_key",
			wantIssuer:     "openai",
		},
		{
			name: "oauth credential scope",
			input: map[string]any{
				"name":         "codex oauth",
				"providerType": "codex",
				"issuerScope":  "openai",
				"secret": map[string]any{
					"apiKey": `{"access_token":"access-token","refresh_token":"refresh-token","token_type":"Bearer"}`,
					"oauth": map[string]any{
						"accessToken":  "access-token",
						"refreshToken": "refresh-token",
						"tokenType":    "Bearer",
					},
				},
				"status": "enabled",
			},
			wantKind:     "oauth",
			wantProvider: "codex",
			wantIssuer:   "openai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"query":         query,
				"operationName": "CreateUpstreamCredential",
				"variables": map[string]any{
					"input": tt.input,
				},
			})
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/admin/graphql", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(authz.WithTestBypass(req.Context()))
			rec := httptest.NewRecorder()

			handler.Graphql.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var payload struct {
				Data struct {
					CreateUpstreamCredential struct {
						ID           string          `json:"id"`
						Name         string          `json:"name"`
						QuotaScopeID *string         `json:"quotaScopeID"`
						QuotaScope   json.RawMessage `json:"quotaScope"`
						Status       string          `json:"status"`
						ChannelRefs       struct {
							TotalCount int `json:"totalCount"`
						} `json:"channelRefs"`
						SecretSummary struct {
							Kind         string  `json:"kind"`
							ProviderType *string `json:"providerType"`
							IssuerScope  *string `json:"issuerScope"`
						} `json:"secretSummary"`
					} `json:"createUpstreamCredential"`
				} `json:"data"`
				Errors []struct {
					Message string `json:"message"`
				} `json:"errors"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
			require.Empty(t, payload.Errors, rec.Body.String())
			require.NotEmpty(t, payload.Data.CreateUpstreamCredential.ID)
			require.Equal(t, "enabled", payload.Data.CreateUpstreamCredential.Status)
			require.Equal(t, 0, payload.Data.CreateUpstreamCredential.ChannelRefs.TotalCount)
			require.Equal(t, tt.wantKind, payload.Data.CreateUpstreamCredential.SecretSummary.Kind)
			if tt.wantProvider != "" {
				require.Equal(t, tt.wantProvider, stringValue(payload.Data.CreateUpstreamCredential.SecretSummary.ProviderType))
			}
			if tt.wantIssuer != "" {
				require.Equal(t, tt.wantIssuer, stringValue(payload.Data.CreateUpstreamCredential.SecretSummary.IssuerScope))
			}
			if tt.wantQuotaScope {
				require.NotNil(t, payload.Data.CreateUpstreamCredential.QuotaScopeID)
				require.NotEqual(t, json.RawMessage("null"), payload.Data.CreateUpstreamCredential.QuotaScope)
			} else {
				require.Nil(t, payload.Data.CreateUpstreamCredential.QuotaScopeID)
				require.Equal(t, json.RawMessage("null"), payload.Data.CreateUpstreamCredential.QuotaScope)
			}
		})
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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
