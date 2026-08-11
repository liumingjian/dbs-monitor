package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Dispatcher struct {
	queries *Queries
}

func NewDispatcher(database DBTX) *Dispatcher {
	return &Dispatcher{queries: New(database)}
}

func (dispatcher *Dispatcher) DispatchOne(ctx context.Context, now time.Time, channel Channel) (bool, error) {
	delivery, err := dispatcher.queries.ClaimDueNotification(ctx, pgtype.Timestamptz{Time: now.UTC(), Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim notification: %w", err)
	}
	message, deliveryErr := messageForDelivery(delivery)
	if deliveryErr == nil {
		deliveryErr = channel.Send(ctx, message)
	}
	attemptedAt := pgtype.Timestamptz{Time: now.UTC(), Valid: true}
	if deliveryErr == nil {
		if recordErr := dispatcher.queries.RecordNotificationSent(ctx, RecordNotificationSentParams{
			NotificationID: delivery.ID,
			EvaluatedAt:    attemptedAt,
			RetryCount:     delivery.AttemptCount,
		}); recordErr != nil {
			return true, fmt.Errorf("record notification success: %w", recordErr)
		}
		return true, nil
	}

	failureCount := int(delivery.AttemptCount) + 1
	terminal := failureCount >= MaxAttempts
	nextAttemptAt := now.UTC()
	if !terminal {
		nextAttemptAt = nextAttemptAt.Add(RetryDelay(failureCount))
	}
	if recordErr := dispatcher.queries.RecordNotificationFailure(ctx, RecordNotificationFailureParams{
		NotificationID: delivery.ID,
		EvaluatedAt:    attemptedAt,
		FailureReason:  pgtype.Text{String: deliveryErr.Error(), Valid: true},
		RetryCount:     delivery.AttemptCount,
		Terminal:       terminal,
		NextAttemptAt:  pgtype.Timestamptz{Time: nextAttemptAt, Valid: true},
	}); recordErr != nil {
		return true, fmt.Errorf("record notification failure: %w", recordErr)
	}
	return true, nil
}

func messageForDelivery(delivery NotificationDelivery) (Message, error) {
	if EventType(delivery.EventType) == EventTest {
		message := FormatTestMessage()
		message.To = delivery.Target
		return message, nil
	}
	var payload AlertPayload
	if err := json.Unmarshal(delivery.Payload, &payload); err != nil {
		return Message{}, fmt.Errorf("decode notification payload: %w", err)
	}
	message, ok := FormatAlertMessage(EventType(delivery.EventType), payload)
	if !ok {
		return Message{}, fmt.Errorf("unsupported notification event %q", delivery.EventType)
	}
	message.To = delivery.Target
	return message, nil
}
