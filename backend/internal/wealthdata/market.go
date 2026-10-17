// Package wealthdata defines controlled bank-wealth identities and public NAVs.
package wealthdata

import (
	"context"
	"errors"
	"time"
	_ "time/tzdata"

	"github.com/shopspring/decimal"
)

var (
	ErrUnavailable  = errors.New("银行理财净值暂不可用")
	ErrUnconfigured = errors.New("尚未配置银行理财采集服务")
	ErrRateLimited  = errors.New("净值查询冷却中，请稍后重试")
	Shanghai        = func() *time.Location {
		location, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			return time.FixedZone("Asia/Shanghai", 8*3600)
		}
		return location
	}()
)

type NAV struct {
	Date       time.Time
	Value      decimal.Decimal
	TotalValue *decimal.Decimal
	FetchedAt  time.Time
	SourceCode string
}

type MarketData interface {
	Configured() bool
	NAVs(context.Context, string, string) ([]NAV, error)
}

// Source is the bank seam: product registration is independent of fetching.
type Source interface {
	Configured() bool
	Fetch(context.Context, Product, string) ([]NAV, error)
}

type Router struct {
	catalog *Registry
	sources map[string]Source
}

func NewRouter(sources map[string]Source) *Router {
	return NewRouterWithCatalog(defaultCatalog, sources)
}

func NewRouterWithCatalog(catalog *Registry, sources map[string]Source) *Router {
	router := &Router{catalog: catalog, sources: make(map[string]Source, len(sources))}
	for name, source := range sources {
		router.sources[name] = source
	}
	return router
}

func (r *Router) Configured() bool {
	for _, source := range r.sources {
		if source != nil && source.Configured() {
			return true
		}
	}
	return false
}

func (r *Router) ConfiguredFor(code string) bool {
	product, ok := r.catalog.Lookup(code)
	source := r.sources[product.Adapter]
	return ok && source != nil && source.Configured()
}

func (r *Router) NAVs(ctx context.Context, code, window string) ([]NAV, error) {
	product, ok := r.catalog.Lookup(code)
	if !ok {
		return nil, ErrUnavailable
	}
	if !r.ConfiguredFor(code) {
		return nil, ErrUnconfigured
	}
	return r.sources[product.Adapter].Fetch(ctx, product, window)
}
