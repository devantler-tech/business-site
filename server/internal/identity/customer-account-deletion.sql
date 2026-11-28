CREATE TABLE portal_customer_deletions (
    issuer text NOT NULL,
    subject text NOT NULL,
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    confirmed_at timestamptz CHECK (confirmed_at >= requested_at),
    PRIMARY KEY (issuer,subject)
);
