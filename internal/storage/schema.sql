CREATE TABLE IF NOT EXISTS sessions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at_unix INTEGER NOT NULL,
    ended_at_unix   INTEGER,
    template_hash   TEXT    NOT NULL,
    template        TEXT    NOT NULL DEFAULT '',
    position        INTEGER NOT NULL,
    api             TEXT    NOT NULL,
    address_type    TEXT    NOT NULL,
    n_addresses     INTEGER NOT NULL,
    rate            REAL    NOT NULL DEFAULT 0,
    wordlist_path   TEXT    NOT NULL DEFAULT '',
    workers         INTEGER NOT NULL DEFAULT 1,
    positions_spec  TEXT    NOT NULL DEFAULT '',
    last_word_index INTEGER NOT NULL DEFAULT -1,
    status          TEXT    NOT NULL DEFAULT 'running'
);

CREATE INDEX IF NOT EXISTS idx_sessions_resume
    ON sessions(template_hash, position, api, address_type, n_addresses, status);

CREATE TABLE IF NOT EXISTS attempts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id      INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    word_index      INTEGER NOT NULL,
    mnemonic_hash   TEXT    NOT NULL,
    addresses_json  TEXT    NOT NULL,
    balance_sats    INTEGER NOT NULL DEFAULT 0,
    valid_checksum  INTEGER NOT NULL,
    error           TEXT,
    duration_ms     INTEGER NOT NULL,
    checked_at_unix INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_attempts_session ON attempts(session_id, word_index);
CREATE INDEX IF NOT EXISTS idx_attempts_balance ON attempts(balance_sats) WHERE balance_sats > 0;
