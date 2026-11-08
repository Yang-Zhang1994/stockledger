package relay

import (
	"context"
	"encoding/json"
	"log"

	"github.com/segmentio/kafka-go"

	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

// KafkaPublisher keys each message by SKU, so all events for one SKU land on
// the same partition and consumers see them in version order.
type KafkaPublisher struct{ w *kafka.Writer }

func NewKafkaPublisher(brokers []string, topic string) *KafkaPublisher {
	return &KafkaPublisher{w: &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Topic:                  topic,
		Balancer:               &kafka.Hash{},
		RequiredAcks:           kafka.RequireAll,
		AllowAutoTopicCreation: true,
	}}
}

func (p *KafkaPublisher) Publish(ctx context.Context, events []inventory.Event) error {
	msgs := make([]kafka.Message, len(events))
	for i, e := range events {
		body, err := json.Marshal(e)
		if err != nil {
			return err
		}
		msgs[i] = kafka.Message{Key: []byte(e.SKU), Value: body}
	}
	return p.w.WriteMessages(ctx, msgs...)
}

func (p *KafkaPublisher) Close() error { return p.w.Close() }

// Consume reads the topic in a consumer group and passes each event to handle.
// The offset is committed only after handle succeeds.
func Consume(ctx context.Context, brokers []string, topic, group string, handle func(inventory.Event) error) error {
	r := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, Topic: topic, GroupID: group})
	defer r.Close()
	for {
		m, err := r.FetchMessage(ctx)
		if err != nil {
			return err
		}
		var e inventory.Event
		if err := json.Unmarshal(m.Value, &e); err != nil {
			log.Printf("consumer: skipping malformed message at offset %d: %v", m.Offset, err)
		} else if err := handle(e); err != nil {
			return err
		}
		if err := r.CommitMessages(ctx, m); err != nil {
			return err
		}
	}
}

// LocalPublisher hands events straight to a handler; used by -mem mode.
type LocalPublisher func(inventory.Event) error

func (h LocalPublisher) Publish(_ context.Context, events []inventory.Event) error {
	for _, e := range events {
		if err := h(e); err != nil {
			return err
		}
	}
	return nil
}
