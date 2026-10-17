package wealthdata

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewConfiguredShareRoutesWithoutBusinessBranches(t *testing.T) {
	var manifest struct {
		Version  int       `json:"schema_version"`
		Products []Product `json:"products"`
	}
	if err := json.Unmarshal(Manifest(), &manifest); err != nil {
		t.Fatal(err)
	}
	product := manifest.Products[1]
	product.Code, product.SourceCode, product.Name = "TESTSHARE2", "TESTSHARE2", "同银行新增份额测试"
	product.PageURL = "https://www.cmbcwm.com.cn/cmbcwm/grlc/cpxq/index.html?code=TESTSHARE2"
	manifest.Products = append(manifest.Products, product)
	raw, _ := json.Marshal(manifest)
	registry, err := ParseCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	source := &fixtureSource{configured: true}
	router := NewRouterWithCatalog(registry, map[string]Source{"cmbcwm": source})
	rows, err := router.NAVs(context.Background(), product.Code, "all")
	if err != nil || len(rows) != 1 || rows[0].SourceCode != product.SourceCode || len(source.codes) != 1 || source.codes[0] != product.Code {
		t.Fatal(rows, err)
	}
	if _, leaked := Catalog(product.Code); leaked {
		t.Fatal("custom catalog mutated global controlled identities")
	}
	manifest.Products = append(manifest.Products, product)
	raw, _ = json.Marshal(manifest)
	if _, err = ParseCatalog(raw); err == nil {
		t.Fatal("ambiguous code admitted")
	}
}

func TestCollectorManifestMatchesCanonicalCatalog(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "collector", "products.json"))
	if os.IsNotExist(err) {
		t.Skip("collector source not included in this build context")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, Manifest()) {
		t.Fatal("run go generate ./internal/wealthdata to refresh collector catalog")
	}
}
