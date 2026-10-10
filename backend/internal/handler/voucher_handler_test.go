package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"jizhang/internal/middleware"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"jizhang/internal/service"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVoucherHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	if err := db.AutoMigrate(&model.Setting{}, &model.Account{}, &model.Category{}, &model.Transaction{}, &model.PeriodClosing{}, &model.Voucher{}, &model.VoucherEvent{}, &model.VoucherPlatform{}); err != nil {
		t.Fatal(err)
	}
	acc := model.Account{Name: "现金", Type: model.AccountAsset, Status: model.StatusActive}
	cat := model.Category{Name: "餐饮", Kind: model.KindExpense}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	txn := service.NewTransactionService(service.TxnDeps{Txns: repository.NewTransactionRepo(db), Accounts: repository.NewAccountRepo(db), Cats: repository.NewCategoryRepo(db), Closings: repository.NewClosingRepo(db), Periods: service.NewPeriodService(repository.NewSettingRepo(db))})
	vs := service.NewVoucherService(db, txn)
	h := NewVoucher(vs)
	th := NewTxn(txn)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		role := model.Role(c.GetHeader("X-Test-Role"))
		if role == "" {
			role = model.RoleAdmin
		}
		id := int64(1)
		if role == model.RoleMember {
			id = 2
		}
		c.Set("currentUser", &model.User{ID: id, Role: role, Status: model.StatusActive})
		c.Next()
	})
	v := r.Group("/vouchers")
	v.GET("", h.List)
	v.GET("/:id", h.Detail)
	w := v.Group("", middleware.RequireRoles(model.RoleAdmin, model.RoleRecorder, model.RoleMember))
	w.POST("", h.Create)
	w.POST("/ensure", h.Ensure)
	w.PUT("/:id", h.Update)
	w.DELETE("/:id", h.Delete)
	w.POST("/:id/redeem", h.Operate(model.VoucherRedeem))
	w.POST("/:id/refund", h.Operate(model.VoucherRefund))
	w.POST("/:id/writeoff", h.Operate(model.VoucherWriteoff))
	w.POST("/:id/events/:eventId/undo", h.Undo)
	r.POST("/transactions", th.Create)
	r.GET("/voucher-platforms", h.ListPlatforms)
	r.POST("/voucher-platforms", h.CreatePlatform)
	r.PUT("/transactions/:id", th.Update)
	r.DELETE("/transactions/:id", th.Delete)
	request := func(method, path, role string, body any, want int) json.RawMessage {
		t.Helper()
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Role", role)
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s: status %d want %d: %s", method, path, res.Code, want, res.Body.String())
		}
		var envelope struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	input := map[string]any{"name": "HTTP 餐券", "paid_amount": "100", "purchase_date": "2026-10-01", "account_id": acc.ID, "category_id": cat.ID, "total_units": 5, "expires_on": "2026-11-30", "request_key": uuid.NewString()}
	for _, role := range []string{"admin", "recorder", "member", "viewer"} {
		request("GET", "/voucher-platforms", role, nil, 200)
		var row model.VoucherPlatform
		if err := json.Unmarshal(request("POST", "/voucher-platforms", role, map[string]string{"name": "  全员共享  "}, 200), &row); err != nil || row.Name != "全员共享" {
			t.Fatal(row, err)
		}
	}
	for _, invalid := range []any{map[string]string{"name": "   "}, map[string]string{"name": "名称", "unknown": "no"}, map[string]int{"name": 123}} {
		request("POST", "/voucher-platforms", "member", invalid, 400)
	}
	var detail service.VoucherDetail
	if err := json.Unmarshal(request("POST", "/vouchers", "admin", input, 200), &detail); err != nil {
		t.Fatal(err)
	}
	if !detail.AutoRefundEnabled || !detail.RemainingAmount.Equal(detail.PaidAmount) {
		t.Fatal(detail)
	}
	id := detail.ID
	path := fmt.Sprintf("/vouchers/%d", id)
	for _, write := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/vouchers", input}, {"PUT", path, input}, {"DELETE", path, nil}, {"POST", path + "/redeem", map[string]any{}}, {"POST", path + "/refund", map[string]any{}}, {"POST", path + "/writeoff", map[string]any{}}, {"POST", path + "/events/1/undo", nil}, {"POST", "/vouchers/ensure", nil},
	} {
		request(write.method, write.path, "viewer", write.body, 403)
	}
	request("GET", "/vouchers", "viewer", nil, 200)
	request("GET", path, "viewer", nil, 200)
	request("GET", "/vouchers/99999", "admin", nil, 404)
	op := map[string]any{"entry_date": "2026-11-01", "units": 2, "request_key": uuid.NewString()}
	if err := json.Unmarshal(request("POST", path+"/redeem", "admin", op, 200), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.UsedUnits != 2 || detail.RemainingUnits != 3 {
		t.Fatal(detail)
	}
	request("POST", path+"/redeem", "admin", map[string]any{"entry_date": "2026-12-01", "units": 1, "request_key": uuid.NewString()}, 400)
	request("POST", path+"/refund", "admin", map[string]any{"entry_date": "2026-11-02", "amount": "100", "request_key": uuid.NewString()}, 400)
	if err := json.Unmarshal(request("POST", path+"/refund", "admin", map[string]any{"entry_date": "2026-11-02", "request_key": uuid.NewString()}, 200), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.RefundedAmount.String() != "60" || detail.RemainingUnits != 0 {
		t.Fatal(detail)
	}
	event := detail.Events[len(detail.Events)-1]
	request("POST", fmt.Sprintf("%s/events/%d/undo", path, event.ID), "member", nil, 403)
	request("POST", fmt.Sprintf("%s/events/%d/undo", path, event.ID), "recorder", nil, 200)
	var purchase model.Transaction
	if err := db.Where("purpose = ?", model.PurposeVoucherPurchase).First(&purchase).Error; err != nil {
		t.Fatal(err)
	}
	request("DELETE", fmt.Sprintf("/transactions/%d", purchase.ID), "admin", nil, 409)
	plain := map[string]any{"kind": "expense", "amount": "1", "account_id": acc.ID, "category_id": cat.ID, "entry_date": "2026-11-03", "purpose": "voucher_redemption"}
	request("POST", "/transactions", "admin", plain, 400)
	delete(plain, "purpose")
	plain["platform"] = "  流水商家  "
	var regular model.Transaction
	if err := json.Unmarshal(request("POST", "/transactions", "member", plain, 200), &regular); err != nil || regular.Platform != "流水商家" {
		t.Fatal("HTTP did not preserve platform", regular, err)
	}
	plain["platform"] = strings.Repeat("商", 129)
	request("POST", "/transactions", "member", plain, 400)
	delete(plain, "platform")
	request("PUT", fmt.Sprintf("/transactions/%d", purchase.ID), "admin", plain, 409)
	today := time.Now().In(time.FixedZone("Beijing", 8*3600))
	input["purchase_date"] = today.AddDate(0, 0, -2).Format("2006-01-02")
	input["expires_on"] = today.AddDate(0, 0, -1).Format("2006-01-02")
	input["request_key"] = uuid.NewString()
	request("POST", "/vouchers", "admin", input, 200)
	var ensure service.VoucherEnsureResult
	if err := json.Unmarshal(request("POST", "/vouchers/ensure", "admin", nil, 200), &ensure); err != nil {
		t.Fatal(err)
	}
	if ensure.Generated < 1 {
		t.Fatal(ensure)
	}
}
