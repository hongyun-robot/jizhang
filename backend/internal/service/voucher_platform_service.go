package service

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

func normalizeVoucherPlatform(name string, required bool) (string, error) {
	name = strings.TrimSpace(name)
	if (required && name == "") || utf8.RuneCountInString(name) > 128 {
		return "", fmt.Errorf("%w：平台 / 商家名称须为 1 至 128 字", ErrVoucherInvalid)
	}
	return name, nil
}

func (s *VoucherService) ListPlatforms() ([]model.VoucherPlatform, error) {
	return repository.NewVoucherPlatformRepo(s.db).List()
}

func (s *VoucherService) CreatePlatform(name string) (*model.VoucherPlatform, error) {
	name, err := normalizeVoucherPlatform(name, true)
	if err != nil {
		return nil, err
	}
	return repository.NewVoucherPlatformRepo(s.db).Ensure(name)
}

func ensureVoucherPlatform(tx *gorm.DB, name string) error {
	if name == "" {
		return nil
	}
	_, err := repository.NewVoucherPlatformRepo(tx).Ensure(name)
	return err
}
