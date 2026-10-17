package wealthdata

import (
	"context"
	"errors"
	"testing"
)

type fixtureSource struct {
	configured bool
	err        error
	codes      []string
}

func (s *fixtureSource) Configured() bool { return s.configured }
func (s *fixtureSource) Fetch(_ context.Context, product Product, _ string) ([]NAV, error) {
	s.codes = append(s.codes, product.Code)
	return []NAV{{SourceCode: product.SourceCode}}, s.err
}

func TestIndependentSourcesAndControlledRouting(t *testing.T) {
	suyin := &fixtureSource{}
	cmbcwm := &fixtureSource{configured: true}
	sources := map[string]Source{"suyin": suyin, "cmbcwm": cmbcwm}
	router := NewRouter(sources)
	delete(sources, "cmbcwm")
	if !router.Configured() || !router.ConfiguredFor("FBAE68020E") || router.ConfiguredFor("J07241") || router.ConfiguredFor("FBAE68020A") {
		t.Fatal("source configuration not independent")
	}
	rows, err := router.NAVs(context.Background(), "FBAE68020E", "all")
	if err != nil || len(rows) != 1 || len(cmbcwm.codes) != 1 || len(suyin.codes) != 0 {
		t.Fatal(rows, err)
	}
	if _, err = router.NAVs(context.Background(), "J07241", "all"); !errors.Is(err, ErrUnconfigured) {
		t.Fatal(err)
	}
	if _, err = router.NAVs(context.Background(), "FBAE68020A", "all"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	suyin.configured, suyin.err = true, ErrUnavailable
	if _, err = router.NAVs(context.Background(), "J07241", "all"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err = router.NAVs(context.Background(), "FBAE68020E", "all"); err != nil || len(cmbcwm.codes) != 2 {
		t.Fatal("one source failure blocked another", err)
	}
}
