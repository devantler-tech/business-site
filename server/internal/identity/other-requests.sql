ALTER TABLE portal_requests
    DROP CONSTRAINT portal_requests_product_check,
    ADD CONSTRAINT portal_requests_product_check
        CHECK (product IN ('website','app','service','other'));
