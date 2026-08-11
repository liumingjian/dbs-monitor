package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"
	"golang.org/x/crypto/bcrypt"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
)

const (
	sessionCookie            = "dbs_monitor_session"
	agentBackfillWindow      = 5 * time.Minute
	agentClockSkew           = 30 * time.Second
	agentClockSkewErrorCode  = "CLOCK_SKEW"
	agentClockSkewMessage    = "时钟偏移"
	agentVersionErrorCode    = "AGENT_VERSION_TOO_OLD"
	agentVersionErrorMessage = "版本过旧，需升级"
)

type authenticatedAgentKey struct{}
type authenticatedUserKey struct{}

type Handler struct {
	platform      *db.Pool
	clock         clock.Clock
	keyring       *instance.CredentialKeyring
	dialer        monitorpg.Dialer
	serverVersion string
}

func NewHandler(platform *db.Pool, currentClock clock.Clock, keyring *instance.CredentialKeyring) *Handler {
	return NewHandlerWithVersion(platform, currentClock, keyring, "1.0.0")
}

func NewHandlerWithVersion(platform *db.Pool, currentClock clock.Clock, keyring *instance.CredentialKeyring, serverVersion string) *Handler {
	return NewHandlerWithDialer(platform, currentClock, keyring, monitorpg.DirectDialer{}, serverVersion)
}

func NewHandlerWithDialer(platform *db.Pool, currentClock clock.Clock, keyring *instance.CredentialKeyring, dialer monitorpg.Dialer, serverVersion string) *Handler {
	return &Handler{platform: platform, clock: currentClock, keyring: keyring, dialer: dialer, serverVersion: serverVersion}
}

func (handler *Handler) Routes() http.Handler {
	strict := api.NewStrictHandler(handler, []api.StrictMiddlewareFunc{handler.authenticate})
	return api.HandlerFromMux(strict, http.NewServeMux())
}

func (handler *Handler) CreateSession(ctx context.Context, request api.CreateSessionRequestObject) (api.CreateSessionResponseObject, error) {
	if request.Body == nil {
		return unauthorizedLogin(), nil
	}
	user, err := New(handler.platform).GetUserForLogin(ctx, request.Body.Username)
	if err != nil || bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(request.Body.Password)) != nil {
		return unauthorizedLogin(), nil
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return nil, err
	}
	expires := handler.clock.Now().UTC().Add(24 * time.Hour)
	if err := New(handler.platform).CreateSession(ctx, CreateSessionParams{
		TokenHash: tokenHash, UserID: user.ID,
		ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		return nil, err
	}
	cookie := (&http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	}).String()
	return api.CreateSession204Response{Headers: api.CreateSession204ResponseHeaders{SetCookie: cookie}}, nil
}

func (handler *Handler) ListInstances(ctx context.Context, _ api.ListInstancesRequestObject) (api.ListInstancesResponseObject, error) {
	rows, err := instance.New(handler.platform).ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	response := make(api.ListInstances200JSONResponse, 0, len(rows))
	for _, row := range rows {
		status, err := alertStatus(ctx, handler.platform, row.ID)
		if err != nil {
			return nil, err
		}
		response = append(response, toAPIInstance(row.ID, row.Name, row.Host, row.Port, row.DatabaseName, row.Username, row.AgentVersion, status))
	}
	return response, nil
}

func (handler *Handler) CreateInstance(ctx context.Context, request api.CreateInstanceRequestObject) (api.CreateInstanceResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("instance body is required")
	}
	if err := validateTargetConnection(ctx, handler.dialer, targetConnectionInput{
		host:     request.Body.Host,
		port:     request.Body.Port,
		database: request.Body.Database,
		username: request.Body.Username,
		password: request.Body.Password,
	}); err != nil {
		if responseBody, ok := targetValidationResponseBody(err); ok {
			return api.CreateInstance400JSONResponse(responseBody), nil
		}
		return nil, err
	}
	id := uuid.New()
	ciphertext, keyVersion, err := handler.keyring.EncryptPassword(id, request.Body.Password)
	if err != nil {
		return nil, err
	}
	row, err := instance.New(handler.platform).CreateInstance(ctx, instance.CreateInstanceParams{
		ID:                 pgtype.UUID{Bytes: id, Valid: true},
		Name:               request.Body.Name,
		Host:               request.Body.Host,
		Port:               int32(request.Body.Port),
		DatabaseName:       request.Body.Database,
		Username:           request.Body.Username,
		PasswordCiphertext: ciphertext,
		PasswordKeyVersion: keyVersion,
	})
	if err != nil {
		return nil, err
	}
	return api.CreateInstance201JSONResponse{
		Instance: toAPIInstance(row.ID, row.Name, row.Host, row.Port, row.DatabaseName, row.Username, row.AgentVersion, api.OK),
	}, nil
}

func (handler *Handler) GetInstance(ctx context.Context, request api.GetInstanceRequestObject) (api.GetInstanceResponseObject, error) {
	row, err := instance.New(handler.platform).GetInstance(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
	if err != nil {
		return nil, err
	}
	status, err := alertStatus(ctx, handler.platform, row.ID)
	if err != nil {
		return nil, err
	}
	return api.GetInstance200JSONResponse(toAPIInstance(row.ID, row.Name, row.Host, row.Port, row.DatabaseName, row.Username, row.AgentVersion, status)), nil
}

func (handler *Handler) UpdateInstance(ctx context.Context, request api.UpdateInstanceRequestObject) (api.UpdateInstanceResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("instance body is required")
	}
	instanceID := pgtype.UUID{Bytes: request.Id, Valid: true}
	var updatedInstance instance.UpdateInstanceMetadataRow
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := instance.New(tx)
		storedInstance, err := queries.GetInstanceForUpdate(ctx, instanceID)
		if err != nil {
			return err
		}
		connectionTargetChanged := storedInstance.Host != request.Body.Host ||
			storedInstance.Port != int32(request.Body.Port) ||
			storedInstance.DatabaseName != request.Body.Database
		if connectionTargetChanged {
			password, err := handler.keyring.DecryptPassword(
				request.Id,
				storedInstance.PasswordCiphertext,
				storedInstance.PasswordKeyVersion,
			)
			if err != nil {
				return err
			}
			if err := validateTargetConnection(ctx, handler.dialer, targetConnectionInput{
				host:     request.Body.Host,
				port:     request.Body.Port,
				database: request.Body.Database,
				username: storedInstance.Username,
				password: password,
			}); err != nil {
				return err
			}
		}
		updatedInstance, err = queries.UpdateInstanceMetadata(ctx, instance.UpdateInstanceMetadataParams{
			ID:           instanceID,
			Name:         request.Body.Name,
			Host:         request.Body.Host,
			Port:         int32(request.Body.Port),
			DatabaseName: request.Body.Database,
		})
		return err
	})
	if err != nil {
		if responseBody, ok := targetValidationResponseBody(err); ok {
			return api.UpdateInstance400JSONResponse(responseBody), nil
		}
		return nil, err
	}
	status, err := alertStatus(ctx, handler.platform, updatedInstance.ID)
	if err != nil {
		return nil, err
	}
	return api.UpdateInstance200JSONResponse(toAPIInstance(
		updatedInstance.ID,
		updatedInstance.Name,
		updatedInstance.Host,
		updatedInstance.Port,
		updatedInstance.DatabaseName,
		updatedInstance.Username,
		updatedInstance.AgentVersion,
		status,
	)), nil
}

func (handler *Handler) UpdateInstanceCredential(ctx context.Context, request api.UpdateInstanceCredentialRequestObject) (api.UpdateInstanceCredentialResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("instance credential body is required")
	}
	instanceID := pgtype.UUID{Bytes: request.Id, Valid: true}
	var updatedUsername string
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := instance.New(tx)
		storedInstance, err := queries.GetInstanceForUpdate(ctx, instanceID)
		if err != nil {
			return err
		}
		if err := validateTargetConnection(ctx, handler.dialer, targetConnectionInput{
			host:     storedInstance.Host,
			port:     int(storedInstance.Port),
			database: storedInstance.DatabaseName,
			username: request.Body.Username,
			password: request.Body.Password,
		}); err != nil {
			return err
		}
		ciphertext, keyVersion, err := handler.keyring.EncryptPassword(request.Id, request.Body.Password)
		if err != nil {
			return err
		}
		updatedUsername, err = queries.UpdateInstanceCredential(ctx, instance.UpdateInstanceCredentialParams{
			ID:                 instanceID,
			Username:           request.Body.Username,
			PasswordCiphertext: ciphertext,
			PasswordKeyVersion: keyVersion,
		})
		return err
	})
	if err != nil {
		if responseBody, ok := targetValidationResponseBody(err); ok {
			return api.UpdateInstanceCredential400JSONResponse(responseBody), nil
		}
		return nil, err
	}
	return api.UpdateInstanceCredential200JSONResponse{Username: updatedUsername}, nil
}

func (handler *Handler) DeleteInstance(ctx context.Context, request api.DeleteInstanceRequestObject) (api.DeleteInstanceResponseObject, error) {
	if err := instance.New(handler.platform).DeleteInstance(ctx, pgtype.UUID{Bytes: request.Id, Valid: true}); err != nil {
		return nil, err
	}
	return api.DeleteInstance204Response{}, nil
}

func (handler *Handler) ListCollectionTaskStates(ctx context.Context, request api.ListCollectionTaskStatesRequestObject) (api.ListCollectionTaskStatesResponseObject, error) {
	states, err := handler.collectionTaskStates(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
	if err != nil {
		return nil, err
	}
	return api.ListCollectionTaskStates200JSONResponse(states), nil
}

func (handler *Handler) UpdateCollectionTaskInterval(ctx context.Context, request api.UpdateCollectionTaskIntervalRequestObject) (api.UpdateCollectionTaskIntervalResponseObject, error) {
	if request.Body == nil {
		return api.UpdateCollectionTaskInterval400JSONResponse(errorBody(api.VALIDATIONFAILED, "collection task interval body is required")), nil
	}
	taskID := metric.TaskID(request.TaskId)
	interval := time.Duration(request.Body.IntervalSeconds) * time.Second
	if err := metric.ValidateTaskInterval(taskID, interval); err != nil {
		return api.UpdateCollectionTaskInterval400JSONResponse(errorBody(api.VALIDATIONFAILED, err.Error())), nil
	}
	instanceID := pgtype.UUID{Bytes: request.Id, Valid: true}
	if err := metric.New(handler.platform).SetTaskInterval(ctx, metric.SetTaskIntervalParams{
		InstanceID: instanceID, TaskID: request.TaskId, IntervalSeconds: int32(request.Body.IntervalSeconds),
	}); err != nil {
		return nil, err
	}
	states, err := handler.collectionTaskStates(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	for _, state := range states {
		if string(state.TaskId) == request.TaskId {
			return api.UpdateCollectionTaskInterval200JSONResponse(state), nil
		}
	}
	return api.UpdateCollectionTaskInterval400JSONResponse(errorBody(api.VALIDATIONFAILED, "unknown collection task")), nil
}

func (handler *Handler) collectionTaskStates(ctx context.Context, instanceID pgtype.UUID) ([]api.CollectionTaskState, error) {
	persistedRows, err := New(handler.platform).ListPersistedCollectionTaskStates(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	persistedStates := make(map[metric.TaskID]ListPersistedCollectionTaskStatesRow, len(persistedRows))
	for _, row := range persistedRows {
		persistedStates[metric.TaskID(row.TaskID)] = row
	}
	configuredRows, err := metric.New(handler.platform).ListTaskIntervals(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	configuredIntervals := make(map[metric.TaskID]int, len(configuredRows))
	for _, row := range configuredRows {
		configuredIntervals[metric.TaskID(row.TaskID)] = int(row.IntervalSeconds)
	}

	states := make([]api.CollectionTaskState, 0, len(metric.Tasks))
	for _, task := range metric.Tasks {
		intervalSeconds := int(task.Interval / time.Second)
		if configuredInterval, exists := configuredIntervals[task.ID]; exists {
			intervalSeconds = configuredInterval
		}
		state := api.CollectionTaskState{
			TaskId:          api.CollectionTaskStateTaskId(task.ID),
			Kind:            api.CollectionTaskStateKind(task.Kind),
			IntervalSeconds: intervalSeconds,
		}
		if persistedState, exists := persistedStates[task.ID]; exists {
			state.ConsecutiveFailures = int(persistedState.ConsecutiveFailures)
			state.LastDueAt = timePointer(persistedState.LastDueAt)
			state.LastStartedAt = timePointer(persistedState.LastStartedAt)
			state.LastFinishedAt = timePointer(persistedState.LastFinishedAt)
			state.LastSuccessAt = timePointer(persistedState.LastSuccessAt)
			state.NextEligibleAt = timePointer(persistedState.NextEligibleAt)
			if persistedState.LastResult.Valid {
				value := api.CollectionTaskResult(persistedState.LastResult.String)
				state.LastResult = &value
			}
			if persistedState.LastErrorCode.Valid {
				value := persistedState.LastErrorCode.String
				state.LastErrorCode = &value
			}
			if persistedState.LastErrorMessage.Valid {
				value := persistedState.LastErrorMessage.String
				state.LastErrorMessage = &value
			}
		}
		states = append(states, state)
	}
	return states, nil
}

func (handler *Handler) GetMetricSeries(ctx context.Context, request api.GetMetricSeriesRequestObject) (api.GetMetricSeriesResponseObject, error) {
	requested := api.Auto
	if request.Params.Step != nil {
		requested = *request.Params.Step
	}
	step, err := chooseMetricStep(requested, request.Params.From, request.Params.To)
	if err != nil {
		return api.GetMetricSeries400JSONResponse(errorBody(api.VALIDATIONFAILED, err.Error())), nil
	}
	result := api.MetricSeriesResponse{From: request.Params.From, To: request.Params.To, Step: step.name}
	queries := metric.New(handler.platform)
	for _, metricID := range request.Params.Metric {
		entry := struct {
			Metric string `json:"metric"`
			Series []struct {
				Labels map[string]string `json:"labels"`
				Points [][]*float64      `json:"points"`
			} `json:"series"`
			Unavailability nullable.Nullable[api.Unavailability] `json:"unavailability"`
			Unit           string                                `json:"unit"`
		}{Metric: string(metricID), Unit: metricUnit(string(metricID)), Series: []struct {
			Labels map[string]string `json:"labels"`
			Points [][]*float64      `json:"points"`
		}{}, Unavailability: nullable.NewNullNullable[api.Unavailability]()}

		if strings.HasPrefix(string(metricID), "pg.") {
			var probeResult pgtype.Text
			err := handler.platform.QueryRow(ctx, `SELECT last_result FROM instance_collection_task_state
				WHERE instance_id = $1 AND task_id = 'pg.probe'`, pgtype.UUID{Bytes: request.Id, Valid: true}).Scan(&probeResult)
			if err == nil && probeResult.Valid {
				switch api.CollectionTaskResult(probeResult.String) {
				case api.FAILED, api.TIMEDOUT:
					entry.Unavailability = nullable.NewNullableWithValue(api.DBUNREACHABLE)
					result.Metrics = append(result.Metrics, entry)
					continue
				}
			}
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}

		series, err := queries.SeriesForMetric(ctx, metric.SeriesForMetricParams{
			InstanceID: pgtype.UUID{Bytes: request.Id, Valid: true}, MetricID: string(metricID),
		})
		if err != nil {
			return nil, err
		}
		for _, found := range series {
			points := make([]struct {
				ts    time.Time
				value float64
			}, 0)
			if step.raw {
				raw, err := queries.PointsInRange(ctx, metric.PointsInRangeParams{
					SeriesID: found.SeriesID,
					Ts:       pgtype.Timestamptz{Time: request.Params.From, Valid: true},
					Ts_2:     pgtype.Timestamptz{Time: request.Params.To, Valid: true},
				})
				if err != nil {
					return nil, err
				}
				for _, point := range raw {
					points = append(points, struct {
						ts    time.Time
						value float64
					}{point.Ts.Time, point.Value})
				}
			} else {
				bucketed, err := queries.BucketedPointsInRange(ctx, metric.BucketedPointsInRangeParams{
					SeriesID: found.SeriesID,
					Ts:       pgtype.Timestamptz{Time: request.Params.From, Valid: true},
					Ts_2:     pgtype.Timestamptz{Time: request.Params.To, Valid: true},
					Bucket:   step.bucket,
				})
				if err != nil {
					return nil, err
				}
				for _, point := range bucketed {
					points = append(points, struct {
						ts    time.Time
						value float64
					}{point.Ts.Time, point.Value})
				}
			}
			if len(points) == 0 {
				continue
			}
			item := struct {
				Labels map[string]string `json:"labels"`
				Points [][]*float64      `json:"points"`
			}{Labels: map[string]string{}, Points: make([][]*float64, 0, len(points))}
			_ = json.Unmarshal(found.Labels, &item.Labels)
			for _, point := range points {
				timestamp, value := float64(point.ts.Unix()), point.value
				item.Points = append(item.Points, []*float64{&timestamp, &value})
			}
			entry.Series = append(entry.Series, item)
		}
		if len(entry.Series) == 0 {
			if len(series) == 0 {
				entry.Unavailability = nullable.NewNullableWithValue(api.NOSAMPLESYET)
			} else {
				entry.Unavailability = nullable.NewNullableWithValue(api.NODATAINRANGE)
			}
		}
		result.Metrics = append(result.Metrics, entry)
	}
	return api.GetMetricSeries200JSONResponse(result), nil
}

func (handler *Handler) ReportAgentMetrics(ctx context.Context, request api.ReportAgentMetricsRequestObject) (api.ReportAgentMetricsResponseObject, error) {
	if request.Body == nil {
		return badAgentRequest("report body is required"), nil
	}
	authenticated, ok := ctx.Value(authenticatedAgentKey{}).(uuid.UUID)
	if !ok || authenticated != request.Body.InstanceId {
		return unauthorizedAgent(), nil
	}
	if !agentVersionSupported(request.Body.AgentVersion, handler.serverVersion) {
		return handler.rejectAgentReport(ctx, authenticated, request.Body.AgentVersion, agentVersionErrorCode, agentVersionErrorMessage)
	}
	now := handler.clock.Now().UTC()
	if agentReportHasClockSkew(*request.Body, now) {
		return handler.rejectAgentReport(ctx, authenticated, request.Body.AgentVersion, agentClockSkewErrorCode, agentClockSkewMessage)
	}

	if err := handler.storeAgentReport(ctx, authenticated, *request.Body, now); err != nil {
		if !metric.IsMissingPartition(err) {
			return nil, err
		}
		if err := metric.EnsurePartitions(ctx, handler.platform, now.Add(-agentBackfillWindow)); err != nil {
			return nil, err
		}
		if err := handler.storeAgentReport(ctx, authenticated, *request.Body, now); err != nil {
			return nil, err
		}
	}
	return api.ReportAgentMetrics204Response{}, nil
}

func agentReportHasClockSkew(report api.AgentReport, now time.Time) bool {
	earliestCurrentSample := now.Add(-agentClockSkew)
	latestSample := now.Add(agentClockSkew)
	if report.Timestamp.Before(earliestCurrentSample) || report.Timestamp.After(latestSample) {
		return true
	}
	if report.Backfill == nil {
		return false
	}
	for _, sample := range *report.Backfill {
		if sample.Timestamp.After(latestSample) {
			return true
		}
	}
	return false
}

func (handler *Handler) storeAgentReport(ctx context.Context, instanceID uuid.UUID, report api.AgentReport, receivedAt time.Time) error {
	samples := []api.AgentSample{{Timestamp: report.Timestamp, Metrics: report.Metrics}}
	if report.Backfill != nil {
		samples = append(samples, (*report.Backfill)...)
	}
	oldestSample := receivedAt.Add(-agentBackfillWindow)
	databaseInstanceID := pgtype.UUID{Bytes: instanceID, Valid: true}

	return handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := metric.New(tx)
		for _, batch := range samples {
			if batch.Timestamp.Before(oldestSample) {
				continue
			}
			for _, reportedMetric := range batch.Metrics {
				seriesID, err := queries.UpsertSeries(ctx, metric.UpsertSeriesParams{
					InstanceID: databaseInstanceID, MetricID: string(reportedMetric.Metric),
					Labels: json.RawMessage(`{}`), LabelsKey: "{}",
					LastSeen: pgtype.Timestamptz{Time: batch.Timestamp, Valid: true},
				})
				if err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, $3)", seriesID, batch.Timestamp, reportedMetric.Value); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(ctx, "UPDATE instance SET agent_version = $2 WHERE id = $1", instanceID, report.AgentVersion); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO instance_collect_state (instance_id, source, last_report_at)
			VALUES ($1, 'AGENT', $2) ON CONFLICT (instance_id, source)
			DO UPDATE SET last_report_at = EXCLUDED.last_report_at, last_error_code = NULL, last_error_message = NULL`, instanceID, receivedAt)
		return err
	})
}

func (handler *Handler) rejectAgentReport(ctx context.Context, instanceID uuid.UUID, version, code, message string) (api.ReportAgentMetricsResponseObject, error) {
	if err := handler.recordAgentError(ctx, instanceID, version, code, message); err != nil {
		return nil, err
	}
	return badAgentRequest(message), nil
}

func (handler *Handler) recordAgentError(ctx context.Context, instanceID uuid.UUID, version, code, message string) error {
	return handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE instance SET agent_version = $2 WHERE id = $1", instanceID, version); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO instance_collect_state (instance_id, source, last_error_code, last_error_message)
			VALUES ($1, 'AGENT', $2, $3) ON CONFLICT (instance_id, source)
			DO UPDATE SET last_error_code = EXCLUDED.last_error_code, last_error_message = EXCLUDED.last_error_message`, instanceID, code, message)
		return err
	})
}

func agentVersionSupported(agentVersion, serverVersion string) bool {
	agentMajor, agentOK := majorVersion(agentVersion)
	serverMajor, serverOK := majorVersion(serverVersion)
	return agentOK && serverOK && agentMajor >= serverMajor-1
}

func majorVersion(version string) (int, bool) {
	version = strings.TrimPrefix(version, "v")
	majorText, _, _ := strings.Cut(version, ".")
	major, err := strconv.Atoi(majorText)
	return major, err == nil && major >= 0
}

func (handler *Handler) authenticate(next api.StrictHandlerFunc, operationID string) api.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value interface{}) (interface{}, error) {
		if operationID == "CreateSession" {
			return next(ctx, writer, request, value)
		}
		if operationID == "ReportAgentMetrics" {
			report := value.(api.ReportAgentMetricsRequestObject)
			if report.Body == nil {
				return badAgentRequest("report body is required"), nil
			}
			token := bearerToken(request.Header.Get("Authorization"))
			stored, err := New(handler.platform).GetAgentTokenHash(ctx, pgtype.UUID{Bytes: report.Body.InstanceId, Valid: true})
			provided := sha256.Sum256([]byte(token))
			if err != nil || token == "" || subtle.ConstantTimeCompare(stored, provided[:]) != 1 {
				return unauthorizedAgent(), nil
			}
			return next(context.WithValue(ctx, authenticatedAgentKey{}, report.Body.InstanceId), writer, request, value)
		}
		cookie, err := request.Cookie(sessionCookie)
		if err != nil {
			writer.WriteHeader(http.StatusUnauthorized)
			return nil, nil
		}
		hash := sha256.Sum256([]byte(cookie.Value))
		user, err := New(handler.platform).GetSessionUser(ctx, GetSessionUserParams{
			TokenHash: hash[:], ExpiresAt: pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true},
		})
		if err != nil {
			writer.WriteHeader(http.StatusUnauthorized)
			return nil, nil
		}
		required, exists := RequiredRoles[operationID]
		if !exists {
			writer.WriteHeader(http.StatusForbidden)
			return nil, nil
		}
		if roleRank(user.Role) < roleRank(required) {
			writer.WriteHeader(http.StatusForbidden)
			return nil, nil
		}
		return next(context.WithValue(ctx, authenticatedUserKey{}, uuid.UUID(user.ID.Bytes)), writer, request, value)
	}
}

var RequiredRoles = map[string]string{
	"CreateSession": "READONLY", "ReportAgentMetrics": "AGENT",
	"ListAlertRules": "READONLY", "CreateAlertRule": "ALERT_ADMIN",
	"ListInstances": "READONLY", "GetInstance": "READONLY", "GetMetricSeries": "READONLY",
	"ListCollectionTaskStates": "READONLY", "UpdateCollectionTaskInterval": "PLATFORM_ADMIN",
	"CreateInstance": "PLATFORM_ADMIN", "UpdateInstance": "ALERT_ADMIN", "UpdateInstanceCredential": "PLATFORM_ADMIN", "DeleteInstance": "PLATFORM_ADMIN",
	"GetCurrentUser": "READONLY", "ChangeOwnPassword": "READONLY", "ListUsers": "READONLY",
	"CreateUser": "PLATFORM_ADMIN", "ResetUserPassword": "PLATFORM_ADMIN",
	"UpdateUserRole": "PLATFORM_ADMIN", "UpdateUserStatus": "PLATFORM_ADMIN",
}

func roleRank(role string) int {
	return map[string]int{"READONLY": 1, "ALERT_ADMIN": 2, "PLATFORM_ADMIN": 3}[role]
}

func AdminExists(ctx context.Context, platform *db.Pool) (bool, error) {
	return New(platform).AdminExists(ctx)
}

func SeedAdmin(ctx context.Context, platform *db.Pool, username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	id := uuid.New()
	return New(platform).CreateAdmin(ctx, CreateAdminParams{
		ID: pgtype.UUID{Bytes: id, Valid: true}, Username: username, PasswordHash: hash,
	})
}

func toAPIInstance(id pgtype.UUID, name, host string, port int32, database, username string, agentVersion pgtype.Text, status api.AlertStatus) api.Instance {
	result := api.Instance{Id: id.Bytes, Name: name, Host: host, Port: int(port), Database: database, Username: username, AlertStatus: status}
	if agentVersion.Valid {
		result.AgentVersion = &agentVersion.String
	}
	return result
}

func alertStatus(ctx context.Context, platform *db.Pool, id pgtype.UUID) (api.AlertStatus, error) {
	status, err := New(platform).GetInstanceAlertStatus(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.OK, nil
	}
	if err != nil {
		return "", err
	}
	return api.AlertStatus(status), nil
}

func metricUnit(metricID string) string {
	return metric.UnitFor(metric.MetricID(metricID))
}

func timePointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func newToken() (string, []byte, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", nil, err
	}
	token := hex.EncodeToString(value)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}

func unauthorizedLogin() api.CreateSession401JSONResponse {
	return api.CreateSession401JSONResponse(errorBody(api.UNAUTHENTICATED, "invalid credentials"))
}

func unauthorizedAgent() api.ReportAgentMetrics401JSONResponse {
	return api.ReportAgentMetrics401JSONResponse(errorBody(api.UNAUTHENTICATED, "invalid agent token"))
}

func badAgentRequest(message string) api.ReportAgentMetrics400JSONResponse {
	return api.ReportAgentMetrics400JSONResponse(errorBody(api.VALIDATIONFAILED, message))
}

func errorBody(code api.ErrorErrorCode, message string) api.Error {
	body := api.Error{}
	body.Error.Code = code
	body.Error.Message = message
	return body
}
