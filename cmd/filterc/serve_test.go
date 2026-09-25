package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jfberry/golbat-filterc/golbat"
)

func testSettings() settings {
	s := settings{Config: defaultConfig()}
	s.Bounds.Min, s.Bounds.Max, s.Bounds.Limit = latLon{51.4, -0.2}, latLon{51.6, 0.1}, 300
	return s
}

func post(t *testing.T, h http.Handler, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: bad JSON %q", path, rec.Body.String())
		}
	}
	return rec.Code, out
}

func TestCompileEndpoint(t *testing.T) {
	h := newHandler(testSettings(), nil)
	code, out := post(t, h, "/compile", `{"expression": "iv == 100"}`)
	if code != 200 {
		t.Fatalf("status %d: %v", code, out)
	}
	req := out["request"].(map[string]any)
	if req["limit"] != float64(300) || req["min"].(map[string]any)["lat"] != 51.4 {
		t.Errorf("config bounds not applied: %v", req)
	}
	if w, ok := out["warnings"].([]any); !ok || len(w) != 0 {
		t.Errorf("warnings = %v", out["warnings"])
	}

	code, out = post(t, h, "/compile", `{"expression": "iv > 100", "bounds": {"min": {"lat": 1, "lon": 2}, "max": {"lat": 3, "lon": 4}}, "limit": 7}`)
	req = out["request"].(map[string]any)
	if code != 200 || req["limit"] != float64(7) || req["max"].(map[string]any)["lon"] != float64(4) || len(req["filters"].([]any)) != 0 {
		t.Errorf("override: %d %v", code, out)
	}
	if w := out["warnings"].([]any); len(w) != 1 {
		t.Errorf("warnings = %v", w)
	}
}

func TestCompileEndpointErrors(t *testing.T) {
	h := newHandler(testSettings(), nil)
	code, out := post(t, h, "/compile", `{"expression": "iv >= 90 && x == 1"}`)
	e := out["error"].(map[string]any)
	pos := e["position"].(map[string]any)
	if code != 400 || e["message"] != `unknown field "x"` || pos["line"] != float64(1) || pos["column"] != float64(13) {
		t.Errorf("%d %v", code, out)
	}
	if code, out := post(t, h, "/compile", `{"expression": `); code != 400 || out["error"] == nil {
		t.Errorf("bad JSON: %d %v", code, out)
	}
	if code, _ := post(t, h, "/compile", `{"expression": "`+strings.Repeat("x", 70<<10)+`"}`); code != 413 {
		t.Errorf("oversize body: %d", code)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/compile", nil))
	if rec.Code != 405 {
		t.Errorf("GET /compile = %d", rec.Code)
	}
}

func TestScanEndpoint(t *testing.T) {
	var gotBody string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, `{"pokemon":[{"id":"9"}],"examined":3,"skipped":0,"total":3,"limit_reached":false}`)
	}))
	defer fake.Close()

	h := newHandler(testSettings(), &golbat.Client{URL: fake.URL})
	code, out := post(t, h, "/scan", `{"expression": "size == 5 && pokemon != 710"}`)
	if code != 200 {
		t.Fatalf("status %d: %v", code, out)
	}
	if !strings.Contains(gotBody, `"filters":[{"size":{"min":5,"max":5}},{"pokemon":[{"id":710}],"iv":{"min":1,"max":0}}]`) {
		t.Errorf("golbat received %s", gotBody)
	}
	resp := out["response"].(map[string]any)
	if resp["examined"] != float64(3) || len(resp["pokemon"].([]any)) != 1 || out["request"] == nil {
		t.Errorf("response %v", out)
	}

	fake.Close()
	if code, out := post(t, h, "/scan", `{"expression": "iv == 100"}`); code != 502 || out["error"] == nil {
		t.Errorf("golbat down: %d %v", code, out)
	}
	if code, out := post(t, newHandler(testSettings(), nil), "/scan", `{"expression": "iv == 100"}`); code != 503 || out["error"] == nil {
		t.Errorf("no golbat configured: %d %v", code, out)
	}
}

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(testSettings(), nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != 200 {
		t.Errorf("healthz = %d", rec.Code)
	}
}
