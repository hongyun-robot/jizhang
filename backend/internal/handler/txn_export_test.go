package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/service"
)

type exportReaderFunc func(context.Context, repository.TxnFilter, int) (*repository.TxnExportSnapshot, error)

func (f exportReaderFunc) ReadXLSX(c context.Context, q repository.TxnFilter, n int) (*repository.TxnExportSnapshot, error) {
	return f(c, q, n)
}

func exportRequest(s *service.TransactionService, query string) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("currentUser", &model.User{ID: 1, Role: model.RoleViewer}) })
	r.GET("/export", NewTxn(s).Export)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/export"+query, nil))
	return w
}

func TestTransactionExportFormatsAndErrors(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.Account{}, &model.Category{}, &model.Transaction{}); err != nil {
		t.Fatal(err)
	}
	s := service.NewTransactionService(service.TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: repository.NewAccountRepo(db), Cats: repository.NewCategoryRepo(db)})
	for _, query := range []string{"", "?format=", "?format=csv"} {
		w := exportRequest(s, query)
		if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "\xef\xbb\xbf日期,类型,") || !strings.Contains(w.Header().Get("Content-Disposition"), ".csv") {
			t.Fatal(query, w.Code, w.Body.String())
		}
		if strings.Count(strings.TrimSpace(strings.TrimPrefix(w.Body.String(), "\xef\xbb\xbf")), ",") != 12 {
			t.Fatal("CSV schema changed")
		}
	}
	w := exportRequest(s, "?format=xlsx")
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "PK") || w.Header().Get("Content-Type") != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	for _, tc := range []struct {
		query   string
		err     error
		over    bool
		status  int
		message string
	}{{"?format=pdf", nil, false, 400, "格式无效"}, {"?format=xlsx", nil, true, 413, "100,000"}, {"?format=xlsx", context.DeadlineExceeded, false, 504, "超时"}, {"?format=xlsx", service.ErrExportAssociation, false, 500, "关联资料"}, {"?format=xlsx", errors.New("db unavailable"), false, 500, ""}} {
		svc := service.NewTransactionService(service.TxnDeps{Exports: exportReaderFunc(func(context.Context, repository.TxnFilter, int) (*repository.TxnExportSnapshot, error) {
			if tc.query == "?format=pdf" {
				t.Fatal("invalid format read database")
			}
			if tc.over {
				return &repository.TxnExportSnapshot{Rows: make([]model.Transaction, service.XLSXMaxRows+1)}, nil
			}
			return nil, tc.err
		})})
		w := exportRequest(svc, tc.query)
		if w.Code != tc.status || w.Header().Get("Content-Disposition") != "" || !strings.Contains(w.Header().Get("Content-Type"), "application/json") || !strings.Contains(w.Body.String(), tc.message) {
			t.Fatal(tc, w.Code, w.Header(), w.Body.String())
		}
	}
}
