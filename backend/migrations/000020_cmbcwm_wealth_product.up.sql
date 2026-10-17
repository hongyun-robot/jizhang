-- Public identity only. Explicit user authorization on 2026-10-16 admits this
-- share class; it does not assert that multi-day qualification has passed.
INSERT INTO wealth_products
 (issuer, registration_code, code, name, currency, valuation_basis,
  inception_date, source_adapter, source_code, admission_status, evidence_version)
VALUES
 ('cmbcwm', 'Z7007923000092', 'FBAE68020E', '民生理财贵竹固收增利天天盈9号理财产品E',
  'CNY', 'cny_wealth_unit_nav', '2023-06-20', 'cmbcwm', 'FBAE68020E',
  'enabled', 'user-approved-cmbcwm-http-20261016');
