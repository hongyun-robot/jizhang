-- Export personal merchant positions first: removing this column loses custom ordering.
-- Names, ownership, identifiers and all financial history are retained.
ALTER TABLE voucher_platforms DROP INDEX idx_voucher_platform_owner_sort, DROP COLUMN sort;
DROP TABLE catalog_order_requests;
