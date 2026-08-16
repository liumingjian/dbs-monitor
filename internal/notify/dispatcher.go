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
	queries         *Queries
	retryBackoffCap time.Duration
}

const (
	SMTPChannelKey         = "SMTP"
	DefaultRetryBackoffCap = 5 * time.Second
)

func NewDispatcher(database DBTX) *Dispatcher {
	return NewDispatcherWithRetryBackoffCap(database, DefaultRetryBackoffCap)
}

func NewDispatcherWithRetryBackoffCap(database DBTX, retryBackoffCap time.Duration) *Dispatcher {
	return &Dispatcher{queries: New(database), retryBackoffCap: retryBackoffCap}
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
		maintenanceWindowID, inMaintenance, err := FindActiveMaintenanceWindow(ctx, dispatcher.queries, candidate.InstanceID, now)
		if err != nil {
			return enqueued, fmt.Errorf("match repeat maintenance window: %w", err)
		}
		notificationIDs, err := dispatcher.queries.EnqueueAlertNotifications(ctx, EnqueueAlertNotificationsParams{
			AlertInstanceID: candidate.AlertInstanceID,
			EventType:       string(EventRepeat),
			TemplateID:      pgtype.Text{String: "builtin.notification.repeat.v1", Valid: true},
			Payload:         candidate.Payload,
			NextAttemptAt:   pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return enqueued, fmt.Errorf("enqueue repeat notification: %w", err)
		}
		if !ShouldDeliver(EventRepeat, SuppressionFacts{Maintenance: inMaintenance}) && len(notificationIDs) > 0 {
			for _, notificationID := range notificationIDs {
				if err := dispatcher.queries.DeletePendingNotification(ctx, notificationID); err != nil {
					return enqueued, fmt.Errorf("discard maintenance-suppressed repeat: %w", err)
				}
			}
			if err := dispatcher.queries.RecordMaintenanceSuppressed(ctx, RecordMaintenanceSuppressedParams{
				ID:                  candidate.AlertInstanceID,
				MaintenanceWindowID: maintenanceWindowID,
				EvaluatedAt:         pgtype.Timestamptz{Time: now, Valid: true},
			}); err != nil {
				return enqueued, fmt.Errorf("record repeat maintenance suppression: %w", err)
			}
			continue
		}
		enqueued += len(notificationIDs)
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
	alert, alertErr := dispatcher.queries.GetNotificationAlertInstance(ctx, delivery.ID)
	if alertErr == nil {
		maintenanceWindowID, inMaintenance, matchErr := FindActiveMaintenanceWindow(ctx, dispatcher.queries, alert.InstanceID, now)
		if matchErr != nil {
			return true, fmt.Errorf("match delivery maintenance window: %w", matchErr)
		}
		if !ShouldDeliver(EventType(delivery.EventType), SuppressionFacts{Maintenance: inMaintenance}) {
			if err := dispatcher.queries.SuppressNotificationForMaintenance(ctx, SuppressNotificationForMaintenanceParams{
				ID:                  delivery.ID,
				MaintenanceWindowID: maintenanceWindowID,
				EvaluatedAt:         pgtype.Timestamptz{Time: now.UTC(), Valid: true},
			}); err != nil {
				return true, fmt.Errorf("suppress claimed notification for maintenance: %w", err)
			}
			return true, nil
		}
	} else if !errors.Is(alertErr, pgx.ErrNoRows) {
		return true, fmt.Errorf("read notification alert instance: %w", alertErr)
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
		nextAttemptAt = nextAttemptAt.Add(RetryDelay(failureCount, dispatcher.retryBackoffCap))
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
