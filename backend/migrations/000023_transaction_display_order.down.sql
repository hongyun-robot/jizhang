-- Only personal display preferences are discarded; financial history is retained.
DELETE FROM catalog_order_requests WHERE scope='transactions';
DROP TABLE transaction_display_orders;
