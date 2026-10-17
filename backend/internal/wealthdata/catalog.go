package wealthdata

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed products.json
var productManifest []byte

//go:generate go run ../../cmd/wealth-catalog -sync-collector

type Product struct {
	Issuer            string `json:"issuer"`
	Adapter           string `json:"adapter"`
	Code              string `json:"code"`
	Name              string `json:"name"`
	RegistrationCode  string `json:"registration_code"`
	Currency          string `json:"currency"`
	ValuationBasis    string `json:"valuation_basis"`
	PageURL           string `json:"page_url"`
	SourceCode        string `json:"source_code"`
	InceptionDate     string `json:"inception_date"`
	OrderPolicy       string `json:"order_policy"`
	DefaultCashPolicy string `json:"default_cash_policy,omitempty"`
}

type Registry struct{ products map[string]Product }

var identifier = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func ParseCatalog(raw []byte) (*Registry, error) {
	var manifest struct {
		Version  int       `json:"schema_version"`
		Products []Product `json:"products"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 2<<20 || decoder.Decode(&manifest) != nil || decoder.Decode(new(any)) != io.EOF || manifest.Version != 1 || len(manifest.Products) == 0 {
		return nil, fmt.Errorf("invalid wealth product manifest")
	}
	registry := &Registry{products: map[string]Product{}}
	for _, product := range manifest.Products {
		for _, field := range []struct {
			value string
			limit int
		}{{product.Code, 32}, {product.Issuer, 32}, {product.Adapter, 32}, {product.RegistrationCode, 64}, {product.SourceCode, 64}} {
			if len(field.value) > field.limit || !identifier.MatchString(field.value) {
				return nil, fmt.Errorf("invalid wealth identity: %s", product.Code)
			}
		}
		page, err := url.Parse(product.PageURL)
		date, dateErr := time.Parse("2006-01-02", product.InceptionDate)
		if err != nil || page.Scheme != "https" || page.Hostname() == "" || page.User != nil || page.Fragment != "" || dateErr != nil || date.Format("2006-01-02") != product.InceptionDate || product.Code != strings.ToUpper(product.Code) || utf8.RuneCountInString(product.Name) < 1 || utf8.RuneCountInString(product.Name) > 128 || product.Currency != "CNY" || product.ValuationBasis != "cny_wealth_unit_nav" || (product.OrderPolicy != "confirmed_facts" && product.OrderPolicy != "zhoulibao_20250402") {
			return nil, fmt.Errorf("invalid wealth product: %s", product.Code)
		}
		if _, duplicate := registry.products[product.Code]; duplicate {
			return nil, fmt.Errorf("ambiguous wealth share code: %s", product.Code)
		}
		if product.DefaultCashPolicy != "" && product.DefaultCashPolicy != "unit_nav" && product.DefaultCashPolicy != "yulibao_20250715" {
			return nil, fmt.Errorf("invalid cash policy: %s", product.Code)
		}
		registry.products[product.Code] = product
	}
	return registry, nil
}

var defaultCatalog = func() *Registry {
	registry, err := ParseCatalog(productManifest)
	if err != nil {
		panic(err)
	}
	return registry
}()

func (r *Registry) Lookup(code string) (Product, bool) {
	product, ok := r.products[code]
	return product, ok
}

func (r *Registry) Products() []Product {
	rows := make([]Product, 0, len(r.products))
	for _, product := range r.products {
		rows = append(rows, product)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Code < rows[j].Code })
	return rows
}

func Catalog(code string) (Product, bool) { return defaultCatalog.Lookup(code) }
func Products() []Product                 { return defaultCatalog.Products() }
func Manifest() []byte                    { return append([]byte(nil), productManifest...) }

// ProductInsertSQL creates public metadata in the closed admission state.
// Operators still review a numbered migration and qualify the product.
func ProductInsertSQL(product Product) string {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	fields := []string{product.Issuer, product.RegistrationCode, product.Code, product.Name, product.Currency, product.ValuationBasis, product.InceptionDate, product.Adapter, product.SourceCode}
	for i := range fields {
		fields[i] = quote(fields[i])
	}
	return "INSERT INTO wealth_products (issuer,registration_code,code,name,currency,valuation_basis,inception_date,source_adapter,source_code)\nVALUES (" + strings.Join(fields, ",") + ");\n"
}
