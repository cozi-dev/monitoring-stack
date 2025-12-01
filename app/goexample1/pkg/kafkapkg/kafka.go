package kafkapkg

import (
	"os"
	"strings"

	"github.com/segmentio/kafka-go"
)

func GetKafkaWriter(topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:                   kafka.TCP(os.Getenv("KAFKA_ENDPOINT")),
		Topic:                  topic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
	}
}

func GetKafkaReader(topic, groupID string) *kafka.Reader {
	brokers := strings.Split(os.Getenv("KAFKA_ENDPOINT"), ",")
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:  brokers,
		GroupID:  groupID,
		Topic:    topic,
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
	})
}
