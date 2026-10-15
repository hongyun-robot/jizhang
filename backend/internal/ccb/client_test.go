package ccb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sample = `{"SUCCESS":"true","retCode":"000000","PM_Txn_Vrty_Cd":"33","CcyCd":"156","Cst_Buy_Prc":"899.04","Cst_Sell_Prc":"893.04","Tms":"2026-10-15 19:30:57.911"}`

func TestAnonymousSessionAndRecovery(t *testing.T) {
	initCalls, quotes := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.UserAgent(), "JizhangGold") {
			t.Error("public portal user agent missing")
		}
		if r.URL.Query().Get("TXCODE") == "100119" {
			initCalls++
			http.SetCookie(w, &http.Cookie{Name: "anon", Value: "public", Path: "/"})
			return
		}
		quotes++
		if _, e := r.Cookie("anon"); e != nil {
			t.Error("cookie missing")
		}
		if quotes == 1 {
			fmt.Fprint(w, "<html>请重新登录</html>")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, sample)
	}))
	defer srv.Close()
	now := time.Date(2026, 10, 15, 20, 0, 0, 0, Shanghai)
	q, e := NewAt(srv.URL, nil, func() time.Time { return now }).Quote(context.Background())
	if e != nil || q.Sell.String() != "893.04" || q.Buy.String() != "899.04" || initCalls != 2 || quotes != 2 {
		t.Fatal(q, e, initCalls, quotes)
	}
}
func TestResponseBoundsAndRedirects(t *testing.T) {
	t.Run("size", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("TXCODE") == "NGJS01" {
				fmt.Fprint(w, strings.Repeat("x", (4<<20)+1))
			}
		}))
		defer srv.Close()
		if _, e := NewAt(srv.URL, nil, time.Now).Quote(context.Background()); e == nil {
			t.Fatal("oversized body accepted")
		}
	})
	t.Run("redirect", func(t *testing.T) {
		targetCalls := 0
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
		defer target.Close()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
		defer srv.Close()
		if _, e := NewAt(srv.URL, nil, time.Now).Quote(context.Background()); e == nil || targetCalls != 0 {
			t.Fatal("redirect left origin", e, targetCalls)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(100 * time.Millisecond):
			}
		}))
		defer srv.Close()
		c := NewAt(srv.URL, nil, time.Now)
		c.http.Timeout = 5 * time.Millisecond
		if _, e := c.Quote(context.Background()); e == nil {
			t.Fatal("timeout ignored")
		}
	})
}
func TestBadQuotesAreRejected(t *testing.T) {
	now := time.Date(2026, 10, 15, 20, 0, 0, 0, Shanghai)
	for _, pair := range [][2]string{{`"33"`, `"23"`}, {`"156"`, `"840"`}, {`"true"`, `"false"`}, {`"893.04"`, `"0"`}, {`"893.04"`, `"NaN"`}, {`"899.04"`, `"-1"`}, {`2026-10-15`, `2026-10-16`}} {
		if _, e := Parse([]byte(strings.Replace(sample, pair[0], pair[1], 1)), now); e == nil {
			t.Fatal(pair)
		}
	}
	if _, e := Parse([]byte("<html>bad gateway</html>"), now); e == nil {
		t.Fatal("HTML accepted")
	}
}
func TestSessionRetriesBounded(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n++; fmt.Fprint(w, "重新登录") }))
	defer srv.Close()
	if _, e := NewAt(srv.URL, nil, time.Now).Quote(context.Background()); e == nil || n != 4 {
		t.Fatal(e, n)
	}
}
func TestHTTPFailureAndCancellation(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n++; w.WriteHeader(429) }))
	defer srv.Close()
	if _, e := NewAt(srv.URL, nil, time.Now).Quote(context.Background()); e == nil || n != 1 {
		t.Fatal(e, n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := NewAt(srv.URL, nil, time.Now).Quote(ctx); e == nil {
		t.Fatal("cancel ignored")
	}
}
