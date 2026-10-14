-- Append-only operator decisions; drafts themselves remain immutable/private.
CREATE TABLE portal_proposal_visibility (
    request_id text NOT NULL,
    client_id text NOT NULL,
    event integer NOT NULL CHECK (event BETWEEN 1 AND 100),
    expected_event integer NOT NULL CHECK (expected_event = event - 1),
    proposal_version integer NOT NULL,
    expected_proposal integer NOT NULL CHECK (expected_proposal BETWEEN proposal_version AND 50),
    action text NOT NULL CHECK (action IN ('share', 'withdraw')),
    submission_key text NOT NULL CHECK (submission_key ~ '^[A-Z2-7]{26}$'),
    operator_issuer text NOT NULL,
    operator_subject text NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (request_id, event),
    UNIQUE (request_id, submission_key),
    UNIQUE (request_id, proposal_version, action),
    FOREIGN KEY (request_id, client_id) REFERENCES portal_requests(id, client_id),
    FOREIGN KEY (request_id, proposal_version) REFERENCES portal_proposals(request_id, version)
);
