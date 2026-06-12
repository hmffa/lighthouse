package adminapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-oidfed/lib/cache"
	"github.com/gofiber/fiber/v2"

	"github.com/go-oidfed/lighthouse/internal"
)

func setCacheEntry(t *testing.T, key string, value []byte) {
	t.Helper()
	_ = cache.Delete(key)
	if err := cache.Set(key, value, time.Minute); err != nil {
		t.Fatalf("failed to seed cache for %q: %v", key, err)
	}
	t.Cleanup(func() {
		_ = cache.Delete(key)
	})
}

func requireCacheEntry(t *testing.T, key string, wantSet bool, wantValue []byte) {
	t.Helper()
	var got []byte
	set, err := cache.Get(key, &got)
	if err != nil {
		t.Fatalf("failed to read cache key %q: %v", key, err)
	}
	if set != wantSet {
		t.Fatalf("expected cache present=%v for %q, got %v", wantSet, key, set)
	}
	if wantSet && !bytes.Equal(got, wantValue) {
		t.Fatalf("expected cached value %q for %q, got %q", string(wantValue), key, string(got))
	}
	if !wantSet && len(got) != 0 {
		t.Fatalf("expected empty cached value for cleared key %q, got %q", key, string(got))
	}
}

// TestEntityConfigurationCacheInvalidationMiddleware must NOT use t.Parallel().
// It operates on the global process-wide cache (cache.Set/Get/Delete), which is
// shared mutable state. Parallelizing these subtests would cause race conditions
// on the entity configuration cache key.
func TestEntityConfigurationCacheInvalidationMiddleware(t *testing.T) {
	cacheValue := []byte("entity-config-jwt")

	t.Run("SuccessDeletesCache", func(t *testing.T) {
		setEntityConfigurationCache(t, cacheValue)
		app := fiber.New()
		app.Post("/entity-config", entityConfigurationCacheInvalidationMiddleware, func(c *fiber.Ctx) error {
			return c.SendStatus(http.StatusNoContent)
		})

		req := httptest.NewRequest(http.MethodPost, "/entity-config", http.NoBody)
		resp, bodyBytes := doRequest(t, app, req)
		requireStatus(t, resp, bodyBytes, http.StatusNoContent)
		requireEntityConfigurationCache(t, false, nil)
	})

	t.Run("FailureKeepsCache", func(t *testing.T) {
		setEntityConfigurationCache(t, cacheValue)
		app := fiber.New()
		app.Post("/entity-config", entityConfigurationCacheInvalidationMiddleware, func(c *fiber.Ctx) error {
			return c.SendStatus(http.StatusBadRequest)
		})

		req := httptest.NewRequest(http.MethodPost, "/entity-config", http.NoBody)
		resp, bodyBytes := doRequest(t, app, req)
		requireStatus(t, resp, bodyBytes, http.StatusBadRequest)
		requireEntityConfigurationCache(t, true, cacheValue)
	})

	t.Run("Exact200DeletesCache", func(t *testing.T) {
		setEntityConfigurationCache(t, cacheValue)
		app := fiber.New()
		app.Post("/entity-config", entityConfigurationCacheInvalidationMiddleware, func(c *fiber.Ctx) error {
			return c.SendString("ok") // exactly 200, the lower boundary
		})

		req := httptest.NewRequest(http.MethodPost, "/entity-config", http.NoBody)
		resp, bodyBytes := doRequest(t, app, req)
		requireStatus(t, resp, bodyBytes, http.StatusOK)
		requireEntityConfigurationCache(t, false, nil)
	})

	// PIN: a 3xx redirect currently CLEARS the cache — the middleware treats
	// everything in [200,400) as a successful mutation. No redirects exist on
	// these routes today; an earlier (commented-out) draft expected redirects
	// to keep the cache instead. If the intended boundary ever changes to
	// status < 300, flip this assertion.
	t.Run("RedirectClearsCache", func(t *testing.T) {
		setEntityConfigurationCache(t, cacheValue)
		app := fiber.New()
		app.Post("/entity-config", entityConfigurationCacheInvalidationMiddleware, func(c *fiber.Ctx) error {
			return c.Redirect("/other", fiber.StatusMovedPermanently)
		})

		req := httptest.NewRequest(http.MethodPost, "/entity-config", http.NoBody)
		resp, bodyBytes := doRequest(t, app, req)
		requireStatus(t, resp, bodyBytes, fiber.StatusMovedPermanently)
		requireEntityConfigurationCache(t, false, nil)
	})

	t.Run("HandlerErrorKeepsCache", func(t *testing.T) {
		setEntityConfigurationCache(t, cacheValue)
		app := fiber.New()
		app.Post("/entity-config", entityConfigurationCacheInvalidationMiddleware, func(_ *fiber.Ctx) error {
			return fiber.ErrConflict
		})

		req := httptest.NewRequest(http.MethodPost, "/entity-config", http.NoBody)
		resp, bodyBytes := doRequest(t, app, req)
		requireStatus(t, resp, bodyBytes, http.StatusConflict)
		requireEntityConfigurationCache(t, true, cacheValue)
	})
}

// TestSubordinateStatementsCacheInvalidationMiddleware must NOT use t.Parallel().
// It operates on the global process-wide cache (cache.Set/Get/Delete), which is
// shared mutable state. Parallelizing these subtests would cause race conditions
// on the subordinate statement cache keys.
func TestSubordinateStatementsCacheInvalidationMiddleware(t *testing.T) {
	key123 := cache.Key(internal.CacheKeySubordinateStatement, "123")
	key456 := cache.Key(internal.CacheKeySubordinateStatement, "456")
	value123 := []byte("statement-123")
	value456 := []byte("statement-456")

	t.Run("SpecificSubordinateDeletesOnlyTarget", func(t *testing.T) {
		setCacheEntry(t, key123, value123)
		setCacheEntry(t, key456, value456)
		app := fiber.New()
		app.Delete("/subordinates/:subordinateID", subordinateStatementsCacheInvalidationMiddleware, func(c *fiber.Ctx) error {
			return c.SendStatus(http.StatusNoContent)
		})

		req := httptest.NewRequest(http.MethodDelete, "/subordinates/123", http.NoBody)
		resp, bodyBytes := doRequest(t, app, req)
		requireStatus(t, resp, bodyBytes, http.StatusNoContent)
		requireCacheEntry(t, key123, false, nil)
		requireCacheEntry(t, key456, true, value456)
	})

	t.Run("CollectionSuccessClearsAll", func(t *testing.T) {
		setCacheEntry(t, key123, value123)
		setCacheEntry(t, key456, value456)
		app := fiber.New()
		app.Post("/subordinates", subordinateStatementsCacheInvalidationMiddleware, func(c *fiber.Ctx) error {
			return c.SendStatus(http.StatusCreated)
		})

		req := httptest.NewRequest(http.MethodPost, "/subordinates", http.NoBody)
		resp, bodyBytes := doRequest(t, app, req)
		requireStatus(t, resp, bodyBytes, http.StatusCreated)
		requireCacheEntry(t, key123, false, nil)
		requireCacheEntry(t, key456, false, nil)
	})

	t.Run("FailureKeepsAll", func(t *testing.T) {
		setCacheEntry(t, key123, value123)
		setCacheEntry(t, key456, value456)
		app := fiber.New()
		app.Delete("/subordinates/:subordinateID", subordinateStatementsCacheInvalidationMiddleware, func(c *fiber.Ctx) error {
			return c.SendStatus(http.StatusInternalServerError)
		})

		req := httptest.NewRequest(http.MethodDelete, "/subordinates/123", http.NoBody)
		resp, bodyBytes := doRequest(t, app, req)
		requireStatus(t, resp, bodyBytes, http.StatusInternalServerError)
		requireCacheEntry(t, key123, true, value123)
		requireCacheEntry(t, key456, true, value456)
	})

	t.Run("Exact200DeletesOnlyTarget", func(t *testing.T) {
		setCacheEntry(t, key123, value123)
		setCacheEntry(t, key456, value456)
		app := fiber.New()
		app.Put("/subordinates/:subordinateID", subordinateStatementsCacheInvalidationMiddleware, func(c *fiber.Ctx) error {
			return c.SendString("ok") // exactly 200, the lower boundary
		})

		req := httptest.NewRequest(http.MethodPut, "/subordinates/123", http.NoBody)
		resp, bodyBytes := doRequest(t, app, req)
		requireStatus(t, resp, bodyBytes, http.StatusOK)
		requireCacheEntry(t, key123, false, nil)
		requireCacheEntry(t, key456, true, value456)
	})

	// PIN: a 3xx redirect currently CLEARS the targeted statement — same
	// [200,400) boundary as the entity-configuration middleware above.
	t.Run("RedirectClearsTarget", func(t *testing.T) {
		setCacheEntry(t, key123, value123)
		setCacheEntry(t, key456, value456)
		app := fiber.New()
		app.Put("/subordinates/:subordinateID", subordinateStatementsCacheInvalidationMiddleware, func(c *fiber.Ctx) error {
			return c.Redirect("/other", fiber.StatusMovedPermanently)
		})

		req := httptest.NewRequest(http.MethodPut, "/subordinates/123", http.NoBody)
		resp, bodyBytes := doRequest(t, app, req)
		requireStatus(t, resp, bodyBytes, fiber.StatusMovedPermanently)
		requireCacheEntry(t, key123, false, nil)
		requireCacheEntry(t, key456, true, value456)
	})

	t.Run("HandlerErrorKeepsAll", func(t *testing.T) {
		setCacheEntry(t, key123, value123)
		setCacheEntry(t, key456, value456)
		app := fiber.New()
		app.Delete("/subordinates/:subordinateID", subordinateStatementsCacheInvalidationMiddleware, func(_ *fiber.Ctx) error {
			return fiber.ErrConflict
		})

		req := httptest.NewRequest(http.MethodDelete, "/subordinates/123", http.NoBody)
		resp, bodyBytes := doRequest(t, app, req)
		requireStatus(t, resp, bodyBytes, http.StatusConflict)
		requireCacheEntry(t, key123, true, value123)
		requireCacheEntry(t, key456, true, value456)
	})
}
