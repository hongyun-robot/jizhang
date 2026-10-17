// Package cmbcwm reads public unit NAVs from Minsheng Wealth's official site.
package cmbcwm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"jizhang/internal/wealthdata"
)

const baseURL = "https://www.cmbcwm.com.cn/gw/po_web/"
const pageSize = 100
const maxRows = 9999

type Client struct {
	http *http.Client
	now  func() time.Time
}

func New() *Client { return NewAt(nil) }

func NewAt(transport http.RoundTripper) *Client {
	return &Client{now: time.Now, http: &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return wealthdata.ErrUnavailable
		},
	}}
}

func (c *Client) Configured() bool { return true }

type returnCode struct {
	Type string `json:"type"`
	Code string `json:"code"`
}

func (r returnCode) valid() bool { return r.Type == "S" && r.Code == "AAAAAAA" }

func (c *Client) post(ctx context.Context, endpoint string, form url.Values, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return wealthdata.ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return wealthdata.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return wealthdata.ErrRateLimited
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || response.StatusCode != http.StatusOK || mediaType != "application/json" {
		return wealthdata.ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return wealthdata.ErrUnavailable
	}
	if json.Unmarshal(body, result) != nil {
		return wealthdata.ErrUnavailable
	}
	return nil
}

func (c *Client) identity(ctx context.Context, product wealthdata.Product) error {
	var response struct {
		ReturnCode returnCode `json:"returnCode"`
		Total      int        `json:"totalSize"`
		List       []struct {
			Code         string `json:"REAL_PRD_CODE"`
			Name         string `json:"PRD_NAME"`
			Registration string `json:"TBPRDEXTEND_DEBT_REGIST_CODE"`
			Currency     string `json:"CURR_TYPE"`
			Inception    int    `json:"ESTAB_DATE"`
		} `json:"list"`
	}
	err := c.post(ctx, "BTAProductQry", url.Values{
		"prd_type": {"0"}, "code_or_name": {product.Code}, "pageNo": {"1"}, "pageSize": {"10"},
	}, &response)
	if err != nil {
		return err
	}
	if !response.ReturnCode.valid() || response.Total != 1 || len(response.List) != 1 {
		return wealthdata.ErrUnavailable
	}
	row := response.List[0]
	if row.Code != product.Code || row.Name != product.Name || row.Registration != product.RegistrationCode || row.Currency != "156" || strconv.Itoa(row.Inception) != strings.ReplaceAll(product.InceptionDate, "-", "") {
		return wealthdata.ErrUnavailable
	}
	return nil
}

type dailyResponse struct {
	ReturnCode returnCode `json:"returnCode"`
	Total      int        `json:"totalSize"`
	Cash       string     `json:"isJXGL"`
	List       []struct {
		Date  int             `json:"ISS_DATE"`
		Unit  json.RawMessage `json:"NAV"`
		Total json.RawMessage `json:"TOT_NAV"`
	} `json:"list"`
}

func price(raw json.RawMessage) (decimal.Decimal, error) {
	raw = bytes.TrimSpace(raw)
	value := string(raw)
	if len(raw) > 64 || len(raw) == 0 {
		return decimal.Zero, wealthdata.ErrUnavailable
	}
	if raw[0] == '"' && json.Unmarshal(raw, &value) != nil {
		return decimal.Zero, wealthdata.ErrUnavailable
	}
	if value == "" || strings.ContainsAny(value, "eE+ \t\r\n") {
		return decimal.Zero, wealthdata.ErrUnavailable
	}
	result, err := decimal.NewFromString(value)
	if err != nil || !result.IsPositive() || !result.Round(8).Equal(result) || result.GreaterThan(decimal.RequireFromString("9999999999.99")) {
		return decimal.Zero, wealthdata.ErrUnavailable
	}
	return result, nil
}

// NAVs returns only a complete, validated history. A failed later page cannot
// replace earlier valid quotes, and the existing scheduler owns all retries.
func (c *Client) NAVs(ctx context.Context, code, _ string) ([]wealthdata.NAV, error) {
	product, ok := wealthdata.Catalog(code)
	if !ok {
		return nil, wealthdata.ErrUnavailable
	}
	return c.Fetch(ctx, product, "all")
}

func (c *Client) Fetch(ctx context.Context, product wealthdata.Product, _ string) ([]wealthdata.NAV, error) {
	if product.Adapter != "cmbcwm" || product.Currency != "CNY" || product.ValuationBasis != "cny_wealth_unit_nav" {
		return nil, wealthdata.ErrUnavailable
	}
	if err := c.identity(ctx, product); err != nil {
		return nil, err
	}
	fetchedAt := c.now()
	today := fetchedAt.In(wealthdata.Shanghai).Format("20060102")
	rows := make(map[int]wealthdata.NAV)
	expected := 0
	for page := 1; page <= 100; page++ {
		var response dailyResponse
		err := c.post(ctx, "BTADailyQry", url.Values{
			"chart_type": {"1"}, "real_prd_code": {product.Code},
			"pageNo": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)},
		}, &response)
		if err != nil {
			return nil, err
		}
		if !response.ReturnCode.valid() || response.Cash != "0" || response.Total <= 0 || response.Total > maxRows || len(response.List) == 0 || len(response.List) > pageSize {
			return nil, wealthdata.ErrUnavailable
		}
		if page == 1 {
			expected = response.Total
		} else if response.Total != expected {
			return nil, wealthdata.ErrUnavailable
		}
		previousCount := len(rows)
		for _, row := range response.List {
			dateText := strconv.Itoa(row.Date)
			date, err := time.Parse("20060102", dateText)
			if err != nil || date.Format("20060102") != dateText || date.Format("2006-01-02") < product.InceptionDate || dateText > today {
				return nil, wealthdata.ErrUnavailable
			}
			unit, err := price(row.Unit)
			if err != nil {
				return nil, err
			}
			nav := wealthdata.NAV{Date: date, Value: unit, SourceCode: product.SourceCode, FetchedAt: fetchedAt}
			if len(row.Total) > 0 && string(row.Total) != "null" {
				total, err := price(row.Total)
				if err != nil {
					return nil, err
				}
				nav.TotalValue = &total
			}
			if _, duplicate := rows[row.Date]; duplicate {
				return nil, wealthdata.ErrUnavailable
			}
			rows[row.Date] = nav
		}
		if len(rows) > expected || len(rows) == previousCount {
			return nil, wealthdata.ErrUnavailable
		}
		if len(rows) == expected {
			result := make([]wealthdata.NAV, 0, len(rows))
			for _, nav := range rows {
				result = append(result, nav)
			}
			sort.Slice(result, func(i, j int) bool { return result[i].Date.Before(result[j].Date) })
			return result, nil
		}
	}
	return nil, wealthdata.ErrUnavailable
}
