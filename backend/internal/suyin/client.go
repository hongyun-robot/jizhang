// Package suyin validates public NAVs collected from an anonymous official page.
// Personal financial data is never sent to the collector or the bank.
package suyin

import (
	"context"
	"encoding/json"
	"io"
	"jizhang/internal/wealthdata"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var (
	ErrUnavailable  = wealthdata.ErrUnavailable
	ErrUnconfigured = wealthdata.ErrUnconfigured
	ErrRateLimited  = wealthdata.ErrRateLimited
)

var Shanghai = wealthdata.Shanghai

type Product = wealthdata.Product

// Catalog is returned by value so callers cannot mutate the allowlist.
func Catalog(code string) (Product, bool) {
	product, ok := wealthdata.Catalog(code)
	if !ok || product.Adapter != "suyin" {
		return Product{}, false
	}
	return product, true
}

type NAV = wealthdata.NAV
type MarketData = wealthdata.MarketData

type Client struct {
	base string
	key  string
	http *http.Client
	now  func() time.Time
}

func New(base, key string) *Client { return NewAt(base, key, nil) }
func NewAt(base, key string, transport http.RoundTripper) *Client {
	return &Client{base: strings.TrimRight(base, "/"), key: key, now: time.Now, http: &http.Client{
		Timeout: 60 * time.Second, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable },
	}}
}

func (c *Client) Configured() bool {
	u, err := url.Parse(c.base)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && len(strings.TrimSpace(c.key)) >= 24
}

func (c *Client) NAVs(ctx context.Context, code, _ string) ([]NAV, error) {
	product, ok := Catalog(code)
	if !ok {
		return nil, ErrUnavailable
	}
	return c.Fetch(ctx, product, "all")
}

func (c *Client) Fetch(ctx context.Context, product Product, _ string) ([]NAV, error) {
	if product.Adapter != "suyin" || product.Currency != "CNY" {
		return nil, ErrUnavailable
	}
	if !c.Configured() {
		return nil, ErrUnconfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/products/"+product.Code+"/nav", nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	response, err := c.http.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, ErrRateLimited
	}
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		return nil, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return nil, ErrUnavailable
	}
	return Parse(body, product, c.now())
}

func positive(raw json.RawMessage) (decimal.Decimal, error) {
	var value string
	if len(raw) > 64 || json.Unmarshal(raw, &value) != nil {
		return decimal.Zero, ErrUnavailable
	}
	if value == "" || strings.ContainsAny(value, "eE+ \t\r\n") {
		return decimal.Zero, ErrUnavailable
	}
	d, err := decimal.NewFromString(value)
	if err != nil || !d.IsPositive() || !d.Round(8).Equal(d) || d.GreaterThan(decimal.RequireFromString("9999999999.99")) {
		return decimal.Zero, ErrUnavailable
	}
	return d, nil
}

// Parse validates the entire batch before callers persist any row.
func Parse(body []byte, product Product, now time.Time) ([]NAV, error) {
	var envelope struct {
		Code             string    `json:"code"`
		Name             string    `json:"name"`
		RegistrationCode string    `json:"registration_code"`
		Currency         string    `json:"currency"`
		RequestCode      string    `json:"request_fund_code"`
		SourceCode       string    `json:"source_code"`
		SourceURL        string    `json:"source_url"`
		FetchedAt        time.Time `json:"fetched_at"`
		Response         struct {
			Code string `json:"code"`
			Data []struct {
				Date       string          `json:"cfm_date"`
				Value      json.RawMessage `json:"nav"`
				TotalValue json.RawMessage `json:"tot_nav"`
				SourceCode string          `json:"prd_code"`
			} `json:"data"`
		} `json:"response"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Code != product.Code || envelope.RequestCode != product.Code || envelope.Name != product.Name || envelope.RegistrationCode != product.RegistrationCode || envelope.Currency != product.Currency || envelope.SourceURL != product.PageURL || envelope.SourceCode != product.SourceCode || envelope.Response.Code != "0000" || len(envelope.Response.Data) == 0 || len(envelope.Response.Data) > 9999 || envelope.FetchedAt.IsZero() || envelope.FetchedAt.After(now.Add(5*time.Minute)) || envelope.FetchedAt.Before(now.Add(-5*time.Minute)) {
		return nil, ErrUnavailable
	}
	today := now.In(Shanghai).Format("2006-01-02")
	byDate := map[string]NAV{}
	for _, row := range envelope.Response.Data {
		date, err := time.Parse("2006-01-02", row.Date)
		if err != nil || date.Format("2006-01-02") != row.Date || row.Date < product.InceptionDate || row.Date > today || row.SourceCode != product.SourceCode {
			return nil, ErrUnavailable
		}
		value, err := positive(row.Value)
		if err != nil {
			return nil, err
		}
		nav := NAV{Date: date, Value: value, FetchedAt: envelope.FetchedAt, SourceCode: row.SourceCode}
		if len(row.TotalValue) > 0 && string(row.TotalValue) != "null" && string(row.TotalValue) != `""` {
			total, err := positive(row.TotalValue)
			if err != nil {
				return nil, err
			}
			nav.TotalValue = &total
		}
		if old, ok := byDate[row.Date]; ok {
			if !old.Value.Equal(value) || (old.TotalValue == nil) != (nav.TotalValue == nil) || (old.TotalValue != nil && !old.TotalValue.Equal(*nav.TotalValue)) {
				return nil, ErrUnavailable
			}
		}
		byDate[row.Date] = nav
	}
	result := make([]NAV, 0, len(byDate))
	for _, nav := range byDate {
		result = append(result, nav)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date.Before(result[j].Date) })
	return result, nil
}
