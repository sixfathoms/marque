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
    name text NOT NULL,

    -- Bounded because it is a primary key, and every text column below is
    -- bounded for the same reason: a btree index row above about 2704 bytes is
    -- refused by PostgreSQL at INSERT, which turns an over-long identifier into
    -- a 500 where a 400 belongs. btrim, so a value of nothing but spaces is not
    -- a legal identifier — length() alone accepts three spaces as a name.
    CHECK (length(btrim(id)) BETWEEN 1 AND 128),
    CHECK (length(btrim(name)) BETWEEN 1 AND 256)
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
    CHECK (length(btrim(reference)) BETWEEN 1 AND 128),
    CHECK (length(btrim(statement)) BETWEEN 1 AND 100000),
    CHECK (length(btrim(reason)) BETWEEN 1 AND 4000),
    CHECK (length(btrim(target)) BETWEEN 1 AND 128),
    CHECK (length(btrim(role)) BETWEEN 1 AND 128),
    CHECK (length(btrim(submitter)) BETWEEN 1 AND 320),
    CHECK (length(btrim(idempotency_key)) BETWEEN 1 AND 128),

    PRIMARY KEY (tenant_id, reference),
    UNIQUE (tenant_id, idempotency_key)
);

CREATE TABLE approvals (
    tenant_id text        NOT NULL,
    reference text        NOT NULL,
    -- Numbered from 1. M1 has no escalation chain, so it is always 1 — the
    -- column exists because a stage-less approvals table is the flat shape
    -- EDR-0030 exists to refuse, and adding it later is a table rebuild.
    -- integer, which is int32. The proto's field is uint32, so the two do NOT
    -- have the same range and an earlier comment here claiming they did was
    -- backwards: a wire value above 2^31-1 does not fit this column. The upper
    -- bound is what makes that unreachable, and 64 is already absurd for an
    -- escalation chain — EDR-0019's are a few stages long. The service rejects
    -- an out-of-range stage before it reaches the database.
    stage     integer     NOT NULL CHECK (stage BETWEEN 1 AND 64),
    approver  text        NOT NULL CHECK (length(btrim(approver)) BETWEEN 1 AND 320),
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
    nonce         text        NOT NULL CHECK (length(btrim(nonce)) BETWEEN 1 AND 128),
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
-- local-development story is where that stops being a footnote (issue #40).
--
-- GRANTED UNCONDITIONALLY. An earlier version guarded this with
-- `IF EXISTS (SELECT 1 FROM pg_roles ...)` so a database with no such role could
-- still migrate. That produced two different privilege states from one digest:
-- migrate before creating the role and the grants never happen, the migration
-- reports success, no re-run repairs it because 0001 is applied, and the failure
-- surfaces later as a permission error at Verify that does not name its cause.
-- Two reviewers reproduced it. `role "marque_runtime" does not exist` at
-- migration time is the better failure, so creating the role is a documented
-- step before the first migration rather than something the schema papers over.
DO $$
BEGIN
    -- EDR-0012: the migrator owns these tables and the runtime role must not,
    -- because an owner can grant itself anything and the withheld grant would
    -- be decorative. Ownership follows whoever runs the CREATE TABLE, so the
    -- only moment this is checkable is here.
    IF current_user = 'marque_runtime' THEN
        RAISE EXCEPTION 'the migrator must not run as marque_runtime: it would own these tables, and EDR-0012 requires the runtime role not to';
    END IF;
END
$$;

GRANT SELECT ON public.schema_migrations TO marque_runtime;
GRANT SELECT, INSERT, UPDATE ON tenants, requests, approvals, executions TO marque_runtime;

-- One tenant, so M1's six steps can run. EDR-0025 has tenant_id come from the
-- authenticated principal at M4; until then it is configuration, and a schema
-- that requires a tenant while creating none cannot execute its own milestone —
-- the first Submit fails on the foreign key.
INSERT INTO tenants (id, name) VALUES ('development', 'Development');
