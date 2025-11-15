
CREATE TABLE IF NOT EXISTS blocks (
  hash         TEXT PRIMARY KEY,
  parent_hash  TEXT NOT NULL,
  number       BIGINT NOT NULL,
  difficulty   BIGINT,
  author       TEXT,
  first_seen   TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_blocks_parent ON blocks(parent_hash);
CREATE INDEX IF NOT EXISTS idx_blocks_number ON blocks(number);

CREATE TABLE IF NOT EXISTS observations (
  node_id     TEXT NOT NULL,
  hash        TEXT NOT NULL,
  first_seen  TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (node_id, hash),
  FOREIGN KEY (hash) REFERENCES blocks(hash) ON DELETE CASCADE
);

CREATE MATERIALIZED VIEW IF NOT EXISTS tips AS
SELECT b.hash, b.number
FROM blocks b LEFT JOIN blocks c ON c.parent_hash = b.hash
WHERE c.hash IS NULL;

CREATE TABLE IF NOT EXISTS fork_snapshots (
  snapshot_at     timestamptz NOT NULL,
  head_hash       text        NOT NULL,
  head_number     bigint      NOT NULL,
  anchor_hash     text        NOT NULL,
  anchor_number   bigint      NOT NULL,
  depth           int         NOT NULL,
  experiment_id   text,
  run_id          text,
  PRIMARY KEY (snapshot_at, anchor_hash)
);

CREATE INDEX IF NOT EXISTS fork_snapshots_anchor_idx
  ON fork_snapshots(anchor_hash);

CREATE INDEX IF NOT EXISTS fork_snapshots_headnum_idx
  ON fork_snapshots(head_number);

-- ALTER TABLE IF EXISTS fork_snapshots
--   ADD COLUMN IF NOT EXISTS experiment_id text,
--   ADD COLUMN IF NOT EXISTS run_id text;

CREATE INDEX IF NOT EXISTS idx_fork_snapshots_exp_run
  ON fork_snapshots (experiment_id, run_id);

CREATE TABLE IF NOT EXISTS current_run (
  experiment_id text PRIMARY KEY,
  run_id text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

