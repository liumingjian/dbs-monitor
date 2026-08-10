package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

var (
	errLastPlatformAdmin = errors.New("at least one enabled platform administrator is required")
	errSelfDisable       = errors.New("you cannot disable your own account")
	errSelfDowngrade     = errors.New("you cannot downgrade your own role")
	errSelfPasswordReset = errors.New("use change password to update your own password")
	errUserNotFound      = errors.New("user not found")
)

func (handler *Handler) GetCurrentUser(ctx context.Context, _ api.GetCurrentUserRequestObject) (api.GetCurrentUserResponseObject, error) {
	current := ctx.Value(authenticatedUserKey{}).(authenticatedUser)
	row, err := New(handler.platform).GetCurrentUser(ctx, userID(current.id))
	if err != nil {
		return nil, err
	}
	return api.GetCurrentUser200JSONResponse(toAPIUser(row.ID, row.Username, row.Role, row.Enabled, row.CreatedAt)), nil
}

func (handler *Handler) ListUsers(ctx context.Context, _ api.ListUsersRequestObject) (api.ListUsersResponseObject, error) {
	rows, err := New(handler.platform).ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	response := make(api.ListUsers200JSONResponse, 0, len(rows))
	for _, row := range rows {
		response = append(response, toAPIUser(row.ID, row.Username, row.Role, row.Enabled, row.CreatedAt))
	}
	return response, nil
}

func (handler *Handler) CreateUser(ctx context.Context, request api.CreateUserRequestObject) (api.CreateUserResponseObject, error) {
	if request.Body == nil || strings.TrimSpace(request.Body.Username) == "" || !validRole(request.Body.Role) {
		return api.CreateUser400JSONResponse(errorBody(api.VALIDATIONFAILED, "valid username and role are required")), nil
	}
	password, hash, err := generatedPassword()
	if err != nil {
		return nil, err
	}
	row, err := New(handler.platform).CreateUser(ctx, CreateUserParams{
		ID: userID(uuid.New()), Username: request.Body.Username, PasswordHash: hash, Role: string(request.Body.Role),
	})
	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return api.CreateUser409JSONResponse(errorBody(api.CONFLICT, "username already exists")), nil
		}
		return nil, err
	}
	return api.CreateUser201JSONResponse{
		User: toAPIUser(row.ID, row.Username, row.Role, row.Enabled, row.CreatedAt), InitialPassword: password,
	}, nil
}

func (handler *Handler) UpdateUserStatus(ctx context.Context, request api.UpdateUserStatusRequestObject) (api.UpdateUserStatusResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("user status body is required")
	}
	current := ctx.Value(authenticatedUserKey{}).(authenticatedUser)
	err := handler.setUserEnabled(ctx, current.id, request.Id, request.Body.Enabled)
	if errors.Is(err, errSelfDisable) || errors.Is(err, errLastPlatformAdmin) {
		return api.UpdateUserStatus400JSONResponse(errorBody(api.VALIDATIONFAILED, err.Error())), nil
	}
	if errors.Is(err, errUserNotFound) {
		return api.UpdateUserStatus404JSONResponse(errorBody(api.NOTFOUND, err.Error())), nil
	}
	if err != nil {
		return nil, err
	}
	row, err := New(handler.platform).GetCurrentUser(ctx, userID(request.Id))
	if err != nil {
		return nil, err
	}
	return api.UpdateUserStatus200JSONResponse(toAPIUser(row.ID, row.Username, row.Role, row.Enabled, row.CreatedAt)), nil
}

func (handler *Handler) UpdateUserRole(ctx context.Context, request api.UpdateUserRoleRequestObject) (api.UpdateUserRoleResponseObject, error) {
	if request.Body == nil || !validRole(request.Body.Role) {
		return api.UpdateUserRole400JSONResponse(errorBody(api.VALIDATIONFAILED, "valid role is required")), nil
	}
	current := ctx.Value(authenticatedUserKey{}).(authenticatedUser)
	err := handler.setUserRole(ctx, current.id, request.Id, string(request.Body.Role))
	if errors.Is(err, errSelfDowngrade) || errors.Is(err, errLastPlatformAdmin) {
		return api.UpdateUserRole400JSONResponse(errorBody(api.VALIDATIONFAILED, err.Error())), nil
	}
	if errors.Is(err, errUserNotFound) {
		return api.UpdateUserRole404JSONResponse(errorBody(api.NOTFOUND, err.Error())), nil
	}
	if err != nil {
		return nil, err
	}
	row, err := New(handler.platform).GetCurrentUser(ctx, userID(request.Id))
	if err != nil {
		return nil, err
	}
	return api.UpdateUserRole200JSONResponse(toAPIUser(row.ID, row.Username, row.Role, row.Enabled, row.CreatedAt)), nil
}

func (handler *Handler) ResetUserPassword(ctx context.Context, request api.ResetUserPasswordRequestObject) (api.ResetUserPasswordResponseObject, error) {
	current := ctx.Value(authenticatedUserKey{}).(authenticatedUser)
	if current.id == request.Id {
		return api.ResetUserPassword400JSONResponse(errorBody(api.VALIDATIONFAILED, errSelfPasswordReset.Error())), nil
	}
	password, hash, err := generatedPassword()
	if err != nil {
		return nil, err
	}
	err = handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := New(tx)
		updated, err := queries.SetUserPassword(ctx, SetUserPasswordParams{ID: userID(request.Id), PasswordHash: hash})
		if err != nil {
			return err
		}
		if updated == 0 {
			return errUserNotFound
		}
		return queries.DeleteUserSessions(ctx, userID(request.Id))
	})
	if errors.Is(err, errUserNotFound) {
		return api.ResetUserPassword404JSONResponse(errorBody(api.NOTFOUND, err.Error())), nil
	}
	if err != nil {
		return nil, err
	}
	return api.ResetUserPassword200JSONResponse{Password: password}, nil
}

func (handler *Handler) ChangeOwnPassword(ctx context.Context, request api.ChangeOwnPasswordRequestObject) (api.ChangeOwnPasswordResponseObject, error) {
	if request.Body == nil || utf8.RuneCountInString(request.Body.NewPassword) < 12 {
		return api.ChangeOwnPassword400JSONResponse(errorBody(api.VALIDATIONFAILED, "new password must be at least 12 characters")), nil
	}
	current := ctx.Value(authenticatedUserKey{}).(authenticatedUser)
	queries := New(handler.platform)
	oldHash, err := queries.GetUserPassword(ctx, userID(current.id))
	if err != nil || bcrypt.CompareHashAndPassword(oldHash, []byte(request.Body.OldPassword)) != nil {
		return api.ChangeOwnPassword400JSONResponse(errorBody(api.VALIDATIONFAILED, "old password is incorrect")), nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.Body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	if _, err := queries.SetUserPassword(ctx, SetUserPasswordParams{ID: userID(current.id), PasswordHash: hash}); err != nil {
		return nil, err
	}
	return api.ChangeOwnPassword204Response{}, nil
}

func (handler *Handler) setUserEnabled(ctx context.Context, actorID, targetID uuid.UUID, enabled bool) error {
	return handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := New(tx)
		admins, err := queries.LockEnabledPlatformAdmins(ctx)
		if err != nil {
			return err
		}
		target, err := queries.GetUserForUpdate(ctx, userID(targetID))
		if errors.Is(err, pgx.ErrNoRows) {
			return errUserNotFound
		}
		if err != nil {
			return err
		}
		if !enabled && actorID == targetID {
			return errSelfDisable
		}
		if !enabled && target.Enabled && target.Role == string(api.PLATFORMADMIN) && len(admins) <= 1 {
			return errLastPlatformAdmin
		}
		if _, err := queries.SetUserEnabled(ctx, SetUserEnabledParams{ID: target.ID, Enabled: enabled}); err != nil {
			return err
		}
		if !enabled {
			return queries.DeleteUserSessions(ctx, target.ID)
		}
		return nil
	})
}

func (handler *Handler) setUserRole(ctx context.Context, actorID, targetID uuid.UUID, role string) error {
	return handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := New(tx)
		admins, err := queries.LockEnabledPlatformAdmins(ctx)
		if err != nil {
			return err
		}
		target, err := queries.GetUserForUpdate(ctx, userID(targetID))
		if errors.Is(err, pgx.ErrNoRows) {
			return errUserNotFound
		}
		if err != nil {
			return err
		}
		if actorID == targetID && target.Role == string(api.PLATFORMADMIN) && role != string(api.PLATFORMADMIN) {
			return errSelfDowngrade
		}
		if target.Enabled && target.Role == string(api.PLATFORMADMIN) && role != string(api.PLATFORMADMIN) && len(admins) <= 1 {
			return errLastPlatformAdmin
		}
		_, err = queries.SetUserRole(ctx, SetUserRoleParams{ID: target.ID, Role: role})
		return err
	})
}

func generatedPassword() (string, []byte, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", nil, err
	}
	password := hex.EncodeToString(value)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return password, hash, err
}

func validRole(role api.Role) bool {
	return role == api.READONLY || role == api.ALERTADMIN || role == api.PLATFORMADMIN
}

func userID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func toAPIUser(id pgtype.UUID, username, role string, enabled bool, createdAt pgtype.Timestamptz) api.User {
	return api.User{Id: id.Bytes, Username: username, Role: api.Role(role), Enabled: enabled, CreatedAt: createdAt.Time}
}
