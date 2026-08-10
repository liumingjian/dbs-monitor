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
)

const sessionCookie = "dbs_monitor_session"

type authenticatedAgentKey struct{}

type Handler struct {
	platform *db.Pool
	clock    clock.Clock
	keyring  *instance.CredentialKeyring
}

func NewHandler(platform *db.Pool, currentClock clock.Clock, keyring *instance.CredentialKeyring) *Handler {
	return &Handler{platform: platform, clock: currentClock, keyring: keyring}
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
		response = append(response, toAPIInstance(row.ID, row.Name, row.Host, row.Port, row.DatabaseName, row.Username, status))
	}
	return response, nil
}

func (handler *Handler) CreateInstance(ctx context.Context, request api.CreateInstanceRequestObject) (api.CreateInstanceResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("instance body is required")
	}
	id := uuid.New()
	ciphertext, keyVersion, err := handler.keyring.EncryptPassword(id, request.Body.Password)
	if err != nil {
		return nil, err
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return nil, err
	}
	row, err := instance.New(handler.platform).CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgtype.UUID{Bytes: id, Valid: true}, Name: request.Body.Name,
		Host: request.Body.Host, Port: int32(request.Body.Port), DatabaseName: request.Body.Database,
		Username: request.Body.Username, PasswordCiphertext: ciphertext,
		PasswordKeyVersion: keyVersion, AgentTokenHash: tokenHash,
	})
	if err != nil {
		return nil, err
	}
	return api.CreateInstance201JSONResponse{
		AgentToken: token,
		Instance:   toAPIInstance(row.ID, row.Name, row.Host, row.Port, row.DatabaseName, row.Username, api.OK),
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
	return api.GetInstance200JSONResponse(toAPIInstance(row.ID, row.Name, row.Host, row.Port, row.DatabaseName, row.Username, status)), nil
}

func (handler *Handler) UpdateInstance(ctx context.Context, request api.UpdateInstanceRequestObject) (api.UpdateInstanceResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("instance body is required")
	}
	id := request.Id
	ciphertext, keyVersion, err := handler.keyring.EncryptPassword(id, request.Body.Password)
	if err != nil {
		return nil, err
	}
	row, err := instance.New(handler.platform).UpdateInstance(ctx, instance.UpdateInstanceParams{
		ID: pgtype.UUID{Bytes: request.Id, Valid: true}, Name: request.Body.Name,
		Host: request.Body.Host, Port: int32(request.Body.Port), DatabaseName: request.Body.Database,
		Username: request.Body.Username, PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	})
	if err != nil {
		return nil, err
	}
	status, err := alertStatus(ctx, handler.platform, row.ID)
	if err != nil {
		return nil, err
	}
	return api.UpdateInstance200JSONResponse(toAPIInstance(row.ID, row.Name, row.Host, row.Port, row.DatabaseName, row.Username, status)), nil
}

func (handler *Handler) DeleteInstance(ctx context.Context, request api.DeleteInstanceRequestObject) (api.DeleteInstanceResponseObject, error) {
	if err := instance.New(handler.platform).DeleteInstance(ctx, pgtype.UUID{Bytes: request.Id, Valid: true}); err != nil {
		return nil, err
	}
	return api.DeleteInstance204Response{}, nil
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
			state, err := instance.New(handler.platform).GetCollectState(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
			if err == nil && state.LastErrorCode.Valid && state.LastErrorCode.String == string(api.DBUNREACHABLE) {
				entry.Unavailability = nullable.NewNullableWithValue(api.DBUNREACHABLE)
				result.Metrics = append(result.Metrics, entry)
				continue
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
	now := handler.clock.Now().UTC()
	if delta := request.Body.Timestamp.Sub(now); delta > 30*time.Second || delta < -30*time.Second {
		return badAgentRequest("timestamp must be within 30 seconds of server time"), nil
	}

	write := func() error {
		return handler.platform.InTx(ctx, func(tx pgx.Tx) error {
			queries := metric.New(tx)
			for _, sample := range request.Body.Metrics {
				seriesID, err := queries.UpsertSeries(ctx, metric.UpsertSeriesParams{
					InstanceID: pgtype.UUID{Bytes: authenticated, Valid: true}, MetricID: string(sample.Metric),
					Labels: json.RawMessage(`{}`), LabelsKey: "{}",
					LastSeen: pgtype.Timestamptz{Time: request.Body.Timestamp, Valid: true},
				})
				if err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, $3)", seriesID, request.Body.Timestamp, sample.Value); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `INSERT INTO instance_collect_state (instance_id, source, last_report_at)
				VALUES ($1, 'AGENT', $2) ON CONFLICT (instance_id, source)
				DO UPDATE SET last_report_at = EXCLUDED.last_report_at, last_error_code = NULL, last_error_message = NULL`, authenticated, request.Body.Timestamp)
			return err
		})
	}
	if err := write(); err != nil {
		if !metric.IsMissingPartition(err) {
			return nil, err
		}
		if err := metric.EnsurePartitions(ctx, handler.platform, now); err != nil {
			return nil, err
		}
		if err := write(); err != nil {
			return nil, err
		}
	}
	return api.ReportAgentMetrics204Response{}, nil
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
		role, err := New(handler.platform).GetSessionRole(ctx, GetSessionRoleParams{
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
		if roleRank(role) < roleRank(required) {
			writer.WriteHeader(http.StatusForbidden)
			return nil, nil
		}
		return next(ctx, writer, request, value)
	}
}

var RequiredRoles = map[string]string{
	"CreateSession": "READONLY", "ReportAgentMetrics": "AGENT",
	"ListInstances": "READONLY", "GetInstance": "READONLY", "GetMetricSeries": "READONLY",
	"CreateInstance": "PLATFORM_ADMIN", "UpdateInstance": "PLATFORM_ADMIN", "DeleteInstance": "PLATFORM_ADMIN",
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

func toAPIInstance(id pgtype.UUID, name, host string, port int32, database, username string, status api.AlertStatus) api.Instance {
	return api.Instance{Id: id.Bytes, Name: name, Host: host, Port: int(port), Database: database, Username: username, AlertStatus: status}
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
