package pubsub2

import (
	"context"
	"project/events"
)

func (p *PubSubClient) PublishTxCreate(
	ctx context.Context,
	msg events.TxCreateRequest,
) error {
	return p.PublishJSON(ctx, "tx.create", msg)
}

func (p *PubSubClient) PublishTxAdd(
	ctx context.Context,
	msg events.TxAddRequest,
) error {
	return p.PublishJSON(ctx, "tx.add", msg)
}
