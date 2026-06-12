package adminapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"gopkg.in/yaml.v3"

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

// --- OpenAPI spec ↔ route table conformance ---

// pathParamRe matches both spec-style {param} and fiber-style :param segments.
var pathParamRe = regexp.MustCompile(`\{[^}]*\}|:[^/]+`)

// normalizePathParams maps both parameter syntaxes onto "{}" so spec paths and
// fiber routes compare structurally, independent of parameter names, and
// strips a trailing slash (fiber group roots register as ".../").
func normalizePathParams(p string) string {
	p = pathParamRe.ReplaceAllString(p, "{}")
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	return p
}

// specDocumentedPaths parses the embedded OpenAPI documents and returns the
// normalized documented path set plus any path items that declare no
// operations at all (stale spec entries).
func specDocumentedPaths(t *testing.T) (documented map[string]bool, emptyPathItems []string) {
	t.Helper()
	documented = map[string]bool{}
	for _, name := range []string{"openapi.yaml", "openapi-users.yaml"} {
		raw, err := assets.ReadFile(name)
		if err != nil {
			t.Fatalf("Failed to read embedded %s: %v", name, err)
		}
		var doc struct {
			Paths map[string]map[string]any `yaml:"paths"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("Failed to parse %s: %v", name, err)
		}
		for p, item := range doc.Paths {
			ops := 0
			for k := range item {
				switch strings.ToLower(k) {
				case "get", "post", "put", "patch", "delete", "head", "options":
					ops++
				}
			}
			if ops == 0 {
				emptyPathItems = append(emptyPathItems, p+" ("+name+")")
				continue
			}
			documented[normalizePathParams(p)] = true
		}
	}
	return documented, emptyPathItems
}

// TestRegisteredRoutesMatchOpenAPISpec is a conformance RATCHET between the
// fiber route table (via the real Register()) and the embedded OpenAPI specs:
// any NEW undocumented route, documented-but-unregistered path, or empty spec
// path item fails the test. Today's known documentation debt is allowlisted
// below (see OPENAPI_FINDINGS.md); when the spec is fixed, the corresponding
// entry becomes stale and the test fails until it is removed — the lists can
// only shrink.
func TestRegisteredRoutesMatchOpenAPISpec(t *testing.T) {
	t.Parallel()

	// Spec/docs-serving meta endpoints: intentionally not part of the API spec.
	metaRoutes := map[string]bool{
		"/api/v1/admin/docs":               true,
		"/api/v1/admin/docs/users":         true,
		"/api/v1/admin/openapi.yaml":       true,
		"/api/v1/admin/openapi.json":       true,
		"/api/v1/admin/openapi-users.yaml": true,
		"/api/v1/admin/openapi-users.json": true,
	}

	// Known documentation debt — routes that exist but are absent from the
	// spec (OPENAPI_FINDINGS.md, structural gaps): the entire stats API is
	// undocumented. Remove entries as the spec gains them.
	knownUndocumentedRoutes := map[string]bool{
		"/api/v1/admin/stats/summary":         true,
		"/api/v1/admin/stats/daily":           true,
		"/api/v1/admin/stats/timeseries":      true,
		"/api/v1/admin/stats/latency":         true,
		"/api/v1/admin/stats/export":          true,
		"/api/v1/admin/stats/top/clients":     true,
		"/api/v1/admin/stats/top/countries":   true,
		"/api/v1/admin/stats/top/endpoints":   true,
		"/api/v1/admin/stats/top/params":      true,
		"/api/v1/admin/stats/top/user-agents": true,
	}

	// Known stale spec entries — documented paths with no registered route.
	knownSpecPathsWithoutRoute := map[string]bool{}

	// Known spec path items declaring zero operations (OPENAPI_FINDINGS.md:
	// leftover of the plural→singular metadata-policy-crit rename).
	knownEmptySpecPathItems := map[string]bool{
		"/api/v1/admin/subordinates/metadata-policies-crit (openapi.yaml)": true,
	}

	app, _ := setupRegisteredApp(t, nil)
	registered := map[string]bool{}
	for _, r := range app.GetRoutes(true) {
		switch r.Method {
		case http.MethodHead, http.MethodConnect, http.MethodTrace, http.MethodOptions:
			continue
		}
		registered[normalizePathParams(r.Path)] = true
	}
	documented, emptyItems := specDocumentedPaths(t)

	for p := range registered {
		if metaRoutes[p] || knownUndocumentedRoutes[p] {
			continue
		}
		if !documented[p] {
			t.Errorf("Registered route %s is not documented in the OpenAPI spec", p)
		}
	}
	for p := range documented {
		if knownSpecPathsWithoutRoute[p] {
			continue
		}
		if !registered[p] {
			t.Errorf("Spec documents %s but no route is registered", p)
		}
	}
	for _, p := range emptyItems {
		if !knownEmptySpecPathItems[p] {
			t.Errorf("Spec path item %s declares no operations (stale entry)", p)
		}
	}

	// Ratchet maintenance: every allowlist entry must still describe a real
	// discrepancy; otherwise it must be removed.
	for p := range knownUndocumentedRoutes {
		if !registered[p] {
			t.Errorf("Allowlist entry %s is no longer a registered route — remove it", p)
		} else if documented[p] {
			t.Errorf("Allowlist entry %s is now documented — remove it from knownUndocumentedRoutes", p)
		}
	}
	for p := range knownSpecPathsWithoutRoute {
		if !documented[p] {
			t.Errorf("Allowlist entry %s is no longer in the spec — remove it", p)
		} else if registered[p] {
			t.Errorf("Allowlist entry %s now has a route — remove it from knownSpecPathsWithoutRoute", p)
		}
	}
	for p := range knownEmptySpecPathItems {
		found := false
		for _, e := range emptyItems {
			if e == p {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Allowlist entry %s is no longer an empty spec path item — remove it", p)
		}
	}
}
