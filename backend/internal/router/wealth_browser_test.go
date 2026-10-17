package router_test

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/service"
	"jizhang/internal/suyin"
	"jizhang/internal/wealthdata"
)

func TestWealthBrowserServer(t *testing.T) {
	if os.Getenv("WEALTH_BROWSER_SERVER") != "1" {
		t.Skip("explicit isolated wealth browser fixture only")
	}
	runBrowserServer(t, false)
}

type wealthBrowserMarket struct {
	mu       sync.Mutex
	now      func() time.Time
	rows     []suyin.NAV
	products map[string][]suyin.NAV
	failures map[string]bool
	failure  bool
	calls    atomic.Int64
}

func (m *wealthBrowserMarket) Configured() bool { return true }
func (m *wealthBrowserMarket) NAVs(ctx context.Context, code, window string) ([]suyin.NAV, error) {
	m.calls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if (code == "J07241" && m.failure) || m.failures[code] {
		return nil, suyin.ErrUnavailable
	}
	rows := append([]suyin.NAV(nil), m.rows...)
	if code != "J07241" {
		rows = append([]suyin.NAV(nil), m.products[code]...)
	}
	for i := range rows {
		rows[i].FetchedAt = m.now()
	}
	return rows, nil
}
func wealthBrowserRoutes(r *gin.Engine, db *gorm.DB, market *wealthBrowserMarket, clock *atomic.Int64, svc *service.WealthService) {
	if os.Getenv("CMBCWM_BROWSER_FIXTURE") == "1" {
		identity, _ := wealthdata.Catalog("FBAE68020E")
		date, _ := time.Parse("2006-01-02", identity.InceptionDate)
		if err := db.Create(&model.WealthProduct{ID: 2, Issuer: identity.Issuer, RegistrationCode: identity.RegistrationCode, Code: identity.Code, Name: identity.Name, Currency: identity.Currency, ValuationBasis: "cny_wealth_unit_nav", InceptionDate: date, SourceAdapter: identity.Adapter, SourceCode: identity.SourceCode, AdmissionStatus: "enabled", EvidenceVersion: "isolated-browser-fixture-only"}).Error; err != nil {
			panic(err)
		}
	}
	r.POST("/api/v1/__test__/wealth-ensure", func(c *gin.Context) {
		if err := svc.Ensure(c.Request.Context()); err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	r.POST("/api/v1/__test__/wealth-admission", func(c *gin.Context) {
		var in struct {
			Status string `json:"status"`
		}
		if c.ShouldBindJSON(&in) != nil || (in.Status != "enabled" && in.Status != "pending_validation" && in.Status != "retired") {
			c.Status(400)
			return
		}
		if err := db.Model(&model.WealthProduct{}).Where("id = ?", 1).Updates(map[string]any{"admission_status": in.Status, "evidence_version": "isolated-browser-fixture-only"}).Error; err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	r.POST("/api/v1/__test__/wealth-quote", func(c *gin.Context) {
		var in struct {
			Code    string `json:"code"`
			Date    string `json:"date"`
			NAV     string `json:"nav"`
			Failure bool   `json:"failure"`
			Advance int64  `json:"advance"`
		}
		if c.ShouldBindJSON(&in) != nil {
			c.Status(400)
			return
		}
		market.mu.Lock()
		defer market.mu.Unlock()
		if in.Code == "" {
			in.Code = "J07241"
		}
		identity, allowed := wealthdata.Catalog(in.Code)
		if !allowed {
			c.Status(400)
			return
		}
		rows := market.rows
		if in.Code != "J07241" {
			if market.products == nil {
				market.products = map[string][]suyin.NAV{}
				market.failures = map[string]bool{}
			}
			rows = market.products[in.Code]
		}
		if in.Date != "" {
			date, err := time.Parse("2006-01-02", in.Date)
			if err != nil {
				c.Status(400)
				return
			}
			price, err := decimal.NewFromString(in.NAV)
			if err != nil || !price.IsPositive() {
				c.Status(400)
				return
			}
			found := false
			for i := range rows {
				if rows[i].Date.Equal(date) {
					rows[i].Value = price
					found = true
					break
				}
			}
			if !found {
				rows = append(rows, suyin.NAV{Date: date, Value: price, SourceCode: identity.SourceCode})
			}
		}
		if in.Code == "J07241" {
			market.rows = rows
			market.failure = in.Failure
		} else {
			market.products[in.Code] = rows
			market.failures[in.Code] = in.Failure
		}
		if in.Advance < 61 {
			in.Advance = 61
		}
		clock.Add(in.Advance)
		c.JSON(200, gin.H{"queued": true})
	})
	r.GET("/api/v1/__test__/wealth-calls", func(c *gin.Context) { c.JSON(200, gin.H{"calls": market.calls.Load()}) })
}
