package platformevent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/db"
)

const (
	LoginSucceeded            = "LOGIN_SUCCEEDED"
	LoginFailed               = "LOGIN_FAILED"
	UserCreated               = "USER_CREATED"
	UserStatusChanged         = "USER_STATUS_CHANGED"
	UserRoleChanged           = "USER_ROLE_CHANGED"
	UserPasswordReset         = "USER_PASSWORD_RESET"
	InstanceCredentialUpdated = "INSTANCE_CREDENTIAL_UPDATED"
	InstanceRemoved           = "INSTANCE_REMOVED"
	MasterKeyRotated          = "MASTER_KEY_ROTATED"
)

var Kinds = []string{
	LoginSucceeded,
	LoginFailed,
	UserCreated,
	UserStatusChanged,
	UserRoleChanged,
	UserPasswordReset,
	InstanceCredentialUpdated,
	InstanceRemoved,
	MasterKeyRotated,
}

var ForbiddenFieldNames = []string{
	"password",
	"old_password",
	"new_password",
	"initial_password",
	"password_hash",
	"password_ciphertext",
}

type Event struct {
	Kind         string
	OccurredAt   time.Time
	ActorID      *uuid.UUID
	ActorSubject string
	SubjectID    *uuid.UUID
}

func Record(ctx context.Context, database db.DBTX, event Event) error {
	if !knownKind(event.Kind) {
		return fmt.Errorf("unknown platform event kind %q", event.Kind)
	}
	actorSubject := event.ActorSubject
	if (event.ActorID == nil) == (strings.TrimSpace(actorSubject) == "") {
		return errors.New("platform event requires exactly one actor identity")
	}
	if event.OccurredAt.IsZero() {
		return errors.New("platform event requires an occurrence time")
	}
	_, err := database.Exec(ctx, `INSERT INTO platform_event
		(kind, occurred_at, actor_id, actor_subject, subject_id)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5)`,
		event.Kind, event.OccurredAt.UTC(), event.ActorID, actorSubject, event.SubjectID,
	)
	return err
}

func knownKind(kind string) bool {
	for _, candidate := range Kinds {
		if kind == candidate {
			return true
		}
	}
	return false
}
