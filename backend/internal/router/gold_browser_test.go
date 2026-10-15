package router_test

import (
	"context"
	"github.com/shopspring/decimal"
	"jizhang/internal/ccb"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGoldBrowserServer(t *testing.T) {
	if os.Getenv("GOLD_BROWSER_SERVER") != "1" {
		t.Skip("explicit isolated gold fixture only")
	}
	runBrowserServer(t, false)
}

type goldBrowserMarket struct {
	mu        sync.Mutex
	now       func() time.Time
	buy, sell string
	failure   bool
	calls     atomic.Int64
}

func (m *goldBrowserMarket) Quote(context.Context) (ccb.Quote, error) {
	m.calls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failure {
		return ccb.Quote{}, ccb.ErrUnavailable
	}
	buy, sell := m.buy, m.sell
	if buy == "" {
		buy = "899.04"
	}
	if sell == "" {
		sell = "893.04"
	}
	return ccb.Quote{Buy: decimal.RequireFromString(buy), Sell: decimal.RequireFromString(sell), At: m.now()}, nil
}
