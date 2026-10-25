package service

import (
	"os"
	"path/filepath"
	"testing"

	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func TestPersonalBudgetMySQLMigration(t *testing.T) {
	for _, scenario := range []string{"empty", "history", "edited", "new-user"} {
		t.Run(scenario, func(t *testing.T) {
			db, migrate := catalogMySQL(t)
			files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
			mustOK(t, err)
			for _, file := range files {
				name := filepath.Base(file)
				if name >= "000012" && name < "000022" {
					migrate(name)
				}
			}
			if scenario != "empty" {
				mustOK(t, db.Exec("INSERT INTO users(id,username,password_hash,role,status) VALUES(1,'budget-admin','hash','admin','active'),(2,'budget-viewer','hash','viewer','active')").Error)
				mustOK(t, db.Exec("INSERT INTO categories(id,owner_id,kind,name) VALUES(1001,1,'expense','同名餐饮'),(1002,2,'expense','同名餐饮')").Error)
				// Preserve both historical periods and the last effective legacy total.
				mustOK(t, db.Exec("INSERT INTO budgets(period_key,category_id,amount) VALUES('2026-10',NULL,7000),('2026-10',NULL,8000),('2026-10',1001,3000),('2026-10',1002,4000),('2026-09',NULL,6000)").Error)
			}
			migrate("000022_personal_budgets.up.sql")
			if scenario != "empty" {
				for _, owner := range []int64{1, 2} {
					rows, err := repository.NewBudgetRepo(db).ForOwner(owner).List("2026-10")
					mustOK(t, err)
					if len(rows) != 2 {
						t.Fatalf("owner %d: %+v", owner, rows)
					}
					for _, row := range rows {
						if row.OwnerID != owner || (row.CategoryID == nil && !row.Amount.Equal(dec("8000"))) || (row.CategoryID != nil && *row.CategoryID != 1000+owner) {
							t.Fatal("migration leaked or changed budgets", row)
						}
					}
				}
				if err := db.Exec("INSERT INTO budgets(owner_id,period_key,amount) VALUES(1,'2026-10',1)").Error; err == nil {
					t.Fatal("duplicate personal total accepted")
				}
			}
			if scenario == "edited" {
				mustOK(t, repository.NewBudgetRepo(db).ForOwner(2).Replace("2026-10", []model.Budget{{Amount: dec("9000")}}))
			} else if scenario == "new-user" {
				mustOK(t, db.Exec("INSERT INTO users(username,password_hash,role,status) VALUES('new-budget-user','hash','member','active')").Error)
			}
			if scenario == "edited" || scenario == "new-user" {
				down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000022_personal_budgets.down.sql"))
				mustOK(t, err)
				if err := db.Exec(string(down)).Error; err == nil {
					t.Fatal("unsafe rollback accepted")
				}
				if !db.Migrator().HasColumn(&model.Budget{}, "owner_id") {
					t.Fatal("rollback changed schema before guard")
				}
				return
			}
			migrate("000022_personal_budgets.down.sql")
			var count int64
			mustOK(t, db.Table("budgets").Count(&count).Error)
			expected := int64(0)
			if scenario == "history" {
				expected = 5
			}
			if count != expected || db.Migrator().HasColumn("budgets", "owner_id") {
				t.Fatal("rollback did not restore original budgets", count)
			}
		})
	}
}
