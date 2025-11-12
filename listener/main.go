package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
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
	nodeID := mustEnv("NODE_ID")
	wsURL := mustEnv("WS_URL")
	brokers := mustEnv("KAFKA_BROKERS")
	topic := os.Getenv("TOPIC")
	if topic == "" {
		topic = "block-headers"
	}

	writer := &kafka.Writer{Addr: kafka.TCP(brokers), Topic: topic, RequiredAcks: kafka.RequireAll, BatchTimeout: 50 * time.Millisecond}
	defer writer.Close()

	for {
		if err := runOnce(nodeID, wsURL, writer); err != nil {
			log.Printf("[%s] reconnect after error: %v", nodeID, err)
			time.Sleep(2 * time.Second)
		}
	}
}

func runOnce(nodeID, wsURL string, writer *kafka.Writer) error {
	ctx := context.Background()
	cli, err := ethclient.Dial(wsURL)
	if err != nil {
		return err
	}
	defer cli.Close()

	heads := make(chan *types.Header, 64)
	sub, err := cli.SubscribeNewHead(ctx, heads)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	log.Printf("[%s] subscribed to %s", nodeID, wsURL)

	for {
		select {
		case err := <-sub.Err():
			return err
		case h := <-heads:
			log.Printf("[%s] new block : %v", nodeID, h.Hash().Hex())
			msg := HeaderMsg{
				NodeID:     nodeID,
				Hash:       h.Hash().Hex(),
				ParentHash: h.ParentHash.Hex(),
				Number:     h.Number.Uint64(),
				Difficulty: h.Difficulty.String(),
				SeenAt:     time.Now().UTC(),
			}
			b, _ := json.Marshal(msg)
			err := writer.WriteMessages(ctx, kafka.Message{Key: []byte(msg.Hash), Value: b})
			if err != nil {
				log.Printf("[%s] kafka write err: %v", nodeID, err)
			}
		}
	}
}
