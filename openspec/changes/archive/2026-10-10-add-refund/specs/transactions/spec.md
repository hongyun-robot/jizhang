# Spec Delta

## RENAMED Requirements

- FROM: `### Requirement: 记账三态`
  TO: `### Requirement: 记账四态`

## MODIFIED Requirements

### Requirement: 记账四态
系统 SHALL 支持记录支出、收入、转账、退款四类流水；流水包含金额（CNY，两位小数）、归属日期、账户、分类（转账无分类，退款无关联时分类可留空）、备注与创建人，创建人 MUST 记为当前登录用户。退款金额 MUST 为正数，方向语义为冲减支出，MUST NOT 计入收入。

#### Scenario: 记录支出
- **WHEN** 用户填写金额 25.50、归属日期为今天、账户"现金"、分类"餐饮 → 早餐"并保存
- **THEN** 流水创建成功并出现在列表首位，创建人为当前用户，现金账户余额相应减少

#### Scenario: 记录收入
- **WHEN** 用户记录一笔工资收入并关联银行卡
- **THEN** 该笔计入收入统计，银行卡余额相应增加

#### Scenario: 记录退款
- **WHEN** 用户记录一笔 50 元退款并关联微信账户
- **THEN** 流水以"退款"类型保存，微信余额增加 50，收入统计不变

### Requirement: CSV 导出
系统 SHALL 支持将当前筛选结果导出为 CSV 文件，编码 MUST 为 UTF-8 BOM，导出行数 MUST 与当前筛选命中数一致；退款行 SHALL 带可辨识的退款标记（类型为退款、金额为正数）。

#### Scenario: 导出筛选结果
- **WHEN** 用户在选定区间与筛选条件后点击导出
- **THEN** 下载 UTF-8 BOM 编码的 CSV 文件，Excel 打开中文不乱码，内容与列表一致

#### Scenario: 导出含退款的流水
- **WHEN** 用户导出的筛选结果中包含退款流水
- **THEN** 该行类型列显示退款标记，金额为正数
