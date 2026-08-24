package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"webhook-event-router/internal/config"
	"webhook-event-router/internal/dispatcher"
	"webhook-event-router/internal/metrics"
	"webhook-event-router/internal/store/sqlite"
)

func setup(t *testing.T, allowLoopback bool) (*httptest.Server, *sqlite.Store) {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	st := sqlite.New(db)
	att := sqlite.NewAttemptStore(db)
	cfg := &config.Config{
		Addr:           ":0",
		MaxPayload:     1 << 20,
		MaxRetries:     5,
		RetryBase:      10 * time.Millisecond,
		ForwardTimeout: 5 * time.Second,
		AllowLoopback:  allowLoopback,
		AllowPrivate:   true,
		ReplayWindow:   300 * time.Second,
	}
	disp := dispatcher.New(st, att, cfg)
	reg := metrics.NewRegistry()
	handler := NewServer(st, cfg, disp, reg, att)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, st
}

func doJSON(t *testing.T, client *http.Client, method, url string, body any, headers map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return resp, m
}
