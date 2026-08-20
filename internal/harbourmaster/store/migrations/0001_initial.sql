-- The M1 schema (EDR-0042).
--
-- Tenant-partitioned from the first migration (EDR-0025), which means more than
-- a column: tenant_id leads every index that matters, is present in every
-- unique constraint, and every foreign key is composite so a row cannot
-- reference a parent belonging to another tenant.
--
-- M1 has no identity, so tenant_id comes from one configured development
-- tenant. It is never a request field. M4 takes it from the authenticated
-- principal, which is a change of source and not of schema — which is the
-- whole reason the column is here now.

CREATE TABLE tenants (
    id   text PRIMARY KEY,
    name text NOT NULL
);

CREATE TABLE requests (
    tenant_id  text        NOT NULL REFERENCES tenants (id),
    -- The `req_…` reference an operator pastes into chat (EDR-0038). An
    -- identifier, not a capability: resolving one still requires entitlement,
    -- which is M4's to enforce.
    reference  text        NOT NULL,
    statement  text        NOT NULL,
    target     text        NOT NULL,
    role       text        NOT NULL,
    -- A bare string at M1. M4 replaces it with an authenticated principal.
    submitter  text        NOT NULL,
    reason     text        NOT NULL,
    -- All seven of EDR-0038's states. M1 produces three; the rest are here
    -- because a forward-only schema makes widening a constrained column an
    -- unnecessary migration, and the vocabulary is not M1's to shorten.
    state      text        NOT NULL
        CHECK (state IN ('pending', 'verifying', 'approved', 'refused',
                         'expired', 'executed', 'indeterminate')),
    -- The caller's key, so a retried submission is one request. Two different
    -- keys are two requests even for identical statements.
    idempotency_key text   NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    -- Bounded. Without a bound an oversized reference fails as
    -- "index row size exceeds btree maximum", an internal storage error
    -- surfacing as a user-facing one; and an empty statement or reason inserts
    -- cleanly while the wire contract calls both required.
    CHECK (length(reference) BETWEEN 1 AND 128),
    CHECK (length(statement) BETWEEN 1 AND 100000),
    CHECK (length(reason) BETWEEN 1 AND 4000),
    CHECK (length(target) BETWEEN 1 AND 128),
    CHECK (length(role) BETWEEN 1 AND 128),
    CHECK (length(submitter) BETWEEN 1 AND 320),
    CHECK (length(idempotency_key) BETWEEN 1 AND 128),

    PRIMARY KEY (tenant_id, reference),
    UNIQUE (tenant_id, idempotency_key)
);

CREATE TABLE approvals (
    tenant_id text        NOT NULL,
    reference text        NOT NULL,
    -- Numbered from 1. M1 has no escalation chain, so it is always 1 — the
    -- column exists because a stage-less approvals table is the flat shape
    -- EDR-0030 exists to refuse, and adding it later is a table rebuild.
    -- int32, matching the proto's int32. A uint32 there would overflow here.
    stage     integer     NOT NULL CHECK (stage >= 1),
    approver  text        NOT NULL,
    at        timestamptz NOT NULL DEFAULT now(),

    -- One approval per approver per stage: the same person approving the same
    -- stage twice is one approval, which is what makes Approve naturally
    -- idempotent.
    PRIMARY KEY (tenant_id, reference, stage, approver),
    FOREIGN KEY (tenant_id, reference) REFERENCES requests (tenant_id, reference)
);

CREATE TABLE executions (
    tenant_id     text        NOT NULL,
    reference     text        NOT NULL,
    -- One attempt. Repeating a nonce returns the stored outcome rather than
    -- recording a second attempt. This is a uniqueness constraint and NOT
    -- EDR-0011's fence: that ledger is Pilot-local, claimed before the
    -- statement runs, and carries an incarnation. None of that is here.
    nonce         text        NOT NULL,
    at            timestamptz NOT NULL DEFAULT now(),
    -- Decided by EDR-0042. EDR-0011 names three tokens, closes no set and
    -- settles no success token, so this could not be borrowed. `in_progress`
    -- is deliberately absent: a control-plane report is written when an
    -- attempt ends.
    outcome       text        NOT NULL
        CHECK (outcome IN ('committed', 'rolled_back',
                           'aborted_not_applied', 'indeterminate')),
    -- NULL exactly when the outcome is indeterminate. A NOT NULL column would
    -- force a number where the truth is that nobody knows whether the statement
    -- committed, and inventing one is the direction EDR-0011 exists to refuse.
    rows_affected bigint,

    CHECK ((outcome = 'indeterminate') = (rows_affected IS NULL)),
    CHECK (rows_affected IS NULL OR rows_affected >= 0),

    PRIMARY KEY (tenant_id, reference, nonce),
    FOREIGN KEY (tenant_id, reference) REFERENCES requests (tenant_id, reference)
);

-- The work queue reads pending and approved TOGETHER, newest first (EDR-0038),
-- which a (tenant_id, state, created_at) index does not serve: ordering within
-- each state is not ordering across them, so the queue reads every matching row
-- and sorts it. Measured on 20 000 rows. A partial index over the two states the
-- queue asks for gives the order directly.
CREATE INDEX requests_queue ON requests (tenant_id, created_at DESC)
    WHERE state IN ('pending', 'approved');

-- And the general case, for looking at one state at a time.
CREATE INDEX requests_by_state ON requests (tenant_id, state, created_at DESC);

-- The migrator owns these tables and the runtime role does not, which is the
-- separation the implementation plan requires and EDR-0012 will narrow further
-- at M6. Without these grants the two-role split cannot start: measured, the
-- runtime role could not read schema_migrations, so Verify failed before the
-- Harbourmaster served anything.
--
-- The role name is fixed here rather than configured because a migration cannot
-- take a parameter. A deployment using a different name grants by hand, and M4's
-- local-development story is where that stops being a footnote.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'marque_runtime') THEN
        GRANT SELECT ON public.schema_migrations TO marque_runtime;
        GRANT SELECT, INSERT, UPDATE ON tenants, requests, approvals, executions TO marque_runtime;
    END IF;
END
$$;

-- One tenant, so M1's six steps can run. EDR-0025 has tenant_id come from the
-- authenticated principal at M4; until then it is configuration, and a schema
-- that requires a tenant while creating none cannot execute its own milestone —
-- the first Submit fails on the foreign key.
INSERT INTO tenants (id, name) VALUES ('development', 'Development');
