package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

// 时序读取的两个出口：单实例（工作台）与多实例（列表、总览）。
//
// 两个端点共用 metricSeriesEntries 这一段口径 —— 缺数原因、库级收敛、采集状态判定全在
// 那里做一次。批量端点存在的理由是往返次数：列表一页 50 行原本是 50 个 series 请求，
// 500 台时会打满后端连接池；合成一个请求之后服务端顺序取数，同一时刻只占一条连接。

func (handler *Handler) GetMetricSeries(ctx context.Context, request api.GetMetricSeriesRequestObject) (api.GetMetricSeriesResponseObject, error) {
	step, err := chooseMetricStep(requestedStep(request.Params.Step), request.Params.From, request.Params.To)
	if err != nil {
		return api.GetMetricSeries400JSONResponse(errorBody(api.VALIDATIONFAILED, err.Error())), nil
	}
	entries, err := handler.metricSeriesEntries(
		ctx,
		pgtype.UUID{Bytes: request.Id, Valid: true},
		request.Params.Metric,
		step,
		request.Params.From,
		request.Params.To,
		byDatabase(request.Params.ByDatabase),
	)
	if err != nil {
		return nil, err
	}
	return api.GetMetricSeries200JSONResponse(api.MetricSeriesResponse{
		From: request.Params.From, To: request.Params.To, Step: step.name, Metrics: entries,
	}), nil
}

// GetInstancesMetricSeries 一次取回多台实例的同一批指标。
//
// 语义与单实例端点逐字相同（同一段代码），只是外面多了一层实例循环：请求里给的每一台
// 都会出现在响应里，即使它一个点也没有 —— 调用方按 instance_id 对齐行，缺席会让它分不清
// 「没采到」和「漏发了」。库级指标一律收敛成实例级：列表与总览不按库展开。
//
// **两种寻址方式**：`metric` 是具体指标 ID，`slot` 是语义位。总览与实例列表只许用后者
// （ADR-0001），而位只能在这里解析 —— 一次请求里的实例可以跑在不同引擎上，同一个位在
// 每台上未必是同一个指标。把这一步交给前端就等于把 metric.ResolveSlot 抄一份到界面里，
// 那正是接入第二个引擎时非改不可的那一份。
func (handler *Handler) GetInstancesMetricSeries(ctx context.Context, request api.GetInstancesMetricSeriesRequestObject) (api.GetInstancesMetricSeriesResponseObject, error) {
	step, err := chooseMetricStep(requestedStep(request.Params.Step), request.Params.From, request.Params.To)
	if err != nil {
		return api.GetInstancesMetricSeries400JSONResponse(errorBody(api.VALIDATIONFAILED, err.Error())), nil
	}
	metricIDs := requestedMetrics(request.Params.Metric)
	slots := requestedSlots(request.Params.Slot)
	if len(metricIDs) == 0 && len(slots) == 0 {
		return api.GetInstancesMetricSeries400JSONResponse(
			errorBody(api.VALIDATIONFAILED, "metric 与 slot 至少要给一个"),
		), nil
	}
	engines, err := handler.instanceEngines(ctx, request.Params.InstanceId)
	if err != nil {
		return nil, err
	}
	result := api.InstancesMetricSeriesResponse{
		From: request.Params.From, To: request.Params.To, Step: step.name,
		Instances: make([]api.InstanceMetricSeries, 0, len(request.Params.InstanceId)),
	}
	for _, instanceID := range request.Params.InstanceId {
		entries, err := handler.metricSeriesEntries(
			ctx,
			pgtype.UUID{Bytes: instanceID, Valid: true},
			metricIDs,
			step,
			request.Params.From,
			request.Params.To,
			false,
		)
		if err != nil {
			return nil, err
		}
		slotEntries, err := handler.slotSeriesEntries(
			ctx,
			pgtype.UUID{Bytes: instanceID, Valid: true},
			engines[instanceID],
			slots,
			step,
			request.Params.From,
			request.Params.To,
		)
		if err != nil {
			return nil, err
		}
		result.Instances = append(result.Instances, api.InstanceMetricSeries{
			InstanceId: instanceID, Metrics: append(entries, slotEntries...),
		})
	}
	return api.GetInstancesMetricSeries200JSONResponse(result), nil
}

func requestedMetrics(metrics *[]api.MetricId) []api.MetricId {
	if metrics == nil {
		return nil
	}
	return *metrics
}

func requestedSlots(slots *[]api.SemanticSlot) []api.SemanticSlot {
	if slots == nil {
		return nil
	}
	return *slots
}

// instanceEngines 是这一批实例各自跑的产品。查不到的实例不在表里 —— 调用方据此把位
// 解析成「不适用」，而不是替它猜一个引擎。
func (handler *Handler) instanceEngines(ctx context.Context, instanceIDs []uuid.UUID) (map[uuid.UUID]metric.Engine, error) {
	ids := make([]pgtype.UUID, 0, len(instanceIDs))
	for _, id := range instanceIDs {
		ids = append(ids, pgtype.UUID{Bytes: id, Valid: true})
	}
	rows, err := instance.New(handler.platform).ListInstanceEngines(ctx, ids)
	if err != nil {
		return nil, err
	}
	engines := make(map[uuid.UUID]metric.Engine, len(rows))
	for _, row := range rows {
		engines[uuid.UUID(row.ID.Bytes)] = metric.Engine(row.Engine)
	}
	return engines, nil
}

// slotSeriesEntries 把语义位逐个解析成这台实例上的具体指标，再走与具体指标完全相同的那段码路。
//
// 该引擎没有绑定这个位时，返回的那一条**只带 slot、不带 metric**，并说 NOT_APPLICABLE_ENGINE：
// 「这个引擎上没有这个数」是一个要显式呈现的结论，不是一个空指标 ID，也不是一条空序列 ——
// 后者会在界面上被读成「采到了，是平的」。
func (handler *Handler) slotSeriesEntries(
	ctx context.Context,
	instanceID pgtype.UUID,
	engine metric.Engine,
	slots []api.SemanticSlot,
	step metricStep,
	from, to time.Time,
) ([]api.MetricSeriesEntry, error) {
	entries := make([]api.MetricSeriesEntry, 0, len(slots))
	for _, requestedSlot := range slots {
		slot := metric.SemanticSlot(requestedSlot)
		resolved, err := metric.ResolveSlot(slot, engine)
		if err != nil {
			if errors.Is(err, metric.ErrSlotNotApplicable) {
				entries = append(entries, notApplicableSlotEntry(requestedSlot))
				continue
			}
			return nil, err
		}
		resolvedEntries, err := handler.metricSeriesEntries(
			ctx, instanceID, []api.MetricId{api.MetricId(resolved)}, step, from, to, false,
		)
		if err != nil {
			return nil, err
		}
		for index := range resolvedEntries {
			resolvedEntries[index].Slot = &requestedSlot
		}
		entries = append(entries, resolvedEntries...)
	}
	return entries, nil
}

func notApplicableSlotEntry(slot api.SemanticSlot) api.MetricSeriesEntry {
	return api.MetricSeriesEntry{
		Slot:           &slot,
		Unavailability: nullable.NewNullableWithValue(api.NOTAPPLICABLEENGINE),
		Series: []struct {
			Labels map[string]string `json:"labels"`
			Points [][]*float64      `json:"points"`
		}{},
	}
}

// requestedStep 把两个端点各自的可选 step 参数收敛成一个取值；不给就是 auto。
func requestedStep(step *api.MetricStep) api.MetricStep {
	if step == nil {
		return api.Auto
	}
	return *step
}

// metricSeriesEntries 是两个端点共用的那一段：一台实例、一批指标、一段时间。
func (handler *Handler) metricSeriesEntries(
	ctx context.Context,
	instanceID pgtype.UUID,
	metricIDs []api.MetricId,
	step metricStep,
	from, to time.Time,
	byDatabaseDetail bool,
) ([]api.MetricSeriesEntry, error) {
	pause, err := New(handler.platform).GetCollectionPause(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	queries := metric.New(handler.platform)
	controlPlaneFacts, err := metric.ReadControlPlaneFacts(ctx, handler.platform, instanceID)
	if err != nil {
		return nil, err
	}
	now := handler.clock.Now().UTC()
	agentStatus := metric.AgentStatusAt(controlPlaneFacts, now)
	var capabilityStates map[metric.CapabilityID]metric.CapabilityStatus
	capabilityStatesLoaded := false
	entries := make([]api.MetricSeriesEntry, 0, len(metricIDs))
	for _, requestedMetricID := range metricIDs {
		metricID := metric.MetricID(requestedMetricID)
		metricName := metricID.String()
		entry := api.MetricSeriesEntry{
			Metric:         &metricName,
			Unit:           metricUnit(metricID.String()),
			Unavailability: nullable.NewNullNullable[api.Unavailability](),
			// 明确给空切片而不是 nil：schema 里 series 是必填数组，nil 会编码成 null。
			Series: []struct {
				Labels map[string]string `json:"labels"`
				Points [][]*float64      `json:"points"`
			}{},
		}
		counterReset := false
		if pause.CollectionPaused {
			entry.Unavailability = nullable.NewNullableWithValue(api.COLLECTIONPAUSED)
			entries = append(entries, entry)
			continue
		}

		switch metric.ProducerFor(metricID) {
		case metric.ProducerControlPlane:
			projected, hasProjection := metric.ProjectControlPlaneMetric(metricID, controlPlaneFacts, now)
			if !hasProjection {
				reason := api.NOSAMPLESYET
				if controlPlaneFacts.CollectorLastErrorCode != "" {
					reason = api.COLLECTIONFAILED
				}
				entry.Unavailability = nullable.NewNullableWithValue(reason)
			} else if projected.ObservedAt.Before(from) || projected.ObservedAt.After(to) {
				entry.Unavailability = nullable.NewNullableWithValue(api.NODATAINRANGE)
			} else {
				timestamp, value := float64(projected.ObservedAt.Unix()), projected.Value
				entry.Series = append(entry.Series, struct {
					Labels map[string]string `json:"labels"`
					Points [][]*float64      `json:"points"`
				}{Labels: projected.Labels, Points: [][]*float64{{&timestamp, &value}}})
			}
			entries = append(entries, entry)
			continue
		case metric.ProducerAgent:
			if reason, unavailable := agentMetricUnavailability(controlPlaneFacts, agentStatus); unavailable {
				entry.Unavailability = nullable.NewNullableWithValue(reason)
				entries = append(entries, entry)
				continue
			}
		}

		collectionState := metricCollectionState{}
		// 「这是不是一个由服务端直采、要看目标库连不连得上的指标」由目录回答，不由 ID 前缀。
		// 前缀是一个约定，目录是那个约定的唯一登记处（internal/dbengine 是它的词汇表）；
		// 接入第二个引擎时 `mysql.` 不会自己长进这个 if 里，而漏进来的后果是把「库连不上」
		// 说成「没采到」。引擎无关的指标（host.* / agent.* / collector.*）不走这一段：
		// 它们量的是主机与采集自身，目标库连不连得上与它们无关。
		if engine, catalogued := metric.EngineFor(metricID); catalogued && engine != metric.EngineAgnostic {
			var probeResult pgtype.Text
			err := handler.platform.QueryRow(ctx, `SELECT last_result FROM instance_collection_task_state
				WHERE instance_id = $1 AND task_id = 'pg.probe'`, instanceID).Scan(&probeResult)
			if err == nil && probeResult.Valid {
				switch api.CollectionTaskResult(probeResult.String) {
				case api.FAILED, api.TIMEDOUT:
					entry.Unavailability = nullable.NewNullableWithValue(api.DBUNREACHABLE)
					entries = append(entries, entry)
					continue
				}
			}
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			if !capabilityStatesLoaded {
				capabilityStates, _, err = handler.currentCapabilitySnapshot(ctx, instanceID)
				if err != nil {
					return nil, err
				}
				capabilityStatesLoaded = true
			}
			if reason, blocked := metric.MetricCapabilityBlockReason(metricID, capabilityStates); blocked {
				entry.Unavailability = nullable.NewNullableWithValue(api.Unavailability(reason))
				entries = append(entries, entry)
				continue
			}
			counterReset, err = handler.metricCounterReset(ctx, instanceID, metricID)
			if err != nil {
				return nil, err
			}
			collectionState, err = handler.readMetricCollectionState(
				ctx, instanceID, metricID, controlPlaneFacts.CollectorLastSuccessAt, now, to,
			)
			if err != nil {
				return nil, err
			}
		}

		series, err := readMetricSeries(ctx, queries, instanceID, metricID, step, from, to)
		if err != nil {
			return nil, err
		}
		// 「有没有序列」要在收敛之前数：收敛之后永远是一条，会把「还没采到任何东西」
		// （NO_SAMPLES_YET）说成「这段时间里没有点」（NO_DATA_IN_RANGE）。
		storedSeriesCount := len(series)
		// 库级指标默认收敛成实例级的一条序列：列表与总览只显示实例级值，否则一个连接下
		// 几十个库会把行数以另一种方式撑爆。逐库明细是工作台显式要来的（by_database=true），
		// 那时库名放进 labels，和 replica / slot 一样成为图例上的一维。
		if metric.LevelFor(metricID) == metric.LevelDatabase && !byDatabaseDetail {
			aggregated, err := aggregateSeriesToInstance(ctx, queries, instanceID, metricID, step, from, to, series)
			if err != nil {
				return nil, err
			}
			series = []fetchedSeries{{labels: map[string]string{}, points: aggregated}}
		}
		for _, found := range series {
			if len(found.points) == 0 {
				continue
			}
			item := struct {
				Labels map[string]string `json:"labels"`
				Points [][]*float64      `json:"points"`
			}{Labels: map[string]string{}, Points: make([][]*float64, 0, len(found.points))}
			for key, value := range found.labels {
				item.Labels[key] = value
			}
			if found.databaseName != "" {
				item.Labels[metric.DimensionDatabase] = found.databaseName
			}
			for _, point := range found.points {
				timestamp, value := float64(point.ts.Unix()), point.value
				item.Points = append(item.Points, []*float64{&timestamp, &value})
			}
			entry.Series = append(entry.Series, item)
		}
		if len(entry.Series) == 0 {
			if counterReset {
				entry.Unavailability = nullable.NewNullableWithValue(api.COUNTERRESET)
			} else if collectionState.failed {
				entry.Unavailability = nullable.NewNullableWithValue(api.COLLECTIONFAILED)
			} else if collectionState.stale {
				entry.Unavailability = nullable.NewNullableWithValue(api.STALE)
			} else if storedSeriesCount == 0 {
				entry.Unavailability = nullable.NewNullableWithValue(api.NOSAMPLESYET)
			} else {
				entry.Unavailability = nullable.NewNullableWithValue(api.NODATAINRANGE)
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
