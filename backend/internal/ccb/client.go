// Package ccb reads public CCB gold accumulation quotes using an anonymous session.
package ccb

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

const ProductKey = "ccb:personal-gold-accumulation:CNY"

var ErrUnavailable = errors.New("建行积存金报价暂不可用，请稍后刷新")
var Shanghai = func() *time.Location {
	l, e := time.LoadLocation("Asia/Shanghai")
	if e != nil {
		return time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return l
}()

type Quote struct {
	Buy, Sell decimal.Decimal
	At        time.Time
}
type Market interface {
	Quote(context.Context) (Quote, error)
}
type Client struct {
	base  string
	http  *http.Client
	now   func() time.Time
	mu    sync.Mutex
	ready bool
}

func New() *Client { return NewAt("https://www2.ccb.com", nil, time.Now) }
func NewAt(base string, transport http.RoundTripper, now func() time.Time) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{base: strings.TrimRight(base, "/"), now: now, http: &http.Client{Jar: jar, Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme {
			return ErrUnavailable
		}
		return nil
	}}}
}
func (c *Client) get(ctx context.Context, code string) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/tran/WCCMainPlatV5?CCB_IBSVersion=V5&SERVLET_NAME=WCCMainPlatV5&TXCODE="+code, nil)
	if e != nil {
		return nil, ErrUnavailable
	}
	// The public portal returns an empty HTTP 200 to the default Go client UA.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; JizhangGold/1.0)")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	r, e := c.http.Do(req)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, (4<<20)+1))
	if e != nil || len(b) > 4<<20 {
		return nil, ErrUnavailable
	}
	return b, nil
}
func (c *Client) Quote(ctx context.Context) (Quote, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if !c.ready {
			if _, e := c.get(ctx, "100119"); e != nil {
				return Quote{}, e
			}
			c.ready = true
		}
		b, e := c.get(ctx, "NGJS01")
		if e != nil {
			return Quote{}, e
		}
		// A login HTML page or explicit session error can be recovered once.
		if strings.Contains(string(b), "重新登录") || strings.Contains(string(b), "0060Z1108006") {
			c.ready = false
			jar, _ := cookiejar.New(nil)
			c.http.Jar = jar
			continue
		}
		return Parse(b, c.now())
	}
	return Quote{}, ErrUnavailable
}
func Parse(b []byte, now time.Time) (Quote, error) {
	var raw struct {
		Success  string `json:"SUCCESS"`
		Code     string `json:"retCode"`
		Product  string `json:"PM_Txn_Vrty_Cd"`
		Currency string `json:"CcyCd"`
		Buy      string `json:"Cst_Buy_Prc"`
		Sell     string `json:"Cst_Sell_Prc"`
		At       string `json:"Tms"`
	}
	if json.Unmarshal(b, &raw) != nil || raw.Success != "true" || raw.Code != "000000" || raw.Product != "33" || raw.Currency != "156" {
		return Quote{}, ErrUnavailable
	}
	buy, e1 := decimal.NewFromString(raw.Buy)
	sell, e2 := decimal.NewFromString(raw.Sell)
	if e1 != nil || e2 != nil || !buy.IsPositive() || !sell.IsPositive() || buy.GreaterThan(decimal.NewFromInt(9999999999)) || sell.GreaterThan(decimal.NewFromInt(9999999999)) || !buy.Round(8).Equal(buy) || !sell.Round(8).Equal(sell) {
		return Quote{}, ErrUnavailable
	}
	at, e := time.ParseInLocation("2006-01-02 15:04:05.999999999", raw.At, Shanghai)
	if e != nil || at.Year() < 2000 || at.After(now.Add(5*time.Minute)) {
		return Quote{}, ErrUnavailable
	}
	return Quote{buy, sell, at}, nil
}
