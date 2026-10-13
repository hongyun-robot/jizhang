package router_test

import (
	"context"
	"github.com/shopspring/decimal"
	"jizhang/internal/fuyao"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFundBrowserServer(t *testing.T) {
	if os.Getenv("FUND_BROWSER_SERVER") != "1" {
		t.Skip("explicit isolated fund fixture only")
	}
	runBrowserServer(t, false)
}

type fundBrowserMarket struct {
	mu      sync.Mutex
	quotes  []fuyao.NAV
	failure error
}

func newFundBrowserMarket() *fundBrowserMarket {
	return &fundBrowserMarket{quotes: []fuyao.NAV{{Date: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), Value: decimal.RequireFromString("1.2305")}, {Date: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), Value: decimal.RequireFromString("1.1946")}}}
}
func (f *fundBrowserMarket) Configured() bool { return true }
func (f *fundBrowserMarket) Search(_ context.Context, q string) ([]fuyao.Instrument, error) {
	code := strings.TrimSuffix(q, ".OF") + ".OF"
	return []fuyao.Instrument{{THSCode: code, Ticker: strings.TrimSuffix(code, ".OF"), Name: "测试基金", AssetType: "fund-otc", Currency: "CNY"}}, nil
}
func (f *fundBrowserMarket) Profile(_ context.Context, code string) (fuyao.Profile, error) {
	return fuyao.Profile{THSCode: code, Ticker: strings.TrimSuffix(code, ".OF"), Name: "测试基金"}, nil
}
func (f *fundBrowserMarket) NAVs(context.Context, string, string) ([]fuyao.NAV, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fuyao.NAV(nil), f.quotes...), f.failure
}
func (f *fundBrowserMarket) Dividends(context.Context, string) ([]fuyao.Dividend, error) {
	return []fuyao.Dividend{}, nil
}
