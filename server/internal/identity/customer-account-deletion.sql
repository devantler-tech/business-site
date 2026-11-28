CREATE TABLE portal_customer_deletions (
    issuer text NOT NULL,
    subject text NOT NULL,
    provider_organization text NOT NULL CHECK (length(provider_organization) BETWEEN 1 AND 200),
    provider_created_at text NOT NULL CHECK (length(provider_created_at) BETWEEN 20 AND 35),
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    confirmed_at timestamptz CHECK (confirmed_at >= requested_at),
    PRIMARY KEY (issuer,subject)
);
