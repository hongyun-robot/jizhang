package suyin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Explicit replay of a public collector response, never a personal bank session.
func TestCapturedPublicEnvelope(t *testing.T) {
	path := os.Getenv("SUYIN_PUBLIC_ENVELOPE")
	if path == "" {
		t.Skip("set SUYIN_PUBLIC_ENVELOPE to a captured public-only collector response")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		FetchedAt time.Time `json:"fetched_at"`
	}
	if err = json.Unmarshal(body, &envelope); err != nil || envelope.FetchedAt.IsZero() {
		t.Fatal("invalid capture timestamp")
	}
	product, _ := Catalog("J07241")
	rows, err := Parse(body, product, envelope.FetchedAt)
	if err != nil || len(rows) == 0 {
		t.Fatal("captured public response incompatible with adapter", err)
	}
	t.Logf("validated %d public NAV rows; latest date %s", len(rows), rows[len(rows)-1].Date.Format("2006-01-02"))
}

func sample(now time.Time) map[string]any {
	p, _ := Catalog("J07241")
	return map[string]any{"code": p.Code, "name": p.Name, "registration_code": p.RegistrationCode, "currency": p.Currency, "request_fund_code": p.Code, "source_code": p.SourceCode, "source_url": p.PageURL, "fetched_at": now.Format(time.RFC3339), "response": map[string]any{"code": "0000", "data": []any{
		map[string]any{"cfm_date": "2026-10-14", "nav": "1.02136", "tot_nav": "1.02136", "prd_code": p.SourceCode},
		map[string]any{"cfm_date": "2025-09-02", "nav": "1.0007", "tot_nav": "1.0007", "prd_code": p.SourceCode},
	}}}
}

func TestParseIdentityDatesAndPrices(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, Shanghai)
	p, _ := Catalog("J07241")
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"share", func(s map[string]any) { s["request_fund_code"] = "J07242" }},
		{"registration", func(s map[string]any) { s["registration_code"] = "other" }},
		{"currency", func(s map[string]any) { s["currency"] = "USD" }},
		{"source", func(s map[string]any) { s["source_url"] = "https://example.com" }},
		{"business", func(s map[string]any) { s["response"].(map[string]any)["code"] = "1001" }},
		{"empty", func(s map[string]any) { s["response"].(map[string]any)["data"] = []any{} }},
		{"future", func(s map[string]any) {
			s["response"].(map[string]any)["data"].([]any)[0].(map[string]any)["cfm_date"] = "2026-10-16"
		}},
		{"invalid-date", func(s map[string]any) {
			s["response"].(map[string]any)["data"].([]any)[0].(map[string]any)["cfm_date"] = "2026-02-30"
		}},
		{"source-code", func(s map[string]any) {
			s["response"].(map[string]any)["data"].([]any)[0].(map[string]any)["prd_code"] = "J07242"
		}},
		{"negative", func(s map[string]any) {
			s["response"].(map[string]any)["data"].([]any)[0].(map[string]any)["nav"] = "-1"
		}},
		{"nan", func(s map[string]any) {
			s["response"].(map[string]any)["data"].([]any)[0].(map[string]any)["nav"] = "NaN"
		}},
		{"zero", func(s map[string]any) {
			s["response"].(map[string]any)["data"].([]any)[0].(map[string]any)["nav"] = "0"
		}},
		{"precision", func(s map[string]any) {
			s["response"].(map[string]any)["data"].([]any)[0].(map[string]any)["nav"] = "1.000000001"
		}},
		{"stale-envelope", func(s map[string]any) { s["fetched_at"] = now.Add(-time.Hour).Format(time.RFC3339) }},
		{"duplicate-conflict", func(s map[string]any) {
			r := s["response"].(map[string]any)
			r["data"] = append(r["data"].([]any), map[string]any{"cfm_date": "2026-10-14", "nav": "2", "tot_nav": "2", "prd_code": p.SourceCode})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sample(now)
			tc.edit(s)
			body, _ := json.Marshal(s)
			if _, err := Parse(body, p, now); !errors.Is(err, ErrUnavailable) {
				t.Fatal("invalid batch accepted", err)
			}
		})
	}
	body, _ := json.Marshal(sample(now))
	rows, err := Parse(body, p, now)
	if err != nil || len(rows) != 2 || rows[0].Date.Format("2006-01-02") != "2025-09-02" || rows[1].Value.String() != "1.02136" {
		t.Fatal(rows, err)
	}
}

func TestHTTPBoundaryAndSecretProtection(t *testing.T) {
	now := time.Now()
	key := "test-only-collector-key-123456"
	for _, status := range []int{200, 412, 429, 502} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.URL.Path != "/v1/products/J07241/nav" || r.Header.Get("Authorization") != "Bearer "+key {
				t.Error("unexpected collector request")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(sample(now))
		}))
		client := New(server.URL, key)
		_, err := client.NAVs(context.Background(), "J07241", "all")
		server.Close()
		if status == 200 && err != nil {
			t.Fatal(err)
		}
		if status == 429 && !errors.Is(err, ErrRateLimited) {
			t.Fatal(err)
		}
		if status != 200 && (err == nil || strings.Contains(err.Error(), key)) {
			t.Fatal("invalid or leaking error", err)
		}
	}
	if _, err := New("", "").NAVs(context.Background(), "J07241", "all"); !errors.Is(err, ErrUnconfigured) {
		t.Fatal(err)
	}
	if _, err := New("", "").NAVs(context.Background(), "J07242", "all"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestRejectsHTMLAndRedirectsWithoutForwardingSecret(t *testing.T) {
	var forwarded bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = true
		w.WriteHeader(200)
	}))
	defer target.Close()
	for _, redirect := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if redirect {
				http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>anonymous challenge</html>"))
		}))
		_, err := New(server.URL, "test-only-collector-key-123456").NAVs(context.Background(), "J07241", "all")
		server.Close()
		if !errors.Is(err, ErrUnavailable) || forwarded {
			t.Fatal("HTML/redirect accepted or secret forwarded", err)
		}
	}
}
