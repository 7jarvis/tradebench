// Package events relays the transactional outbox to Kafka.
//
// Order state changes are written to the outbox table in the same database
// transaction as the change itself, so an event is never lost and never
// published for a change that rolled back. The relay publishes pending rows
// and marks them published: delivery is at-least-once, consumers must be
// idempotent (the order id plus event type is a natural dedup key).
package events

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/lib/pq"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Relay struct {
	db       *sql.DB
	client   *kgo.Client
	log      *slog.Logger
	interval time.Duration
	batch    int
}

func NewRelay(db *sql.DB, brokers []string, log *slog.Logger) (*Relay, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.AllowAutoTopicCreation(),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerLinger(0),
	)
	if err != nil {
		return nil, err
	}
	return &Relay{db: db, client: client, log: log, interval: 100 * time.Millisecond, batch: 200}, nil
}

// Run publishes until ctx is canceled.
func (r *Relay) Run(ctx context.Context) {
	defer r.client.Close()
	for {
		n, err := r.publishBatch(ctx)
		if err != nil && ctx.Err() == nil {
			r.log.Warn("outbox relay failed, will retry", "err", err)
		}
		if n == r.batch {
			continue // backlog: keep draining without waiting
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.interval):
		}
	}
}

func (r *Relay) publishBatch(ctx context.Context) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	// SKIP LOCKED lets several replicas relay concurrently without double work.
	rows, err := tx.QueryContext(ctx,
		`SELECT id, topic, key, payload FROM outbox
		  WHERE published_at IS NULL ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`, r.batch)
	if err != nil {
		return 0, err
	}
	var ids []int64
	var records []*kgo.Record
	for rows.Next() {
		var (
			id                int64
			topic, key, value string
		)
		if err := rows.Scan(&id, &topic, &key, &value); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
		records = append(records, &kgo.Record{Topic: topic, Key: []byte(key), Value: []byte(value)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}

	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.client.ProduceSync(pctx, records...).FirstErr(); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE outbox SET published_at = now() WHERE id = ANY($1)`, pq.Array(ids)); err != nil {
		return 0, err
	}
	return len(ids), tx.Commit()
}
