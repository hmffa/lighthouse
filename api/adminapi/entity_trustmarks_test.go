package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	smodel "github.com/go-oidfed/lighthouse/storage/model"
)

type mockPublishedTrustMarksStore struct {
	listFn   func() ([]smodel.PublishedTrustMark, error)
	createFn func(smodel.AddTrustMark) (*smodel.PublishedTrustMark, error)
	getFn    func(string) (*smodel.PublishedTrustMark, error)
	updateFn func(string, smodel.AddTrustMark) (*smodel.PublishedTrustMark, error)
	patchFn  func(string, smodel.UpdateTrustMark) (*smodel.PublishedTrustMark, error)
	deleteFn func(string) error
}

func (m *mockPublishedTrustMarksStore) List() ([]smodel.PublishedTrustMark, error) {
	return m.listFn()
}

func (m *mockPublishedTrustMarksStore) Create(item smodel.AddTrustMark) (*smodel.PublishedTrustMark, error) {
	return m.createFn(item)
}

func (m *mockPublishedTrustMarksStore) Get(ident string) (*smodel.PublishedTrustMark, error) {
	return m.getFn(ident)
}

func (m *mockPublishedTrustMarksStore) Update(ident string, item smodel.AddTrustMark) (*smodel.PublishedTrustMark, error) {
	return m.updateFn(ident, item)
}

func (m *mockPublishedTrustMarksStore) Patch(ident string, item smodel.UpdateTrustMark) (*smodel.PublishedTrustMark, error) {
	return m.patchFn(ident, item)
}

func (m *mockPublishedTrustMarksStore) Delete(ident string) error {
	return m.deleteFn(ident)
}

type mockTrustMarkConfigInvalidator struct {
	calls int
}

func (m *mockTrustMarkConfigInvalidator) Invalidate() {
	m.calls++
}

func setupEntityTrustMarksTestApp(
	store smodel.PublishedTrustMarksStore,
	configInvalidator TrustMarkConfigInvalidator,
) *fiber.App {
	app := fiber.New()
	registerEntityTrustMarks(app, store, configInvalidator)
	return app
}

func TestEntityTrustMarksList(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			listFn: func() ([]smodel.PublishedTrustMark, error) {
				return []smodel.PublishedTrustMark{{ID: 1, TrustMarkType: "https://tm.example/type"}}, nil
			},
		}, nil)

		req := httptest.NewRequest(http.MethodGet, "/entity-configuration/trust-marks/", http.NoBody)
		resp, body := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusOK)

		var got []smodel.PublishedTrustMark
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if len(got) != 1 || got[0].TrustMarkType != "https://tm.example/type" {
			t.Fatalf("unexpected trust mark list: %+v", got)
		}
	})

	t.Run("StoreError", func(t *testing.T) {
		t.Parallel()

		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			listFn: func() ([]smodel.PublishedTrustMark, error) {
				return nil, errors.New("db down")
			},
		}, nil)

		req := httptest.NewRequest(http.MethodGet, "/entity-configuration/trust-marks/", http.NoBody)
		resp, _ := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusInternalServerError)
	})
}

func TestEntityTrustMarksCreate(t *testing.T) {
	cacheValue := []byte("cached-entity-config")

	t.Run("SuccessInvalidatesCacheAndConfig", func(t *testing.T) {
		invalidator := &mockTrustMarkConfigInvalidator{}
		var gotInput smodel.AddTrustMark
		setEntityConfigurationCache(t, cacheValue)
		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			createFn: func(item smodel.AddTrustMark) (*smodel.PublishedTrustMark, error) {
				gotInput = item
				return &smodel.PublishedTrustMark{ID: 3, TrustMarkType: item.TrustMarkType, TrustMarkIssuer: item.TrustMarkIssuer, Refresh: item.Refresh}, nil
			},
		}, invalidator)

		req := httptest.NewRequest(
			http.MethodPost,
			"/entity-configuration/trust-marks/",
			strings.NewReader(`{"trust_mark_type":"https://tm.example/type","trust_mark_issuer":"https://issuer.example","refresh":true}`),
		)
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		resp, body := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusCreated)

		var got smodel.PublishedTrustMark
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if gotInput.TrustMarkType != "https://tm.example/type" || gotInput.TrustMarkIssuer != "https://issuer.example" || !gotInput.Refresh {
			t.Fatalf("unexpected create input: %+v", gotInput)
		}
		if got.ID != 3 || got.TrustMarkType != gotInput.TrustMarkType {
			t.Fatalf("unexpected response payload: %+v", got)
		}
		if invalidator.calls != 1 {
			t.Fatalf("expected invalidator to be called once, got %d", invalidator.calls)
		}
		requireEntityConfigurationCache(t, false, nil)
	})

	t.Run("InvalidBodyKeepsCacheAndSkipsInvalidator", func(t *testing.T) {
		invalidator := &mockTrustMarkConfigInvalidator{}
		setEntityConfigurationCache(t, cacheValue)
		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			createFn: func(item smodel.AddTrustMark) (*smodel.PublishedTrustMark, error) {
				t.Fatalf("create should not be called for invalid body")
				return nil, nil
			},
		}, invalidator)

		req := httptest.NewRequest(
			http.MethodPost,
			"/entity-configuration/trust-marks/",
			strings.NewReader(`{"trust_mark_type":`),
		)
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		resp, _ := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusBadRequest)
		if invalidator.calls != 0 {
			t.Fatalf("expected invalidator to stay at 0 calls, got %d", invalidator.calls)
		}
		requireEntityConfigurationCache(t, true, cacheValue)
	})

	t.Run("ConflictKeepsCacheAndSkipsInvalidator", func(t *testing.T) {
		invalidator := &mockTrustMarkConfigInvalidator{}
		setEntityConfigurationCache(t, cacheValue)
		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			createFn: func(item smodel.AddTrustMark) (*smodel.PublishedTrustMark, error) {
				return nil, smodel.AlreadyExistsError("trust mark already exists")
			},
		}, invalidator)

		req := httptest.NewRequest(
			http.MethodPost,
			"/entity-configuration/trust-marks/",
			strings.NewReader(`{"trust_mark_type":"https://tm.example/type"}`),
		)
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		resp, _ := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusConflict)
		if invalidator.calls != 0 {
			t.Fatalf("expected invalidator to stay at 0 calls, got %d", invalidator.calls)
		}
		requireEntityConfigurationCache(t, true, cacheValue)
	})
}

func TestEntityTrustMarksGet(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			getFn: func(ident string) (*smodel.PublishedTrustMark, error) {
				if ident != "3" {
					t.Fatalf("unexpected trust mark id %q", ident)
				}
				return &smodel.PublishedTrustMark{ID: 3, TrustMarkType: "https://tm.example/type"}, nil
			},
		}, nil)

		req := httptest.NewRequest(http.MethodGet, "/entity-configuration/trust-marks/3", http.NoBody)
		resp, body := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusOK)

		var got smodel.PublishedTrustMark
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if got.ID != 3 || got.TrustMarkType != "https://tm.example/type" {
			t.Fatalf("unexpected response payload: %+v", got)
		}
	})

	t.Run("StoreError", func(t *testing.T) {
		t.Parallel()

		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			getFn: func(string) (*smodel.PublishedTrustMark, error) {
				return nil, errors.New("db down")
			},
		}, nil)

		req := httptest.NewRequest(http.MethodGet, "/entity-configuration/trust-marks/3", http.NoBody)
		resp, _ := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusInternalServerError)
	})
}

func TestEntityTrustMarksUpdate(t *testing.T) {
	cacheValue := []byte("cached-entity-config")

	t.Run("SuccessInvalidatesCacheAndConfig", func(t *testing.T) {
		invalidator := &mockTrustMarkConfigInvalidator{}
		var gotID string
		var gotInput smodel.AddTrustMark
		setEntityConfigurationCache(t, cacheValue)
		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			updateFn: func(ident string, item smodel.AddTrustMark) (*smodel.PublishedTrustMark, error) {
				gotID = ident
				gotInput = item
				return &smodel.PublishedTrustMark{ID: 7, TrustMarkType: item.TrustMarkType, TrustMarkIssuer: item.TrustMarkIssuer}, nil
			},
		}, invalidator)

		req := httptest.NewRequest(
			http.MethodPut,
			"/entity-configuration/trust-marks/7",
			strings.NewReader(`{"trust_mark_type":"https://tm.example/updated","trust_mark_issuer":"https://issuer.example"}`),
		)
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		resp, body := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusOK)

		var got smodel.PublishedTrustMark
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if gotID != "7" {
			t.Fatalf("expected trust mark id %q, got %q", "7", gotID)
		}
		if gotInput.TrustMarkType != "https://tm.example/updated" || gotInput.TrustMarkIssuer != "https://issuer.example" {
			t.Fatalf("unexpected update input: %+v", gotInput)
		}
		if got.TrustMarkType != gotInput.TrustMarkType {
			t.Fatalf("unexpected response payload: %+v", got)
		}
		if invalidator.calls != 1 {
			t.Fatalf("expected invalidator to be called once, got %d", invalidator.calls)
		}
		requireEntityConfigurationCache(t, false, nil)
	})

	t.Run("NotFoundKeepsCacheAndSkipsInvalidator", func(t *testing.T) {
		invalidator := &mockTrustMarkConfigInvalidator{}
		setEntityConfigurationCache(t, cacheValue)
		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			updateFn: func(string, smodel.AddTrustMark) (*smodel.PublishedTrustMark, error) {
				return nil, smodel.NotFoundError("missing")
			},
		}, invalidator)

		req := httptest.NewRequest(
			http.MethodPut,
			"/entity-configuration/trust-marks/missing",
			strings.NewReader(`{"trust_mark_type":"https://tm.example/updated"}`),
		)
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		resp, _ := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusNotFound)
		if invalidator.calls != 0 {
			t.Fatalf("expected invalidator to stay at 0 calls, got %d", invalidator.calls)
		}
		requireEntityConfigurationCache(t, true, cacheValue)
	})
}

func TestEntityTrustMarksPatch(t *testing.T) {
	cacheValue := []byte("cached-entity-config")

	t.Run("SuccessInvalidatesCacheAndConfig", func(t *testing.T) {
		invalidator := &mockTrustMarkConfigInvalidator{}
		var gotID string
		var gotInput smodel.UpdateTrustMark
		setEntityConfigurationCache(t, cacheValue)
		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			patchFn: func(ident string, item smodel.UpdateTrustMark) (*smodel.PublishedTrustMark, error) {
				gotID = ident
				gotInput = item
				return &smodel.PublishedTrustMark{ID: 9, TrustMarkIssuer: *item.TrustMarkIssuer, Refresh: *item.Refresh}, nil
			},
		}, invalidator)

		req := httptest.NewRequest(
			http.MethodPatch,
			"/entity-configuration/trust-marks/9",
			strings.NewReader(`{"trust_mark_issuer":"https://issuer.example","refresh":true}`),
		)
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		resp, body := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusOK)

		var got smodel.PublishedTrustMark
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if gotID != "9" {
			t.Fatalf("expected trust mark id %q, got %q", "9", gotID)
		}
		if gotInput.TrustMarkIssuer == nil || *gotInput.TrustMarkIssuer != "https://issuer.example" {
			t.Fatalf("unexpected patch issuer input: %+v", gotInput)
		}
		if gotInput.Refresh == nil || !*gotInput.Refresh {
			t.Fatalf("unexpected patch refresh input: %+v", gotInput)
		}
		if got.TrustMarkIssuer != *gotInput.TrustMarkIssuer || !got.Refresh {
			t.Fatalf("unexpected response payload: %+v", got)
		}
		if invalidator.calls != 1 {
			t.Fatalf("expected invalidator to be called once, got %d", invalidator.calls)
		}
		requireEntityConfigurationCache(t, false, nil)
	})

	t.Run("ValidationErrorKeepsCacheAndSkipsInvalidator", func(t *testing.T) {
		invalidator := &mockTrustMarkConfigInvalidator{}
		setEntityConfigurationCache(t, cacheValue)
		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			patchFn: func(string, smodel.UpdateTrustMark) (*smodel.PublishedTrustMark, error) {
				return nil, smodel.ValidationError("invalid trust mark")
			},
		}, invalidator)

		req := httptest.NewRequest(
			http.MethodPatch,
			"/entity-configuration/trust-marks/9",
			strings.NewReader(`{"refresh":true}`),
		)
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		resp, _ := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusBadRequest)
		if invalidator.calls != 0 {
			t.Fatalf("expected invalidator to stay at 0 calls, got %d", invalidator.calls)
		}
		requireEntityConfigurationCache(t, true, cacheValue)
	})
}

func TestEntityTrustMarksDelete(t *testing.T) {
	cacheValue := []byte("cached-entity-config")

	t.Run("SuccessInvalidatesCacheAndConfig", func(t *testing.T) {
		invalidator := &mockTrustMarkConfigInvalidator{}
		var gotID string
		setEntityConfigurationCache(t, cacheValue)
		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			deleteFn: func(ident string) error {
				gotID = ident
				return nil
			},
		}, invalidator)

		req := httptest.NewRequest(http.MethodDelete, "/entity-configuration/trust-marks/14", http.NoBody)
		resp, _ := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusNoContent)
		if gotID != "14" {
			t.Fatalf("expected trust mark id %q, got %q", "14", gotID)
		}
		if invalidator.calls != 1 {
			t.Fatalf("expected invalidator to be called once, got %d", invalidator.calls)
		}
		requireEntityConfigurationCache(t, false, nil)
	})

	t.Run("NotFoundKeepsCacheAndSkipsInvalidator", func(t *testing.T) {
		invalidator := &mockTrustMarkConfigInvalidator{}
		setEntityConfigurationCache(t, cacheValue)
		app := setupEntityTrustMarksTestApp(&mockPublishedTrustMarksStore{
			deleteFn: func(string) error {
				return smodel.NotFoundError("missing")
			},
		}, invalidator)

		req := httptest.NewRequest(http.MethodDelete, "/entity-configuration/trust-marks/missing", http.NoBody)
		resp, _ := doRequest(t, app, req)
		requireStatus(t, resp, http.StatusNotFound)
		if invalidator.calls != 0 {
			t.Fatalf("expected invalidator to stay at 0 calls, got %d", invalidator.calls)
		}
		requireEntityConfigurationCache(t, true, cacheValue)
	})
}