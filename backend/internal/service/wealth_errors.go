package service

import "errors"

var (
	ErrWealthInvalid      = errors.New("理财份额、金额、净值或日期无效")
	ErrWealthCode         = errors.New("理财产品未接入或尚未完成准入验证")
	ErrWealthMissing      = errors.New("理财账户、持仓或事件不存在")
	ErrWealthConflict     = errors.New("理财请求已使用、版本过期或记录已有依赖，请刷新核对")
	ErrWealthHistory      = errors.New("理财历史存在后续依赖或不合法的后继持仓")
	ErrWealthPrice        = errors.New("理财净值尚不可用")
	ErrWealthAutomatic    = errors.New("该日期已有自动净值，人工补值仅供审计")
	ErrWealthUnregistered = errors.New("只能为本人已登记产品补录净值")
	ErrWealthManaged      = errors.New("理财资金记录请到独立理财详情统一处理")
)
