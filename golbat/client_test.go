package golbat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jfberry/golbat-filterc/filterc"
)

func TestScanPokemon(t *testing.T) {
	var gotPath, gotSecret, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotSecret = r.URL.Path, r.Header.Get("X-Golbat-Secret")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"pokemon":[{"id":"1","pokemon_id":25}],"examined":10,"skipped":1,"total":11,"limit_reached":false}`)
	}))
	defer srv.Close()

	c := &Client{URL: srv.URL + "/", Secret: "s3cret"}
	resp, err := c.ScanPokemon(context.Background(), filterc.ScanRequest{
		Min: filterc.LatLon{Lat: 1, Lon: 2}, Max: filterc.LatLon{Lat: 3, Lon: 4}, Limit: 5,
		Filters: []filterc.Clause{{Iv: &filterc.MinMax{Min: 100, Max: 100}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/pokemon/v3/scan" || gotSecret != "s3cret" {
		t.Errorf("path %q secret %q", gotPath, gotSecret)
	}
	if want := `{"min":{"lat":1,"lon":2},"max":{"lat":3,"lon":4},"limit":5,"filters":[{"iv":{"min":100,"max":100}}]}`; gotBody != want {
		t.Errorf("body %s, want %s", gotBody, want)
	}
	if len(resp.Pokemon) != 1 || resp.Examined != 10 || resp.Skipped != 1 || resp.Total != 11 || resp.LimitReached {
		t.Errorf("response %+v", resp)
	}
	var first map[string]any
	if json.Unmarshal(resp.Pokemon[0], &first) != nil || first["pokemon_id"] != float64(25) {
		t.Errorf("pokemon passthrough = %s", resp.Pokemon[0])
	}
}

func TestScanPokemonStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := (&Client{URL: srv.URL}).ScanPokemon(context.Background(), filterc.ScanRequest{Filters: []filterc.Clause{}})
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 401 || se.Body != "unauthorized\n" {
		t.Errorf("err = %v", err)
	}
}
