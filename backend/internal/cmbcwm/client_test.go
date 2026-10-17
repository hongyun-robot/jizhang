package cmbcwm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"jizhang/internal/wealthdata"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonReply(value any) *http.Response {
	body, _ := json.Marshal(value)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json;charset=UTF-8"}}, Body: io.NopCloser(strings.NewReader(string(body)))}
}

func goodIdentity() map[string]any {
	return map[string]any{"returnCode": returnCode{Type: "S", Code: "AAAAAAA"}, "totalSize": 1, "list": []map[string]any{{
		"REAL_PRD_CODE": "FBAE68020E", "PRD_NAME": "民生理财贵竹固收增利天天盈9号理财产品E", "TBPRDEXTEND_DEBT_REGIST_CODE": "Z7007923000092", "CURR_TYPE": "156", "ESTAB_DATE": 20230620,
	}}}
}

func goodDaily() map[string]any {
	return map[string]any{"returnCode": returnCode{Type: "S", Code: "AAAAAAA"}, "totalSize": 1, "isJXGL": "0", "list": []map[string]any{{"ISS_DATE": 20261015, "NAV": json.Number("1.068033"), "TOT_NAV": "1.068033", "INCOME": 0, "WEEK_CLIENTRATE": 0}}}
}

func fixtureClient(t *testing.T, handler func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()
	client := NewAt(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Scheme != "https" || req.URL.Host != "www.cmbcwm.com.cn" || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.URL.RawQuery != "" {
			t.Fatalf("not a fixed anonymous official request: %v", req)
		}
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		return handler(req)
	}))
	client.now = func() time.Time { return time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC) }
	return client
}

func TestAnonymousIdentityAndCompletePagination(t *testing.T) {
	all := []map[string]any{}
	latest := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for i := range 101 {
		day, _ := strconv.Atoi(latest.AddDate(0, 0, -i).Format("20060102"))
		all = append(all, map[string]any{"ISS_DATE": day, "NAV": json.Number("1.068033"), "TOT_NAV": json.Number("1.068033")})
	}
	calls := 0
	client := fixtureClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if strings.HasSuffix(req.URL.Path, "/BTAProductQry") {
			if req.Form.Get("code_or_name") != "FBAE68020E" || req.Form.Get("prd_type") != "0" {
				t.Fatal(req.Form)
			}
			return jsonReply(goodIdentity()), nil
		}
		if req.URL.Path != "/gw/po_web/BTADailyQry" || req.Form.Get("real_prd_code") != "FBAE68020E" || req.Form.Get("chart_type") != "1" || req.Form.Get("pageSize") != "100" {
			t.Fatal(req.Form)
		}
		page, _ := strconv.Atoi(req.Form.Get("pageNo"))
		start, end := (page-1)*pageSize, min(page*pageSize, len(all))
		return jsonReply(map[string]any{"returnCode": returnCode{Type: "S", Code: "AAAAAAA"}, "totalSize": len(all), "isJXGL": "0", "list": all[start:end]}), nil
	})
	rows, err := client.NAVs(context.Background(), "FBAE68020E", "all")
	if err != nil || len(rows) != 101 || calls != 3 {
		t.Fatal(len(rows), calls, err)
	}
	if rows[100].Value.String() != "1.068033" || rows[100].TotalValue.String() != "1.068033" || rows[100].Date.Format("2006-01-02") != "2026-10-15" || rows[100].SourceCode != "FBAE68020E" || !rows[100].FetchedAt.Equal(client.now()) || !rows[0].Date.Before(rows[100].Date) {
		t.Fatal(rows[100])
	}
}

func TestAdapterAcceptsAnotherConfiguredShareWithoutBusinessSwitch(t *testing.T) {
	product, _ := wealthdata.Catalog("FBAE68020E")
	product.Code, product.SourceCode, product.Name, product.RegistrationCode = "TESTNEXT", "TESTNEXT", "测试新理财份额", "ZTESTNEXT"
	client := fixtureClient(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/BTAProductQry") {
			if req.Form.Get("code_or_name") != product.Code {
				t.Fatal(req.Form)
			}
			identity := goodIdentity()
			row := identity["list"].([]map[string]any)[0]
			row["REAL_PRD_CODE"], row["PRD_NAME"], row["TBPRDEXTEND_DEBT_REGIST_CODE"] = product.Code, product.Name, product.RegistrationCode
			return jsonReply(identity), nil
		}
		if req.Form.Get("real_prd_code") != product.Code {
			t.Fatal(req.Form)
		}
		return jsonReply(goodDaily()), nil
	})
	rows, err := client.Fetch(context.Background(), product, "all")
	if err != nil || len(rows) != 1 || rows[0].SourceCode != product.SourceCode {
		t.Fatal(rows, err)
	}
}

func TestInvalidIdentityAndNAVBatchRejected(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any, map[string]any)
	}{
		{"wrong-share", func(id, nav map[string]any) { id["list"].([]map[string]any)[0]["REAL_PRD_CODE"] = "FBAE68020A" }},
		{"wrong-registration", func(id, nav map[string]any) {
			id["list"].([]map[string]any)[0]["TBPRDEXTEND_DEBT_REGIST_CODE"] = "other"
		}},
		{"wrong-currency", func(id, nav map[string]any) { id["list"].([]map[string]any)[0]["CURR_TYPE"] = "840" }},
		{"ambiguous-identity", func(id, nav map[string]any) { id["totalSize"] = 2 }},
		{"identity-business-error", func(id, nav map[string]any) { id["returnCode"] = returnCode{Type: "E", Code: "AAAAAAA"} }},
		{"business-error", func(id, nav map[string]any) { nav["returnCode"] = returnCode{Type: "E", Code: "AAAAAAA"} }},
		{"cash-yield-basis", func(id, nav map[string]any) { nav["isJXGL"] = "1" }},
		{"missing-total", func(id, nav map[string]any) { delete(nav, "totalSize") }},
		{"empty", func(id, nav map[string]any) { nav["list"] = []any{} }},
		{"future-date", func(id, nav map[string]any) { nav["list"].([]map[string]any)[0]["ISS_DATE"] = 20261017 }},
		{"invalid-date", func(id, nav map[string]any) { nav["list"].([]map[string]any)[0]["ISS_DATE"] = 20260230 }},
		{"before-inception", func(id, nav map[string]any) { nav["list"].([]map[string]any)[0]["ISS_DATE"] = 20230619 }},
		{"zero-unit", func(id, nav map[string]any) { nav["list"].([]map[string]any)[0]["NAV"] = 0 }},
		{"negative-unit", func(id, nav map[string]any) { nav["list"].([]map[string]any)[0]["NAV"] = -1 }},
		{"missing-unit", func(id, nav map[string]any) { delete(nav["list"].([]map[string]any)[0], "NAV") }},
		{"nan", func(id, nav map[string]any) { nav["list"].([]map[string]any)[0]["NAV"] = "NaN" }},
		{"exponent", func(id, nav map[string]any) { nav["list"].([]map[string]any)[0]["NAV"] = "1e1" }},
		{"excess-precision", func(id, nav map[string]any) { nav["list"].([]map[string]any)[0]["NAV"] = "1.123456789" }},
		{"invalid-cumulative", func(id, nav map[string]any) { nav["list"].([]map[string]any)[0]["TOT_NAV"] = -1 }},
		{"duplicate-date-conflict", func(id, nav map[string]any) {
			nav["totalSize"] = 2
			nav["list"] = append(nav["list"].([]map[string]any), map[string]any{"ISS_DATE": 20261015, "NAV": 2})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity, daily := goodIdentity(), goodDaily()
			test.mutate(identity, daily)
			client := fixtureClient(t, func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "BTAProductQry") {
					return jsonReply(identity), nil
				}
				return jsonReply(daily), nil
			})
			rows, err := client.NAVs(context.Background(), "FBAE68020E", "all")
			if !errors.Is(err, wealthdata.ErrUnavailable) || rows != nil {
				t.Fatal("invalid batch accepted", rows, err)
			}
		})
	}
}

func TestLaterPageFailureAndRepeatedPageDoNotReturnPartialHistory(t *testing.T) {
	for _, mode := range []string{"network", "empty", "changed-total", "repeated-page"} {
		t.Run(mode, func(t *testing.T) {
			client := fixtureClient(t, func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "BTAProductQry") {
					return jsonReply(goodIdentity()), nil
				}
				daily := goodDaily()
				daily["totalSize"] = 2
				if req.Form.Get("pageNo") == "2" {
					switch mode {
					case "network":
						return nil, errors.New("offline")
					case "empty":
						daily["list"] = []any{}
					case "changed-total":
						daily["totalSize"] = 3
					}
				}
				return jsonReply(daily), nil
			})
			rows, err := client.NAVs(context.Background(), "FBAE68020E", "all")
			if rows != nil || err == nil {
				t.Fatal("partial or repeated history accepted", rows, err)
			}
		})
	}
}

func TestHTTPFailuresAndUnknownProducts(t *testing.T) {
	for _, status := range []int{200, 302, 412, 429, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			client := fixtureClient(t, func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("<html>challenge</html>"))}, nil
			})
			rows, err := client.NAVs(context.Background(), "FBAE68020E", "all")
			want := wealthdata.ErrUnavailable
			if status == 429 {
				want = wealthdata.ErrRateLimited
			}
			if rows != nil || !errors.Is(err, want) {
				t.Fatal(rows, err)
			}
		})
	}
	client := fixtureClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("unknown product reached bank"); return nil, nil })
	for _, code := range []string{"FBAE68020A", "Z7007923000092", "J07241", "unknown"} {
		if rows, err := client.NAVs(context.Background(), code, "all"); rows != nil || err == nil {
			t.Fatal(code, rows, err)
		}
	}
}

func TestLiveAnonymousHistory(t *testing.T) {
	if os.Getenv("CMBCWM_LIVE_TEST") != "1" {
		t.Skip("set CMBCWM_LIVE_TEST=1 for explicit anonymous official-source verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	client := NewAt(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		response, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Logf("%s transport error: %v", req.URL.Path, err)
		} else {
			t.Logf("%s status=%d type=%s", req.URL.Path, response.StatusCode, response.Header.Get("Content-Type"))
		}
		return response, err
	}))
	rows, err := client.NAVs(ctx, "FBAE68020E", "all")
	if err != nil || len(rows) == 0 {
		t.Fatal(len(rows), err)
	}
	latest := rows[len(rows)-1]
	t.Logf("public source verified: share=%s count=%d first=%s latest=%s unit_nav=%s", latest.SourceCode, len(rows), rows[0].Date.Format("2006-01-02"), latest.Date.Format("2006-01-02"), latest.Value)
}
