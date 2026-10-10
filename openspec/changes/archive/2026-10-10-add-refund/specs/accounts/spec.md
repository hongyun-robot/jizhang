# Spec Delta

## MODIFIED Requirements

### Requirement: 流水关联账户
支出与收入流水 MUST 关联一个账户，转账流水 MUST 关联转出与转入两个账户，退款流水 MUST 关联一个账户并使该账户余额增加；任何余额变动 MUST 由关联流水驱动。

#### Scenario: 记一笔支出
- **WHEN** 用户以微信账户记录 30 元支出
- **THEN** 微信账户余额减少 30 元

#### Scenario: 记一笔退款
- **WHEN** 用户以支付宝账户记录 100 元退款
- **THEN** 支付宝账户余额增加 100 元
