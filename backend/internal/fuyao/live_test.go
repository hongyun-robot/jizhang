package fuyao

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Explicit read-only smoke: credentials are only sent as a request header.
func TestFundLiveSmoke(t *testing.T) {
	if os.Getenv("FUYAO_LIVE_SMOKE") != "1" {
		t.Skip("set FUYAO_LIVE_SMOKE=1 for explicit public-data smoke")
	}
	c := New(os.Getenv("FUYAO_API_KEY"))
	if !c.Configured() {
		t.Fatal("FUYAO_API_KEY is not available to the process")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	items, e := c.Search(ctx, "023564")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, i := range items {
		if i.THSCode == "023564.OF" {
			found = true
		}
	}
	if !found {
		t.Fatal("exact fund code not resolved")
	}
	p, e := c.Profile(ctx, "023564.OF")
	if e != nil {
		t.Fatal(e)
	}
	quotes, e := c.NAVs(ctx, "023564.OF", "month")
	if e != nil || len(quotes) < 2 {
		t.Fatal("insufficient public NAV data", e)
	}
	last := quotes[len(quotes)-1]
	summary, _ := json.Marshal(struct {
		Code       string `json:"code"`
		SourceName string `json:"source_name"`
		Manager    string `json:"manager"`
		Date       string `json:"nav_date"`
		NAV        string `json:"unit_nav"`
		Rows       int    `json:"rows"`
	}{p.THSCode, p.Name, p.Manager, last.Date.Format("2006-01-02"), last.Value.String(), len(quotes)})
	t.Log(string(summary))
}
