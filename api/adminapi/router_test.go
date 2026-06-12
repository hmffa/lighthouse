package adminapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/go-oidfed/lighthouse/storage/model"
)

func TestYamlToJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		yamlData string
		wantErr  bool
		validate func(t *testing.T, result []byte)
	}{
		{
			name: "simple object",
			yamlData: `
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
`,
			validate: func(t *testing.T, result []byte) {
				var m map[string]any
				if err := json.Unmarshal(result, &m); err != nil {
					t.Fatalf("Failed to unmarshal JSON: %v", err)
				}
				if m["openapi"] != "3.0.0" {
					t.Errorf("Expected openapi=3.0.0, got %v", m["openapi"])
				}
				info, ok := m["info"].(map[string]any)
				if !ok {
					t.Fatal("Expected info to be a map")
				}
				if info["title"] != "Test API" {
					t.Errorf("Expected title='Test API', got %v", info["title"])
				}
			},
		},
		{
			name: "servers array",
			yamlData: `
servers:
  - url: http://localhost:8080
    description: Local server
  - url: https://api.example.com
    description: Production
`,
			validate: func(t *testing.T, result []byte) {
				var m map[string]any
				if err := json.Unmarshal(result, &m); err != nil {
					t.Fatalf("Failed to unmarshal JSON: %v", err)
				}
				servers, ok := m["servers"].([]any)
				if !ok {
					t.Fatal("Expected servers to be an array")
				}
				if len(servers) != 2 {
					t.Errorf("Expected 2 servers, got %d", len(servers))
				}
				server1, ok := servers[0].(map[string]any)
				if !ok {
					t.Fatal("Expected server[0] to be a map")
				}
				if server1["url"] != "http://localhost:8080" {
					t.Errorf("Expected url='http://localhost:8080', got %v", server1["url"])
				}
			},
		},
		{
			name:     "invalid YAML",
			yamlData: "{{invalid yaml",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := yamlToJSON([]byte(tt.yamlData))
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestConvertMapKeysToStrings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  any
		verify func(t *testing.T, result any)
	}{
		{
			name: "map[string]any passthrough",
			input: map[string]any{
				"key": "value",
			},
			verify: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				if !ok {
					t.Fatal("Expected map[string]any")
				}
				if m["key"] != "value" {
					t.Errorf("Expected key=value, got %v", m["key"])
				}
			},
		},
		{
			name: "nested maps",
			input: map[string]any{
				"outer": map[string]any{
					"inner": "value",
				},
			},
			verify: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				if !ok {
					t.Fatal("Expected map[string]any")
				}
				outer, ok := m["outer"].(map[string]any)
				if !ok {
					t.Fatal("Expected outer to be map[string]any")
				}
				if outer["inner"] != "value" {
					t.Errorf("Expected inner=value, got %v", outer["inner"])
				}
			},
		},
		{
			name: "array with maps",
			input: []any{
				map[string]any{"a": 1},
				map[string]any{"b": 2},
			},
			verify: func(t *testing.T, result any) {
				arr, ok := result.([]any)
				if !ok {
					t.Fatal("Expected []any")
				}
				if len(arr) != 2 {
					t.Errorf("Expected 2 elements, got %d", len(arr))
				}
			},
		},
		{
			name:  "primitive passthrough",
			input: "hello",
			verify: func(t *testing.T, result any) {
				if result != "hello" {
					t.Errorf("Expected 'hello', got %v", result)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertMapKeysToStrings(tt.input)
			tt.verify(t, result)
		})
	}
}

// --- Wiring tests: the production Register() assembly ---

// setupRegisteredApp mounts the full admin API via Register, exactly as
// production does (auth + actor middleware, route precedence, users toggle),
// backed by a real per-test SQLite store.
func setupRegisteredApp(t *testing.T, opts *Options) (*fiber.App, model.Backends) {
	t.Helper()
	store := newTestStorage(t)
	backends := store.Backends()
	km := KeyManagement{APIManagedPKs: store.DBPublicKeyStorage("api-managed")}
	if err := km.APIManagedPKs.Load(); err != nil {
		t.Fatalf("Failed to init public key storage: %v", err)
	}
	app := fiber.New()
	group := app.Group("/api/v1/admin")
	if err := Register(group, "https://lighthouse.example.org", backends, newStubFedEntity(), km, opts); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	return app, backends
}

// mustCreateUser seeds an admin user through the real users storage so the
// auth middleware enforces authentication.
func mustCreateUser(t *testing.T, backends model.Backends, username, password string) {
	t.Helper()
	if _, err := backends.Users.Create(username, password, "Wiring Test"); err != nil {
		t.Fatalf("Failed to create user %q: %v", username, err)
	}
}

func TestRegisterWiring(t *testing.T) {
	t.Parallel()

	t.Run("DocsArePublicWithoutAuth", func(t *testing.T) {
		t.Parallel()
		app, backends := setupRegisteredApp(t, nil)
		mustCreateUser(t, backends, "admin", "correct-horse")

		for _, path := range []string{
			"/api/v1/admin/docs",
			"/api/v1/admin/openapi.yaml",
			"/api/v1/admin/openapi.json",
			"/api/v1/admin/docs/users",
		} {
			resp, body := doRequest(t, app, httptest.NewRequest("GET", path, http.NoBody))
			requireStatusMsg(t, resp, body, http.StatusOK, path)
		}
	})

	t.Run("ResourceRoutesRequireAuth", func(t *testing.T) {
		t.Parallel()
		app, backends := setupRegisteredApp(t, nil)
		mustCreateUser(t, backends, "admin", "correct-horse")

		resp, body := doRequest(t, app, httptest.NewRequest("GET", "/api/v1/admin/subordinates", http.NoBody))
		assertErrorResponse(t, resp, body, http.StatusUnauthorized, "invalid_client")
		if got := resp.Header.Get("WWW-Authenticate"); got != "Basic realm=admin" {
			t.Errorf("Expected WWW-Authenticate challenge, got %q", got)
		}

		req := httptest.NewRequest("GET", "/api/v1/admin/subordinates", http.NoBody)
		req.Header.Set("Authorization", basicAuthHeader("admin", "correct-horse"))
		resp, body = doRequest(t, app, req)
		requireStatus(t, resp, body, http.StatusOK)
	})

	t.Run("NoUsersMeansOpenAccess", func(t *testing.T) {
		t.Parallel()
		app, _ := setupRegisteredApp(t, nil)

		resp, body := doRequest(t, app, httptest.NewRequest("GET", "/api/v1/admin/subordinates", http.NoBody))
		requireStatus(t, resp, body, http.StatusOK)
	})

	t.Run("UsersRoutesToggle", func(t *testing.T) {
		t.Parallel()
		appNil, _ := setupRegisteredApp(t, nil)
		resp, body := doRequest(t, appNil, httptest.NewRequest("GET", "/api/v1/admin/users/", http.NoBody))
		requireStatusMsg(t, resp, body, http.StatusOK, "nil opts should mount users routes")

		// PIN of a footgun: nil opts and a zero-value &Options{} diverge —
		// UsersEnabled defaults to false, so callers passing &Options{Port: x}
		// silently lose user management. Documented here until clarified.
		appZero, _ := setupRegisteredApp(t, &Options{})
		resp, body = doRequest(t, appZero, httptest.NewRequest("GET", "/api/v1/admin/users/", http.NoBody))
		requireStatusMsg(t, resp, body, http.StatusNotFound, "zero-value opts should not mount users routes")
	})
}

func TestRegisterSubordinateRoutePrecedence(t *testing.T) {
	t.Parallel()
	app, _ := setupRegisteredApp(t, nil)

	// /subordinates/lifetime must reach the general lifetime handler. If the
	// :subordinateID routes were registered first, "lifetime" would be parsed
	// as a subordinate ID and this would 400/404 instead of the default value.
	resp, body := doRequest(t, app, httptest.NewRequest("GET", "/api/v1/admin/subordinates/lifetime", http.NoBody))
	requireStatus(t, resp, body, http.StatusOK)
	if _, err := strconv.Atoi(strings.TrimSpace(string(body))); err != nil {
		t.Errorf("Expected integer lifetime body, got %q", string(body))
	}

	// /subordinates/constraints must reach the general constraints handler
	// (404 on an empty store) rather than the :subordinateID parser (400).
	resp, body = doRequest(t, app, httptest.NewRequest("GET", "/api/v1/admin/subordinates/constraints", http.NoBody))
	assertStatus(t, resp, body, http.StatusNotFound)
}

func TestAuthActorChainRecordsEventActor(t *testing.T) {
	t.Parallel()
	app, backends := setupRegisteredApp(t, nil)
	mustCreateUser(t, backends, "eventadmin", "correct-horse")

	body := `{"entity_id":"https://actor-chain.example.org","registered_entity_types":["openid_provider"],"status":"pending"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/subordinates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", basicAuthHeader("eventadmin", "correct-horse"))
	resp, respBody := doRequest(t, app, req)
	requireStatus(t, resp, respBody, http.StatusCreated)

	saved, err := backends.Subordinates.Get("https://actor-chain.example.org")
	if err != nil || saved == nil {
		t.Fatalf("Failed to fetch created subordinate: %v", err)
	}
	events, _, err := backends.SubordinateEvents.GetBySubordinateID(saved.ID, model.EventQueryOpts{})
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}
	for _, e := range events {
		if e.Type == model.EventTypeCreated {
			// The full production chain must attribute the event to the
			// authenticated user: authMiddleware stores the username via
			// SetAuthUsername and actorMiddleware exposes it via GetActor.
			if e.Actor == nil || *e.Actor != "eventadmin" {
				got := "<nil>"
				if e.Actor != nil {
					got = *e.Actor
				}
				t.Errorf("Expected created event actor %q, got %q", "eventadmin", got)
			}
			return
		}
	}
	t.Errorf("No %q event recorded for created subordinate", model.EventTypeCreated)
}
