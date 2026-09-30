-- heimdalld schema. Addresses are stored lowercase (0x-prefixed); amounts are decimal strings of base units.

CREATE TABLE users (
    address            text PRIMARY KEY,
    default_policy     jsonb,
    telegram_chat_id   bigint,
    telegram_username  text NOT NULL DEFAULT '',
    email              text NOT NULL DEFAULT '',
    email_verified     boolean NOT NULL DEFAULT false,
    created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE telegram_links (
    code        text PRIMARY KEY,
    address     text NOT NULL,
    expires_at  timestamptz NOT NULL
);

CREATE TABLE email_verifications (
    address     text PRIMARY KEY,
    email       text NOT NULL,
    code_hash   text NOT NULL,
    expires_at  timestamptz NOT NULL,
    attempts    int NOT NULL DEFAULT 0
);

CREATE TABLE auth_nonces (
    nonce       text PRIMARY KEY,
    address     text NOT NULL,
    message     text NOT NULL,
    expires_at  timestamptz NOT NULL
);

CREATE TABLE guards (
    address        text PRIMARY KEY,
    owner          text NOT NULL,
    created_block  bigint NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX guards_owner_idx ON guards (owner);

CREATE TABLE policies (
    guard       text NOT NULL,
    target_id   text NOT NULL,
    policy      jsonb NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (guard, target_id)
);

CREATE TABLE signal_snapshots (
    id         bigserial PRIMARY KEY,
    target_id  text NOT NULL,
    block      bigint NOT NULL,
    time       timestamptz NOT NULL,
    severity   text NOT NULL,
    signals    jsonb NOT NULL
);
CREATE INDEX signal_snapshots_target_idx ON signal_snapshots (target_id, id DESC);

-- One row per severity change with the inputs and a human-readable reason (specs W3).
CREATE TABLE incidents (
    id             bigserial PRIMARY KEY,
    target_id      text NOT NULL,
    block          bigint NOT NULL,
    time           timestamptz NOT NULL,
    from_severity  text NOT NULL,
    severity       text NOT NULL,
    reason         text NOT NULL,
    inputs         jsonb NOT NULL
);
CREATE INDEX incidents_target_idx ON incidents (target_id, id DESC);

CREATE TABLE exits (
    id                        bigserial PRIMARY KEY,
    guard                     text NOT NULL,
    owner                     text NOT NULL,
    target_id                 text NOT NULL,
    trigger                   text NOT NULL,
    reason                    text NOT NULL,
    reason_hash               text NOT NULL,
    severity                  text NOT NULL,
    status                    text NOT NULL,
    episode                   text NOT NULL DEFAULT '',
    decided_at                timestamptz NOT NULL,
    decision_to_broadcast_ms  int,
    tip_cap_usd               double precision NOT NULL DEFAULT 0,
    tip_note                  text NOT NULL DEFAULT '',
    total_out                 text NOT NULL DEFAULT '0',
    start_block               bigint NOT NULL,
    end_block                 bigint
);
CREATE INDEX exits_guard_target_idx ON exits (guard, target_id, id DESC);
CREATE INDEX exits_owner_idx ON exits (owner, id DESC);

CREATE TABLE exit_txs (
    id                      bigserial PRIMARY KEY,
    exit_id                 bigint NOT NULL REFERENCES exits(id) ON DELETE CASCADE,
    hash                    text NOT NULL UNIQUE,
    nonce                   bigint NOT NULL DEFAULT 0,
    sent_block              bigint NOT NULL DEFAULT 0,
    block                   bigint,
    max_priority_fee_wei    text NOT NULL DEFAULT '0',
    max_fee_wei             text NOT NULL DEFAULT '0',
    gas_limit               bigint NOT NULL DEFAULT 0,
    tip_usd                 double precision NOT NULL DEFAULT 0,
    amount_out              text NOT NULL DEFAULT '0',
    burned                  text NOT NULL DEFAULT '0',
    remaining               text NOT NULL DEFAULT '0',
    result                  text NOT NULL DEFAULT 'pending'
);
CREATE INDEX exit_txs_exit_idx ON exit_txs (exit_id, id);

CREATE TABLE events (
    id         bigserial PRIMARY KEY,
    time       timestamptz NOT NULL DEFAULT now(),
    block      bigint NOT NULL DEFAULT 0,
    kind       text NOT NULL,
    target_id  text NOT NULL DEFAULT '',
    guard      text NOT NULL DEFAULT '',
    owner      text NOT NULL DEFAULT '',
    severity   text NOT NULL DEFAULT '',
    message    text NOT NULL,
    tx_hash    text NOT NULL DEFAULT '',
    exit_id    bigint
);
CREATE INDEX events_owner_idx ON events (owner, id DESC);
CREATE INDEX events_target_idx ON events (target_id, id DESC);

CREATE TABLE notifications (
    id          bigserial PRIMARY KEY,
    owner       text NOT NULL,
    channel     text NOT NULL,
    kind        text NOT NULL,
    subject     text NOT NULL,
    body        text NOT NULL,
    status      text NOT NULL,
    attempts    int NOT NULL DEFAULT 0,
    error       text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    sent_at     timestamptz
);

CREATE TABLE backtest_runs (
    id           bigserial PRIMARY KEY,
    incident_id  text NOT NULL,
    target       text NOT NULL,
    from_block   bigint NOT NULL,
    to_block     bigint NOT NULL,
    out_path     text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sim_runs (
    id                   bigserial PRIMARY KEY,
    scenario             text NOT NULL,
    target_id            text NOT NULL,
    status               text NOT NULL,
    start_block          bigint NOT NULL,
    drain_finished_block bigint,
    end_block            bigint,
    started_at           timestamptz NOT NULL DEFAULT now()
);

-- Block cursor, simulator snapshot id/block, ...
CREATE TABLE meta (
    key    text PRIMARY KEY,
    value  text NOT NULL
);
