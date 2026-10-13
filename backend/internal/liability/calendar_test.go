package liability

import (
	"github.com/shopspring/decimal"
	"jizhang/internal/model"
	"testing"
)

func TestCalendarAndFees(t *testing.T) {
	cases := []struct {
		date             string
		day, due, offset int
		start, end, pay  string
	}{
		{"2026-10-15", 31, 10, 1, "2026-10-01", "2026-10-31", "2026-11-10"},
		{"2026-10-20", 20, 25, 0, "2026-09-21", "2026-10-20", "2026-10-25"},
		{"2026-10-21", 20, 25, 0, "2026-10-21", "2026-11-20", "2026-11-25"},
		{"2028-02-29", 31, 31, 1, "2028-02-01", "2028-02-29", "2028-03-31"},
		{"2027-02-28", 31, 31, 1, "2027-02-01", "2027-02-28", "2027-03-31"},
		{"2026-12-31", 31, 10, 1, "2026-12-01", "2026-12-31", "2027-01-10"},
	}
	for _, c := range cases {
		if e := ValidateRule(c.day, c.due, c.offset); e != nil {
			t.Fatalf("valid rule rejected %+v: %v", c, e)
		}
		a, b, d, e := Range(model.LiabilityBillRule{StatementDay: c.day, RepaymentDay: c.due, RepaymentMonthOffset: c.offset, EffectiveFrom: "2000-01-01"}, c.date)
		if e != nil || a != c.start || b != c.end || d != c.pay {
			t.Fatalf("%+v: %s %s %s %v", c, a, b, d, e)
		}
	}
	for _, c := range [][3]int{{20, 10, 0}, {28, 31, 0}, {0, 10, 1}, {31, 32, 1}} {
		if ValidateRule(c[0], c[1], c[2]) == nil {
			t.Fatalf("accepted invalid %v", c)
		}
	}
	for _, c := range []struct {
		v    string
		want []string
	}{{"100", []string{"33.33", "33.33", "33.34"}}, {"10", []string{"3.33", "3.33", "3.34"}}, {"0", []string{"0", "0", "0"}}} {
		v, e := Spread(decimal.RequireFromString(c.v), 3, false)
		if e != nil {
			t.Fatal(e)
		}
		for i, x := range v {
			if !x.Equal(decimal.RequireFromString(c.want[i])) {
				t.Fatal(v)
			}
		}
	}
	if _, e := Spread(decimal.RequireFromString("0.02"), 3, true); e == nil {
		t.Fatal("zero principal term accepted")
	}
}

func TestSpreadPrincipalRemainder(t *testing.T) {
	for _, remainder := range []string{"first", "last", ""} {
		for n := 1; n <= 120; n++ {
			total := decimal.RequireFromString("9999999999.99")
			parts, err := SpreadPrincipal(total, n, remainder)
			if err != nil {
				t.Fatal(err)
			}
			sum := decimal.Zero
			base := total.Div(decimal.NewFromInt(int64(n))).Truncate(2)
			index := n - 1
			if remainder == "first" {
				index = 0
			}
			for i, part := range parts {
				if !ValidAmount(part, false) || (i != index && !part.Equal(base)) {
					t.Fatalf("invalid term %d of %d with %q: %s", i, n, remainder, part)
				}
				sum = sum.Add(part)
			}
			if !sum.Equal(total) {
				t.Fatalf("lost cents with %d terms and %q: %s", n, remainder, sum)
			}
		}
		if _, err := SpreadPrincipal(decimal.RequireFromString("0.02"), 3, remainder); err == nil {
			t.Fatal("zero principal term accepted")
		}
	}
	if _, err := SpreadPrincipal(decimal.NewFromInt(100), 3, "middle"); err == nil {
		t.Fatal("invalid remainder placement accepted")
	}
}
