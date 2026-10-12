CREATE TABLE portal_requests (
    id text PRIMARY KEY CHECK (id ~ '^[A-Z2-7]{26}$'),
    client_id text NOT NULL,
    submission_key text NOT NULL CHECK (submission_key ~ '^[A-Z2-7]{26}$'),
    product text NOT NULL CHECK (product IN ('website','app','service')),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
    brief text NOT NULL CHECK (char_length(brief) BETWEEN 1 AND 1200),
    submitted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (client_id, submission_key)
);
CREATE INDEX portal_requests_client_time ON portal_requests(client_id, submitted_at DESC, id);
