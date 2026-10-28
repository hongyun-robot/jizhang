package handler

import (
	"bytes"
	"encoding/json"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"jizhang/internal/middleware"
	"jizhang/internal/model"
	"jizhang/internal/response"
	"jizhang/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type authTestUsers struct{ user *model.User }

func (s authTestUsers) GetByUsername(name string) (*model.User, error) {
	if name == s.user.Username {
		return s.user, nil
	}
	return nil, nil
}
func (s authTestUsers) GetByID(id int64) (*model.User, error) {
	if id == s.user.ID {
		return s.user, nil
	}
	return nil, nil
}
func TestAuthHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewAuthService(authTestUsers{&model.User{ID: 1, Username: "member", PasswordHash: string(hash), Role: model.RoleMember, Status: model.StatusActive}}, rdb, "handler-test", time.Hour, time.Hour, true, "/jizhang/api/v1/auth")
	h := NewAuth(svc)
	router := gin.New()
	router.POST("/login", h.Login)
	router.POST("/refresh", h.Refresh)
	router.POST("/logout", h.Logout)
	router.POST("/me", middleware.Auth(svc), h.Me)
	calls := 0
	router.POST("/protected", middleware.Auth(svc), func(c *gin.Context) { calls++; response.OK(c, "written") })
	request := func(path string, input interface{}, cookie *http.Cookie, bearer, ua string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(input)
		req := httptest.NewRequest("POST", path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", ua)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	login := func(ua string) (tokenResp, *http.Cookie) {
		w := request("/login", map[string]interface{}{"username": "member", "password": "password123", "protocol_version": 2}, nil, "", ua)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var body struct{ Data tokenResp }
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal(cookies)
		}
		if !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/jizhang/api/v1/auth" {
			t.Fatal("unsafe cookie", cookies[0])
		}
		if strings.Contains(w.Body.String(), cookies[0].Value) {
			t.Fatal("refresh leaked")
		}
		return body.Data, cookies[0]
	}
	old, cookie := login("iPhone Mobile")
	legacy := httptest.NewRecorder()
	router.ServeHTTP(legacy, httptest.NewRequest("POST", "/refresh", nil))
	if legacy.Code != 401 || len(legacy.Result().Cookies()) != 0 {
		t.Fatal(legacy.Code, legacy.Body.String())
	}
	me := request("/me", nil, nil, old.AccessToken, "")
	if me.Code != 200 || !strings.Contains(me.Body.String(), old.SessionID) || !strings.Contains(me.Body.String(), `"device_type":"phone"`) {
		t.Fatal(me.Body.String())
	}
	desktop, desktopCookie := login("Windows NT")
	for _, input := range []map[string]interface{}{
		{"username": "member", "password": "password123"},
		{"username": "member", "password": "password123", "protocol_version": 2, "max_touch_points": 65},
		{"username": "member", "password": "password123", "protocol_version": 2, "max_touch_points": -1},
	} {
		w := request("/login", input, nil, "", "iPhone")
		if w.Code != 400 || len(w.Result().Cookies()) != 0 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request("/protected", nil, nil, old.AccessToken, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	next := request("/refresh", map[string]interface{}{"protocol_version": 2, "session_id": old.SessionID}, cookie, "", "")
	if next.Code != 200 {
		t.Fatal(next.Body.String())
	}
	newCookie := next.Result().Cookies()[0]
	conflict := request("/refresh", map[string]interface{}{"protocol_version": 2, "session_id": old.SessionID}, cookie, "", "")
	var failure response.Body
	json.Unmarshal(conflict.Body.Bytes(), &failure)
	if conflict.Code != 409 || failure.Code != response.CodeRefreshConflict || len(conflict.Result().Cookies()) != 0 {
		t.Fatal(conflict.Code, conflict.Body.String())
	}
	replacement, replacementCookie := login("iPhone Mobile")
	stale := request("/logout", map[string]interface{}{"protocol_version": 2, "session_id": old.SessionID}, newCookie, old.AccessToken, "")
	if stale.Code != 401 || len(stale.Result().Cookies()) != 0 {
		t.Fatal("stale logout changed cookie", stale.Body.String())
	}
	if w := request("/protected", nil, nil, replacement.AccessToken, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	mixed := request("/logout", map[string]interface{}{"protocol_version": 2, "session_id": replacement.SessionID}, desktopCookie, replacement.AccessToken, "")
	if mixed.Code != 401 || len(mixed.Result().Cookies()) != 0 {
		t.Fatal("mixed identity accepted")
	}
	missing := request("/logout", map[string]interface{}{"protocol_version": 2, "session_id": replacement.SessionID}, nil, "", "")
	if missing.Code != 401 {
		t.Fatal(missing.Body.String())
	}
	done := request("/logout", map[string]interface{}{"protocol_version": 2, "session_id": replacement.SessionID}, replacementCookie, replacement.AccessToken, "")
	if done.Code != 200 || done.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal(done.Body.String())
	}
	for _, token := range []string{desktop.AccessToken, replacement.AccessToken} {
		w := request("/protected", nil, nil, token, "")
		if w.Code != 401 {
			t.Fatal(w.Body.String())
		}
	}
	r, _ := login("Windows NT")
	before := calls
	rdb.Close()
	w := request("/protected", nil, nil, r.AccessToken, "")
	if w.Code != 503 || calls != before {
		t.Fatal("unverified handler executed", w.Code)
	}
	w = request("/refresh", map[string]interface{}{"protocol_version": 2}, desktopCookie, "", "")
	if w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Existing state remains when transport access fails.
	if !mr.Exists("auth:v2:1") {
		t.Fatal("service failure deleted state")
	}
}
