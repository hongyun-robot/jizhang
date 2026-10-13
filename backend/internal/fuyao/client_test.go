package fuyao

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicFundDataContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-api-key") != "private-key" {
			t.Error("missing credential header")
		}
		if r.URL.Query().Has("shares") || r.URL.Query().Has("cost") {
			t.Error("private holdings sent")
		}
		switch r.URL.Path {
		case "/api/meta/tickers/search":
			if r.URL.Query().Get("q") != "023564" {
				t.Error("leading zero lost")
			}
			w.Write([]byte(`{"code":0,"data":{"item":[{"thscode":"023564.OF","ticker":"023564","name":"上证A","asset_type":"fund-otc","currency":"CNY"}]}}`))
		case "/api/fund/profile/detail":
			w.Write([]byte(`{"code":0,"data":{"item":[{"thscode":"023564.OF","fund_name":"上证A","manager_name":"李博扬"}]}}`))
		case "/api/fund/performance/nav":
			if r.URL.Query().Get("range") != "month" || r.URL.Query().Get("nav_type") != "unit" {
				t.Error("NAV query contract")
			}
			w.Write([]byte(`{"code":0,"data":{"item":[{"nav_date":1791734400000,"unit_nav":1.1946},{"nav_date":1791475200000,"unit_nav":1.2305},{"nav_date":1791734400000,"unit_nav":1.1946}]}}`))
		case "/api/fund/corporate-actions/dividends":
			w.Write([]byte(`{"code":0,"data":{"item":[{"registration_date_ms":null,"ex_dividend_date_ms":null,"per_ten_cash_after_tax":null}]}}`))
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer server.Close()
	c := NewAt(server.URL, "private-key", nil)
	ctx := context.Background()
	items, e := c.Search(ctx, "023564")
	if e != nil || len(items) != 1 || items[0].THSCode != "023564.OF" {
		t.Fatal(items, e)
	}
	p, e := c.Profile(ctx, items[0].THSCode)
	if e != nil || p.Manager != "李博扬" {
		t.Fatal(p, e)
	}
	n, e := c.NAVs(ctx, p.THSCode, "month")
	if e != nil || len(n) != 2 || n[1].Value.String() != "1.1946" || n[1].Date.Format("2006-01-02") != "2026-10-12" {
		t.Fatal(n, e)
	}
	d, e := c.Dividends(ctx, p.THSCode)
	if e != nil || len(d) != 0 {
		t.Fatal(d, e)
	}
}

func TestFailuresNeverEchoCredential(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		rate   bool
	}{
		{"business", 200, `{"code":2003,"message":"private-key","data":null}`, false},
		{"rate", 429, "private-key", true},
		{"business-rate", 200, `{"code":4001,"message":"private-key"}`, true},
		{"bad-price", 200, `{"code":0,"data":{"item":[{"nav_date":1791734400000,"unit_nav":0}]}}`, false},
		{"bad-date", 200, `{"code":0,"data":{"item":[{"nav_date":9999999999999,"unit_nav":1}]}}`, false},
		{"bad-json", 200, "private-key", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer s.Close()
			_, e := NewAt(s.URL, "private-key", nil).NAVs(context.Background(), "023564.OF", "month")
			if e == nil || strings.Contains(e.Error(), "private-key") {
				t.Fatal(e)
			}
			if tc.rate && !errors.Is(e, ErrRateLimited) {
				t.Fatal(e)
			}
		})
	}
	if _, e := New("").Search(context.Background(), "023564"); !errors.Is(e, ErrUnconfigured) {
		t.Fatal(e)
	}
}

func TestRedirectDoesNotForwardKey(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("foreign origin reached") }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer source.Close()
	_, e := NewAt(source.URL, "private-key", nil).Search(context.Background(), "023564")
	if e == nil {
		t.Fatal("redirect accepted")
	}
}

func TestCanceledAndOversizedResponses(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.Repeat("x", (4<<20)+1))) }))
	defer s.Close()
	c := NewAt(s.URL, "private-key", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.Search(ctx, "023564"); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e := c.Search(context.Background(), "023564"); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}
