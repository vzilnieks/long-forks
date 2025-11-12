package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DistRow struct {
	Depth    int     `json:"depth"`
	Branches int     `json:"branches"`
	Percent  float64 `json:"percent"`
}

type DistEvent struct {
	Ts       time.Time `json:"ts"`
	Kind     string    `json:"kind"` // always "distribution"
	MaxDepth int       `json:"max_depth"`
	Rows     []DistRow `json:"rows"`
}

func mustEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

const qDistribution = `
WITH RECURSIVE
head AS (
  SELECT hash FROM blocks ORDER BY number DESC, hash DESC LIMIT 1
),
canonical AS (
  SELECT b.hash, b.parent_hash, b.number
  FROM blocks b WHERE b.hash = (SELECT hash FROM head)
  UNION ALL
  SELECT p.hash, p.parent_hash, p.number
  FROM blocks p JOIN canonical c ON p.hash = c.parent_hash
),
noncanon AS (
  SELECT b.hash, b.parent_hash, b.number
  FROM blocks b LEFT JOIN canonical c ON c.hash = b.hash
  WHERE c.hash IS NULL
),
paths AS (
  SELECT n.hash, n.parent_hash, n.number,
         n.parent_hash AS anchor,
         1::int AS depth
  FROM noncanon n
  WHERE n.parent_hash IN (SELECT hash FROM canonical)
  UNION ALL
  SELECT n.hash, n.parent_hash, n.number,
         p.anchor,
         p.depth + 1
  FROM noncanon n
  JOIN paths p ON n.parent_hash = p.hash
),
depth_per_anchor AS (
  SELECT anchor, MAX(depth) AS depth
  FROM paths
  GROUP BY anchor
)
SELECT depth,
       COUNT(*) AS branches,
       ROUND(100 * COUNT(*) / SUM(COUNT(*)) OVER (), 8) AS percent
FROM depth_per_anchor
GROUP BY depth
ORDER BY depth DESC;
`

const qSnapshot = `
WITH RECURSIVE
params(k) AS (VALUES (64)),
head AS (
  SELECT b.hash, b.number
  FROM blocks b
  ORDER BY b.number DESC, b.hash DESC
  LIMIT 1
),
canonical AS (
  SELECT b.hash, b.parent_hash, b.number
  FROM blocks b
  WHERE b.hash = (SELECT hash FROM head)
  UNION ALL
  SELECT p.hash, p.parent_hash, p.number
  FROM blocks p
  JOIN canonical c ON p.hash = c.parent_hash
),
finalized_height AS (
  SELECT GREATEST((SELECT number FROM head) - (SELECT k FROM params), 0) AS h
),
anchors AS (
  SELECT hash, number
  FROM canonical
  WHERE number <= (SELECT h FROM finalized_height)
),
noncanon AS (
  SELECT b.hash, b.parent_hash, b.number
  FROM blocks b
  LEFT JOIN canonical c ON c.hash = b.hash
  WHERE c.hash IS NULL
),
paths AS (
  SELECT n.hash, n.parent_hash, n.number,
         n.parent_hash AS anchor,
         1::int AS depth
  FROM noncanon n
  WHERE n.parent_hash IN (SELECT hash FROM anchors)
  UNION ALL
  SELECT n.hash, n.parent_hash, n.number,
         p.anchor,
         p.depth + 1
  FROM noncanon n
  JOIN paths p ON n.parent_hash = p.hash
),
depth_per_anchor AS (
  SELECT anchor, MAX(depth) AS depth
  FROM paths
  GROUP BY anchor
)
INSERT INTO fork_snapshots(snapshot_at, head_hash, head_number, anchor_hash, anchor_number, depth)
SELECT
  now() AS snapshot_at,
  (SELECT hash   FROM head)  AS head_hash,
  (SELECT number FROM head)  AS head_number,
  a.hash  AS anchor_hash,
  a.number AS anchor_number,
  COALESCE(d.depth, 0) AS depth
FROM anchors a
LEFT JOIN depth_per_anchor d ON d.anchor = a.hash;
`

func main() {
	dsn := mustEnv("PG_DSN", "postgresql://clique:secret@postgres:5432/clique")
	poll := mustEnv("POLL_SEC", "2")
	interval, err := time.ParseDuration(poll + "s")
	if err != nil || interval <= 0 {
		interval = 2 * time.Second
	}

	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("pg connect: %v", err)
	}
	defer db.Close()

	// var prev []DistRow

	for {
		// 	rows, maxDepth, err := fetchDistribution(ctx, db)
		// 	if err != nil {
		// 		log.Printf("fetchDistribution: %v", err)
		// 	} else if !reflect.DeepEqual(rows, prev) {
		// 		ev := DistEvent{Ts: time.Now().UTC(), Kind: "distribution", MaxDepth: maxDepth, Rows: rows}
		// 		b, _ := json.Marshal(ev)
		// 		log.Println(string(b))
		// 		prev = rows
		// 	}
		_, err = db.Exec(ctx, qSnapshot)
		if err != nil {
			log.Printf("SQL error: %v", err)
		}
		time.Sleep(interval)
	}
}

func fetchDistribution(ctx context.Context, db *pgxpool.Pool) ([]DistRow, int, error) {
	rs, err := db.Query(ctx, qDistribution)
	if err != nil {
		return nil, 0, err
	}
	defer rs.Close()

	rows := make([]DistRow, 0, 8)
	maxDepth := 0
	for rs.Next() {
		var r DistRow
		if err := rs.Scan(&r.Depth, &r.Branches, &r.Percent); err != nil {
			return nil, 0, err
		}
		rows = append(rows, r)
		if r.Depth > maxDepth {
			maxDepth = r.Depth
		}
	}
	return rows, maxDepth, rs.Err()
}
