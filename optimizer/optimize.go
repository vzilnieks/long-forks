package main

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"time"

	"github.com/c-bata/goptuna"
	"github.com/c-bata/goptuna/tpe"
	_ "github.com/lib/pq"
)

var (
	PG_DSN           = os.Getenv("PG_DSN")
	EXPERIMENT_ID    = os.Getenv("EXPERIMENT_ID")
	CHAOS_CONTAINERS = []string{
		"docker-bayese-chaos_signer1-1",
		"docker-bayese-chaos_signer2-1",
		"docker-bayese-chaos_signer3-1",
		"docker-bayese-chaos_signer4-1",
		"docker-bayese-chaos_signer5-1",
	}
)

func setCurrentRun(db *sql.DB, experimentID, runID string) error {
	_, err := db.Exec(`
	  INSERT INTO current_run(experiment_id, run_id, updated_at)
	  VALUES ($1, $2, now())
	  ON CONFLICT (experiment_id)
	  DO UPDATE SET run_id = EXCLUDED.run_id, updated_at = now()
	`, experimentID, runID)
	return err
}

func purgeSnapshotsForRun(db *sql.DB, experimentID, runID string) error {
	_, err := db.Exec(`
	  DELETE FROM fork_snapshots
	  WHERE experiment_id = $1 AND run_id = $2
	`, experimentID, runID)
	return err
}

func measureExpectedAndMaxDepth(db *sql.DB, experimentID, runID string) (expected float64, maxDepth float64, err error) {
	q := `
WITH per_anchor_closed AS (
  SELECT anchor_hash,
         MIN(snapshot_at) AS closed_at
  FROM fork_snapshots
  WHERE experiment_id = $1 AND run_id = $2
    AND head_number >= anchor_number + 64
  GROUP BY anchor_hash
),
lifespan AS (
  SELECT s.anchor_hash, MAX(s.depth) AS max_depth_reached
  FROM fork_snapshots s
  JOIN per_anchor_closed pa USING (anchor_hash)
  WHERE s.experiment_id = $1 AND s.run_id = $2
    AND s.snapshot_at <= pa.closed_at
  GROUP BY s.anchor_hash
),
tot AS (SELECT COUNT(*)::numeric AS total FROM lifespan),
vals AS (
  SELECT generate_series(
           0,
           COALESCE((SELECT MAX(max_depth_reached) FROM lifespan), 0)
         ) AS N
),
prob AS (
  SELECT
    v.N AS depth_at_least,
    SUM(CASE WHEN l.max_depth_reached >= v.N THEN 1 ELSE 0 END)::numeric AS anchors_ge,
    (SELECT total FROM tot) AS total
  FROM vals v
  CROSS JOIN lifespan l
  GROUP BY v.N
),
agg AS (
  SELECT
    COALESCE(MAX(l.max_depth_reached), 0) AS max_depth,
    -- total P(depth >= n) on n>=1
    COALESCE(SUM(CASE WHEN depth_at_least >= 1 THEN anchors_ge/NULLIF(total,0) ELSE 0 END), 0) AS expected_depth
  FROM prob
  CROSS JOIN lifespan l
)
SELECT expected_depth, max_depth FROM agg;
`
	row := db.QueryRow(q, experimentID, runID)
	if err := row.Scan(&expected, &maxDepth); err != nil {
		return 0, 0, err
	}
	return expected, maxDepth, nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func dockerComposeUp() error {
	if err := runCmd("docker-compose", "up", "--build", "-d"); err != nil {
		return err
	}
	time.Sleep(5 * time.Second)
	return nil
}

// setup_delay(){ tc qdisc replace dev "$dev" root netem delay 2000ms 1000ms 25%; }
//
//	tc qdisc replace dev eth0 root netem delay 3453ms 506ms 37%
//
// delay 3453ms 506ms 37%
func applyTC(container string, baseMs int, jitterMs int, prob float64) error {
	percent := int(math.Round(prob * 100))
	// delayArg := fmt.Sprintf("delay %dms %dms %d%%", baseMs, jitterMs, percent)
	// delayArg := fmt.Sprintf("delay %dms %dms %d distribution normal", baseMs, jitterMs, percent)
	delayArgs := []string{
		"exec", container, "tc", "qdisc", "replace",
		"dev", "eth0", "root", "netem",
		"delay",
		fmt.Sprintf("%dms", baseMs),
		fmt.Sprintf("%dms", jitterMs),
		fmt.Sprintf("%d%%", percent),
		// "distribution", "normal",
	}
	log.Printf("tc params: %v", delayArgs)
	return runCmd("docker", delayArgs...)
}

// clear_tc()   { tc qdisc del dev "$dev" root 2>/dev/null || true; }
func deleteTC(container string) {
	_ = runCmd("docker", "exec", container, "tc", "qdisc", "del", "dev", "eth0", "root")
}

func measureMaxForkLength(db *sql.DB, experimentID string) (float64, error) {
	var maxLen sql.NullFloat64
	err := db.QueryRow(`
		SELECT MAX(fork_length)
		FROM forks
		WHERE experiment_id = $1
	`, experimentID).Scan(&maxLen)
	if err != nil {
		return 0, err
	}
	if !maxLen.Valid {
		return 0, nil
	}
	return maxLen.Float64, nil
}

func objective(t goptuna.Trial) (float64, error) {

	X, err := t.SuggestInt("X", 1, 120)
	if err != nil {
		return 0, err
	}
	Y, err := t.SuggestInt("Y", 0, 120)
	if err != nil {
		return 0, err
	}
	tcDelayBase, err := t.SuggestInt("tcDelayBase", 100, 3000) // ms
	if err != nil {
		return 0, err
	}
	tcDelayJitter, err := t.SuggestInt("tcDelayJitter", 0, 1000)
	if err != nil {
		return 0, err
	}
	tcProbability, err := t.SuggestFloat("tcProbability", 0.0, 0.4)
	if err != nil {
		return 0, err
	}

	runNumber, err := t.Number()
	if err != nil {
		return 0, err
	}
	runID := fmt.Sprintf("trial-%d-%d", runNumber, time.Now().UnixNano())

	log.Printf("Trial params: X=%d Y=%d base=%dms jitter=%dms prob=%.2f",
		X, Y, tcDelayBase, tcDelayJitter, tcProbability)

	db, err := sql.Open("postgres", PG_DSN)
	if err != nil {
		return 0, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := purgeSnapshotsForRun(db, EXPERIMENT_ID, runID); err != nil {
		log.Printf("[WARN] purge run snapshots: %v", err)
	}
	if err := setCurrentRun(db, EXPERIMENT_ID, runID); err != nil {
		return 0, fmt.Errorf("setCurrentRun: %w", err)
	}

	time.Sleep(10 * time.Second)

	runDuration := 5 * 60 * time.Second
	endAt := time.Now().Add(runDuration)

	for time.Now().Before(endAt) {
		for _, c := range CHAOS_CONTAINERS {
			if err := applyTC(c, tcDelayBase, tcDelayJitter, tcProbability); err != nil {
				log.Printf("[WARN] applyTC(%s): %v", c, err)
			}
		}
		time.Sleep(time.Duration(X) * time.Second)

		for _, c := range CHAOS_CONTAINERS {
			deleteTC(c)
		}
		time.Sleep(time.Duration(Y) * time.Second)
	}

	time.Sleep(2 * time.Second)

	expDepth, maxDepth, err := measureExpectedAndMaxDepth(db, EXPERIMENT_ID, runID)
	if err != nil {
		return 0, fmt.Errorf("measure metric: %w", err)
	}
	log.Printf("Trial result: expected_depth=%.4f, max_depth=%.0f", expDepth, maxDepth)
	return expDepth, nil

	// db, err := sql.Open("postgres", PG_DSN)
	// if err != nil {
	// 	return 0, fmt.Errorf("open db: %w", err)
	// }
	// defer db.Close()
	// value, err := measureMaxForkLength(db, EXPERIMENT_ID)
	// if err != nil {
	// 	return 0, fmt.Errorf("measure metric: %w", err)
	// }
	// log.Printf("Trial result (max fork length): %.4f", value)
	// return value, nil
}

func main() {
	if PG_DSN == "" || EXPERIMENT_ID == "" {
		log.Fatal("Please set PG_DSN and EXPERIMENT_ID env vars (see .env.example).")
	}

	study, err := goptuna.CreateStudy(
		"long_forks_study",
		goptuna.StudyOptionSampler(tpe.NewSampler()))
	if err != nil {
		log.Fatalf("CreateStudy: %v", err)
	}

	// if err := study.SetDirection(goptuna.StudyDirectionMaximize); err != nil {
	// 	log.Fatalf("SetDirection: %v", err)
	// }

	nTrials := 10

	if err := dockerComposeUp(); err != nil {
		log.Fatalf("compose up: %v", err)
	}

	log.Printf("Start optimization: %d trials", nTrials)
	if err := study.Optimize(objective, nTrials); err != nil {
		log.Fatalf("Optimize: %v", err)
	}

	v, _ := study.GetBestValue()
	p, _ := study.GetBestParams()
	log.Printf("Best value=%f (x1=%v, x2=%v)", v, p["x1"], p["x2"])

	// best, err := study.GetBestTrial()
	// if err != nil {
	// 	log.Fatalf("GetBestTrial: %v", err)
	// }
	// log.Printf("Best trial: value=%.4f params=%v", best.Value, best.Params)
}
