package service

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"jizhang/internal/model"
)

// fakeUserStore 内存版 UserManageStore。
type fakeUserStore struct {
	users map[int64]*model.User
	next  int64
}

func newFakeUserStore(users ...*model.User) *fakeUserStore {
	f := &fakeUserStore{users: map[int64]*model.User{}, next: 1}
	for _, u := range users {
		f.users[u.ID] = u
	}
	return f
}

func (f *fakeUserStore) GetByUsername(username string) (*model.User, error) {
	for _, u := range f.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, nil
}

func (f *fakeUserStore) GetByID(id int64) (*model.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, nil
}

func (f *fakeUserStore) Create(u *model.User) error {
	f.next++
	u.ID = f.next
	f.users[u.ID] = u
	return nil
}

func (f *fakeUserStore) Update(u *model.User) error {
	f.users[u.ID] = u
	return nil
}

func (f *fakeUserStore) List() ([]model.User, error) {
	out := make([]model.User, 0, len(f.users))
	for _, u := range f.users {
		out = append(out, *u)
	}
	return out, nil
}

func (f *fakeUserStore) CountActiveAdmins() (int64, error) {
	var n int64
	for _, u := range f.users {
		if u.Role == model.RoleAdmin && u.Status == model.StatusActive {
			n++
		}
	}
	return n, nil
}

type fakeSessions struct{ revoked []int64 }

func (f *fakeSessions) RevokeAll(_ context.Context, uid int64) { f.revoked = append(f.revoked, uid) }

func admin(id int64, name string) *model.User {
	return &model.User{ID: id, Username: name, Role: model.RoleAdmin, Status: model.StatusActive}
}

// TestLastAdminProtected 最后一个启用管理员不可停用/降级。
func TestLastAdminProtected(t *testing.T) {
	store := newFakeUserStore(admin(1, "boss"))
	sessions := &fakeSessions{}
	svc := NewUserService(store, sessions)

	if _, err := svc.SetRole(1, model.RoleMember); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("降级最后一个管理员 = %v, want ErrLastAdmin", err)
	}
	if _, err := svc.SetStatus(1, model.StatusDisabled); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("停用最后一个管理员 = %v, want ErrLastAdmin", err)
	}

	// 两个管理员时允许降级其中一个
	store2 := newFakeUserStore(admin(1, "boss"), admin(2, "boss2"))
	svc2 := NewUserService(store2, &fakeSessions{})
	if _, err := svc2.SetRole(2, model.RoleViewer); err != nil {
		t.Errorf("第二个管理员降级 = %v, want nil", err)
	}
}

// TestResetPasswordRevokesSessions 重置密码必须吊销全部会话。
func TestResetPasswordRevokesSessions(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("oldpass1"), bcrypt.DefaultCost)
	store := newFakeUserStore(&model.User{
		ID: 5, Username: "u5", Role: model.RoleMember, Status: model.StatusActive,
		PasswordHash: string(hash),
	})
	sessions := &fakeSessions{}
	svc := NewUserService(store, sessions)

	if err := svc.ResetPassword(5, "newpass1"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if len(sessions.revoked) != 1 || sessions.revoked[0] != 5 {
		t.Errorf("revoked = %v, want [5]", sessions.revoked)
	}
	if bcrypt.CompareHashAndPassword([]byte(store.users[5].PasswordHash), []byte("newpass1")) != nil {
		t.Error("密码未更新")
	}
}

// TestCreateUserValidation 建号校验：角色合法、密码长度、用户名唯一。
func TestCreateUserValidation(t *testing.T) {
	store := newFakeUserStore(admin(1, "boss"))
	svc := NewUserService(store, &fakeSessions{})

	if _, err := svc.Create("a", "123456", model.Role("hacker")); !errors.Is(err, ErrInvalidRole) {
		t.Errorf("非法角色 = %v, want ErrInvalidRole", err)
	}
	if _, err := svc.Create("a", "123", model.RoleViewer); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("短密码 = %v, want ErrWeakPassword", err)
	}
	if _, err := svc.Create("boss", "123456", model.RoleViewer); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("重复用户名 = %v, want ErrUsernameTaken", err)
	}
	if _, err := svc.Create("fresh", "123456", model.RoleRecorder); err != nil {
		t.Errorf("正常建号 = %v, want nil", err)
	}
}
