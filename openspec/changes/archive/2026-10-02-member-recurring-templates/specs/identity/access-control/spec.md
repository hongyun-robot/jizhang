# Spec Delta

## MODIFIED Requirements

### Requirement: 设置类操作仅限管理员
账期结算日配置、封账与解封、分类管理、账户管理、预算配置、用户与角色管理 SHALL 仅管理员可操作；其他角色执行时 MUST 返回 403。周期记账模板管理不适用本条，按 `recurring-entries` 能力中「按创建者归属隔离」的规则执行。

#### Scenario: 记账员修改账期设置
- **WHEN** 记账员调用修改结算日的接口
- **THEN** 返回 403，设置不发生变化

#### Scenario: 普通成员修改分类
- **WHEN** 普通成员尝试新增或删除分类
- **THEN** 返回 403
