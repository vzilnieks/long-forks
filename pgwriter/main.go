package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"
)

type HeaderMsg struct {
	NodeID     string    `json:"node_id"`
	Hash       string    `json:"hash"`
	ParentHash string    `json:"parent_hash"`
	Number     uint64    `json:"number"`
	Difficulty string    `json:"difficulty"`
	SeenAt     time.Time `json:"seen_at"`
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("env %s required", k)
	}
	return v
}

func main() {
	brokers := mustEnv("KAFKA_BROKERS")
	topic := mustEnv("TOPIC")
	group := mustEnv("GROUP_ID")
	pgdsn := mustEnv("PG_DSN")

	ctx := context.Background()

	db, err := pgxpool.New(ctx, pgdsn)
	if err != nil {
		log.Fatalf("pg connect: %v", err)
	}
	defer db.Close()

	r := kafka.NewReader(kafka.ReaderConfig{Brokers: []string{brokers}, Topic: topic, GroupID: group, MinBytes: 1, MaxBytes: 10e6})
	defer r.Close()

	log.Printf("pgwriter started: topic=%s group=%s", topic, group)

	for {
		m, err := r.ReadMessage(ctx)
		if err != nil {
			log.Fatalf("kafka read: %v", err)
		}
		var msg HeaderMsg
		if err := json.Unmarshal(m.Value, &msg); err != nil {
			log.Printf("json err: %v", err)
			continue
		}

		tx, err := db.Begin(ctx)
		if err != nil {
			log.Printf("tx begin: %v", err)
			continue
		}

		_, err = tx.Exec(ctx, `
            INSERT INTO blocks(hash, parent_hash, number, difficulty, first_seen)
            VALUES ($1,$2,$3,$4, $5)
            ON CONFLICT (hash) DO NOTHING
        `, msg.Hash, msg.ParentHash, msg.Number, msg.Difficulty, msg.SeenAt)
		if err != nil {
			log.Printf("insert block: %v", err)
			_ = tx.Rollback(ctx)
			continue
		}

		_, err = tx.Exec(ctx, `
            INSERT INTO observations(node_id, hash, first_seen)
            VALUES ($1,$2,$3)
            ON CONFLICT (node_id, hash) DO NOTHING
        `, msg.NodeID, msg.Hash, msg.SeenAt)
		if err != nil {
			log.Printf("insert obs: %v", err)
			_ = tx.Rollback(ctx)
			continue
		}

		if err := tx.Commit(ctx); err != nil {
			log.Printf("commit err: %v", err)
		}
	}
}
