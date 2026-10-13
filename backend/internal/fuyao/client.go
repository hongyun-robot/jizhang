// Package fuyao adapts public fund market data. Personal holdings never leave the application.
package fuyao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shopspring/decimal"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

var ErrUnavailable = errors.New("基金行情暂不可用")
var ErrUnconfigured = errors.New("尚未配置基金行情密钥")
var ErrRateLimited = errors.New("基金行情请求过于频繁，请稍后重试")
var Shanghai = func() *time.Location {
	l, e := time.LoadLocation("Asia/Shanghai")
	if e != nil {
		return time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return l
}()

type Instrument struct {
	THSCode   string `json:"thscode"`
	Ticker    string `json:"ticker"`
	Name      string `json:"name"`
	AssetType string `json:"asset_type"`
	Currency  string `json:"currency"`
}
type Profile struct {
	THSCode    string `json:"thscode"`
	Ticker     string `json:"ticker"`
	Name       string `json:"fund_name"`
	Management string `json:"mgmt_name"`
	Manager    string `json:"manager_name"`
}
type NAV struct {
	Date  time.Time
	Value decimal.Decimal
}
type Dividend struct {
	RegistrationDate time.Time
	ExDate           time.Time
	PaymentDate      *time.Time
	PerTenCash       decimal.Decimal
	Key              string
}

type MarketData interface {
	Configured() bool
	Search(context.Context, string) ([]Instrument, error)
	Profile(context.Context, string) (Profile, error)
	NAVs(context.Context, string, string) ([]NAV, error)
	Dividends(context.Context, string) ([]Dividend, error)
}

type Client struct {
	base string
	key  string
	http *http.Client
	now  func() time.Time
}

func New(key string) *Client { return NewAt("https://fuyao.aicubes.cn", key, nil) }
func NewAt(base, key string, transport http.RoundTripper) *Client {
	c := &Client{base: strings.TrimRight(base, "/"), key: strings.TrimSpace(key), now: time.Now}
	c.http = &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme {
			return ErrUnavailable
		}
		return nil
	}}
	return c
}
func (c *Client) Configured() bool { return c.key != "" }

func (c *Client) get(ctx context.Context, path string, params url.Values) ([]json.RawMessage, error) {
	if !c.Configured() {
		return nil, ErrUnconfigured
	}
	u, err := url.Parse(c.base + path)
	if err != nil {
		return nil, ErrUnavailable
	}
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("X-api-key", c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		return nil, ErrRateLimited
	}
	if resp.StatusCode != 200 {
		return nil, ErrUnavailable
	}
	const maxBody = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return nil, ErrUnavailable
	}
	var env struct {
		Code int `json:"code"`
		Data *struct {
			Items []json.RawMessage `json:"item"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &env) != nil {
		return nil, ErrUnavailable
	}
	if env.Code == 4001 {
		return nil, ErrRateLimited
	}
	if env.Code != 0 || env.Data == nil {
		return nil, ErrUnavailable
	}
	return env.Data.Items, nil
}
func (c *Client) Search(ctx context.Context, q string) ([]Instrument, error) {
	q = strings.TrimSpace(q)
	if q == "" || len(q) > 128 {
		return nil, ErrUnavailable
	}
	rows, err := c.get(ctx, "/api/meta/tickers/search", url.Values{"q": {q}, "asset_type": {"fund-otc"}, "limit": {"20"}})
	if err != nil {
		return nil, err
	}
	result := []Instrument{}
	for _, row := range rows {
		var item Instrument
		if json.Unmarshal(row, &item) != nil {
			return nil, ErrUnavailable
		}
		if item.AssetType == "fund-otc" && strings.HasSuffix(item.THSCode, ".OF") {
			result = append(result, item)
		}
	}
	return result, nil
}
func (c *Client) Profile(ctx context.Context, code string) (Profile, error) {
	rows, err := c.get(ctx, "/api/fund/profile/detail", url.Values{"thscode": {code}})
	if err != nil {
		return Profile{}, err
	}
	if len(rows) != 1 {
		return Profile{}, ErrUnavailable
	}
	var p Profile
	if json.Unmarshal(rows[0], &p) != nil || p.THSCode != code {
		return Profile{}, ErrUnavailable
	}
	return p, nil
}
func (c *Client) day(ms int64) (time.Time, error) {
	if ms <= 0 {
		return time.Time{}, ErrUnavailable
	}
	t := time.UnixMilli(ms).In(Shanghai)
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, Shanghai)
	if d.Year() < 2000 || d.After(c.now().In(Shanghai)) {
		return time.Time{}, ErrUnavailable
	}
	return d, nil
}
func number(n json.Number) (decimal.Decimal, error) {
	d, e := decimal.NewFromString(string(n))
	if e != nil || d.IsNegative() || d.Round(8).Equal(d) == false || d.GreaterThan(decimal.RequireFromString("9999999999.99")) {
		return decimal.Zero, ErrUnavailable
	}
	return d, nil
}
func (c *Client) NAVs(ctx context.Context, code, window string) ([]NAV, error) {
	rows, err := c.get(ctx, "/api/fund/performance/nav", url.Values{"thscode": {code}, "range": {window}, "nav_type": {"unit"}})
	if err != nil {
		return nil, err
	}
	byDate := map[string]NAV{}
	for _, row := range rows {
		var item struct {
			Date  int64       `json:"nav_date"`
			Value json.Number `json:"unit_nav"`
		}
		if json.Unmarshal(row, &item) != nil {
			return nil, ErrUnavailable
		}
		d, e := c.day(item.Date)
		if e != nil {
			return nil, e
		}
		n, e := number(item.Value)
		if e != nil || !n.IsPositive() {
			return nil, ErrUnavailable
		}
		k := d.Format("2006-01-02")
		if prev, ok := byDate[k]; ok && !prev.Value.Equal(n) {
			return nil, ErrUnavailable
		}
		byDate[k] = NAV{d, n}
	}
	result := make([]NAV, 0, len(byDate))
	for _, v := range byDate {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date.Before(result[j].Date) })
	return result, nil
}
func (c *Client) Dividends(ctx context.Context, code string) ([]Dividend, error) {
	rows, err := c.get(ctx, "/api/fund/corporate-actions/dividends", url.Values{"thscode": {code}})
	if err != nil {
		return nil, err
	}
	result := []Dividend{}
	for _, row := range rows {
		var item struct {
			Registration *int64      `json:"registration_date_ms"`
			Ex           *int64      `json:"ex_dividend_date_ms"`
			Payment      *int64      `json:"payment_date_ms"`
			Cash         json.Number `json:"per_ten_cash_after_tax"`
		}
		if json.Unmarshal(row, &item) != nil {
			return nil, ErrUnavailable
		}
		if item.Ex == nil && item.Registration == nil && item.Cash == "" {
			continue
		}
		if item.Ex == nil || item.Registration == nil {
			return nil, ErrUnavailable
		}
		ex, e := c.day(*item.Ex)
		if e != nil {
			continue
		}
		reg, e := c.day(*item.Registration)
		if e != nil {
			return nil, e
		}
		cash, e := number(item.Cash)
		if e != nil {
			return nil, e
		}
		if !cash.IsPositive() {
			continue
		}
		var payment *time.Time
		if item.Payment != nil && *item.Payment > 0 {
			p := time.UnixMilli(*item.Payment).In(Shanghai)
			d := time.Date(p.Year(), p.Month(), p.Day(), 0, 0, 0, 0, Shanghai)
			payment = &d
		}
		result = append(result, Dividend{RegistrationDate: reg, ExDate: ex, PaymentDate: payment, PerTenCash: cash, Key: fmt.Sprintf("%s:%s:%s", code, ex.Format("2006-01-02"), cash.String())})
	}
	return result, nil
}
