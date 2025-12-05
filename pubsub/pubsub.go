package pubsub2

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"cloud.google.com/go/pubsub"
	"google.golang.org/api/option"
)

type PubSubClient struct {
	Client *pubsub.Client
}

func NewPubSubClient(ctx context.Context, projectID string) (*PubSubClient, error) {

	emulatorHost := os.Getenv("PUBSUB_EMULATOR_HOST")

	// Nếu chạy emulator => KHÔNG cần credentials
	if emulatorHost != "" {
		fmt.Printf("[PubSub] Using emulator at %s\n", emulatorHost)

		// Sử dụng WithEndpoint để override host
		c, err := pubsub.NewClient(ctx, projectID,
			option.WithoutAuthentication(),
			option.WithEndpoint(emulatorHost),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create emulator client: %w", err)
		}

		return &PubSubClient{Client: c}, nil
	}

	// Ngược lại: dùng Google Cloud thật + ADC
	fmt.Println("[PubSub] Using real Google Cloud Pub/Sub")

	c, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloud pubsub client: %w", err)
	}

	return &PubSubClient{Client: c}, nil
}

func (p *PubSubClient) PublishJSON(ctx context.Context, topicName string, data interface{}) error {
	topic := p.Client.Topic(topicName)

	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}

	res := topic.Publish(ctx, &pubsub.Message{
		Data: raw,
	})

	id, err := res.Get(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("[PubSub] published message %s to topic %s\n", id, topicName)
	return nil
}
