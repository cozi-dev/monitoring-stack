package kafkapkg

import (
	"os"
	"time"

	"github.com/segmentio/kafka-go"
)

func GetKafkaWriter(topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:                   kafka.TCP(os.Getenv("KAFKA_ENDPOINT")),
		Topic:                  topic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
		BatchTimeout:           10 * time.Millisecond,
	}
}
