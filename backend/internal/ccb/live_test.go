package ccb

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestGoldLiveSmoke(t *testing.T) {
	if os.Getenv("CCB_GOLD_LIVE_SMOKE") != "1" {
		t.Skip("explicit public quotation smoke only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	q, e := New().Quote(ctx)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("CCB customer buy=%s sell=%s quote_at=%s", q.Buy, q.Sell, q.At.Format(time.RFC3339Nano))
}
