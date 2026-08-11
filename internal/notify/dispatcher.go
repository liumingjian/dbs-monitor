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

const SMTPChannelKey = "SMTP"

func NewDispatcher(database DBTX) *Dispatcher {
	return &Dispatcher{queries: New(database)}
}

func WebhookChannelKey(targetID pgtype.UUID) string {
	return fmt.Sprintf("WEBHOOK:%x", targetID.Bytes)
}

func (dispatcher *Dispatcher) EnqueueDueRepeats(ctx context.Context, now time.Time) (int, error) {
	candidates, err := dispatcher.queries.ListRepeatCandidates(ctx)
	if err != nil {
		return 0, fmt.Errorf("list repeat candidates: %w", err)
	}
	now = now.UTC()
	enqueued := 0
	for _, candidate := range candidates {
		lastNotificationAt := candidate.LastNotificationAt.Time
		if !NotificationDue(
			EventRepeat,
			&lastNotificationAt,
			time.Duration(candidate.RepeatInterval)*time.Second,
			candidate.Disposition,
			now,
		) {
			continue
		}
		ids, err := dispatcher.queries.EnqueueAlertNotifications(ctx, EnqueueAlertNotificationsParams{
			AlertInstanceID: candidate.AlertInstanceID,
			EventType:       string(EventRepeat),
			TemplateID:      pgtype.Text{String: "builtin.notification.repeat.v1", Valid: true},
			Payload:         candidate.Payload,
			NextAttemptAt:   pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return enqueued, fmt.Errorf("enqueue repeat notification: %w", err)
		}
		enqueued += len(ids)
	}
	return enqueued, nil
}

func (dispatcher *Dispatcher) DispatchOne(ctx context.Context, now time.Time, channelsByKey map[string]Channel) (bool, error) {
	delivery, err := dispatcher.queries.ClaimDueNotification(ctx, pgtype.Timestamptz{Time: now.UTC(), Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim notification: %w", err)
	}
	message, deliveryErr := messageForDelivery(delivery)
	if deliveryErr == nil {
		key := SMTPChannelKey
		if delivery.Channel == "WEBHOOK" {
			key = WebhookChannelKey(delivery.ChannelTargetID)
		}
		channel, ok := channelsByKey[key]
		if !ok {
			deliveryErr = fmt.Errorf("notification channel %s is unavailable", delivery.Channel)
		} else {
			deliveryErr = channel.Send(ctx, message)
		}
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
