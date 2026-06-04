package api

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
)

type oauthHandlerTestDeps struct {
	client                    *ent.Client
	upstreamCredentialService *biz.UpstreamCredentialService
}

func newOAuthHandlerTestDeps(t *testing.T) oauthHandlerTestDeps {
	t.Helper()

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	systemService := biz.NewSystemService(biz.SystemServiceParams{
		CacheConfig: xcache.Config{Mode: xcache.ModeMemory},
		Ent:         client,
	})

	return oauthHandlerTestDeps{
		client: client,
		upstreamCredentialService: biz.NewUpstreamCredentialService(biz.UpstreamCredentialServiceParams{
			Ent:           client,
			SystemService: systemService,
		}),
	}
}

func oauthTestContextMiddleware(client *ent.Client, projectID int) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := authz.WithTestBypass(c.Request.Context())
		if projectID > 0 {
			ctx = contexts.WithProjectID(ctx, projectID)
		}
		ctx = ent.NewContext(ctx, client)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func oauthBypassContext(client *ent.Client) context.Context {
	return ent.NewContext(authz.WithTestBypass(context.Background()), client)
}
