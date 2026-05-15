PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;

CREATE TABLE IF NOT EXISTS samples (
    ts INTEGER NOT NULL,
    probe TEXT NOT NULL,
    metric TEXT NOT NULL,
    value REAL NOT NULL,
    labels TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_samples_probe_ts ON samples(probe, ts);

CREATE TABLE IF NOT EXISTS rollups_5m (
    bucket_ts INTEGER NOT NULL,
    probe TEXT NOT NULL,
    metric TEXT NOT NULL,
    min_val REAL NOT NULL,
    max_val REAL NOT NULL,
    avg_val REAL NOT NULL,
    p95_val REAL NOT NULL,
    count INTEGER NOT NULL,
    PRIMARY KEY (bucket_ts, probe, metric)
);

CREATE TABLE IF NOT EXISTS baselines (
    probe TEXT NOT NULL,
    metric TEXT NOT NULL,
    hour_of_week INTEGER NOT NULL,
    p50 REAL NOT NULL,
    p95 REAL NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (probe, metric, hour_of_week)
);

CREATE TABLE IF NOT EXISTS states (
    dimension TEXT PRIMARY KEY,
    state TEXT NOT NULL,
    since_ts INTEGER NOT NULL,
    detail TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS incidents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    start_ts INTEGER NOT NULL,
    end_ts INTEGER,
    overall_state TEXT NOT NULL,
    detail_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_incidents_start ON incidents(start_ts DESC);

CREATE TABLE IF NOT EXISTS meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
