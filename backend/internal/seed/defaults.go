package seed

import (
	"gorm.io/gorm"

	"jizhang/internal/logger"
	"jizhang/internal/model"

	"go.uber.org/zap"
)

// catSpec 两级分类模板。
type catSpec struct {
	Name     string
	Children []string
	// HasChildren=false 表示一级无子分类
	HasChildren bool
}

var defaultAccounts = []struct {
	Name         string
	Type         model.AccountType
	RepaymentDay *int
}{
	{Name: "现金", Type: model.AccountAsset},
	{Name: "微信", Type: model.AccountAsset},
	{Name: "支付宝", Type: model.AccountAsset},
	{Name: "银行卡", Type: model.AccountAsset},
	{Name: "信用卡", Type: model.AccountLiability, RepaymentDay: intPtr(10)},
}

func intPtr(n int) *int { return &n }

var defaultExpenseCats = []catSpec{
	{Name: "餐饮", HasChildren: true, Children: []string{"早餐", "午餐", "晚餐", "外卖", "买菜"}},
	{Name: "交通", HasChildren: true, Children: []string{"公交地铁", "打车", "加油"}},
	{Name: "购物", HasChildren: true, Children: []string{"日用", "服饰", "数码"}},
	{Name: "居家", HasChildren: true, Children: []string{"水电燃气", "房租", "物业"}},
	{Name: "娱乐", HasChildren: true, Children: []string{"电影", "游戏", "旅行"}},
	{Name: "医疗", HasChildren: true, Children: []string{"门诊", "药品"}},
	{Name: "人情", HasChildren: true, Children: []string{"红包", "礼物"}},
	{Name: "其他"},
}

var defaultIncomeCats = []catSpec{
	{Name: "工资", HasChildren: true, Children: []string{"月薪", "奖金"}},
	{Name: "理财", HasChildren: true, Children: []string{"利息", "分红"}},
	{Name: "其他"},
}

// Defaults 幂等创建默认账户与两级分类（账户或分类已存在则跳过）。
func Defaults(db *gorm.DB) error {
	var nAcc int64
	if err := db.Model(&model.Account{}).Count(&nAcc).Error; err != nil {
		return err
	}
	if nAcc == 0 {
		for i, a := range defaultAccounts {
			acc := model.Account{
				Name:         a.Name,
				Type:         a.Type,
				RepaymentDay: a.RepaymentDay,
				Status:       model.StatusActive,
				Sort:         i,
			}
			if err := db.Create(&acc).Error; err != nil {
				return err
			}
		}
		logger.L().Info("seeded default accounts", zap.Int("count", len(defaultAccounts)))
	}

	var nCat int64
	if err := db.Model(&model.Category{}).Count(&nCat).Error; err != nil {
		return err
	}
	if nCat == 0 {
		kindSpecs := map[model.CategoryKind][]catSpec{
			model.KindExpense: defaultExpenseCats,
			model.KindIncome:  defaultIncomeCats,
		}
		for kind, specs := range kindSpecs {
			for i, spec := range specs {
				parent := model.Category{Kind: kind, Name: spec.Name, Sort: i}
				if err := db.Create(&parent).Error; err != nil {
					return err
				}
				if !spec.HasChildren {
					continue
				}
				for j, childName := range spec.Children {
					pid := parent.ID
					child := model.Category{
						ParentID: &pid,
						Kind:     kind,
						Name:     childName,
						Sort:     j,
					}
					if err := db.Create(&child).Error; err != nil {
						return err
					}
				}
			}
		}
		logger.L().Info("seeded default categories")
	}
	return nil
}
