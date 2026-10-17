// wealth-catalog exports controlled product metadata; it never writes a database.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"jizhang/internal/wealthdata"
)

func main() {
	code := flag.String("code", "", "specific share code")
	sql := flag.Bool("sql", false, "print pending-admission public INSERT for a reviewed migration")
	syncCollector := flag.Bool("sync-collector", false, "refresh generated collector/products.json")
	flag.Parse()
	if *syncCollector {
		path, err := os.Getwd()
		if err != nil {
			fail(err)
		}
		for {
			if _, err = os.Stat(filepath.Join(path, "backend", "go.mod")); err == nil {
				if err = os.WriteFile(filepath.Join(path, "collector", "products.json"), wealthdata.Manifest(), 0644); err != nil {
					fail(err)
				}
				return
			}
			parent := filepath.Dir(path)
			if parent == path {
				fail(fmt.Errorf("run this command inside the repository"))
			}
			path = parent
		}
	}
	if *sql {
		product, ok := wealthdata.Catalog(*code)
		if !ok {
			fail(fmt.Errorf("unknown share code"))
		}
		fmt.Print(wealthdata.ProductInsertSQL(product))
		return
	}
	if err := json.NewEncoder(os.Stdout).Encode(wealthdata.Products()); err != nil {
		fail(err)
	}
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
