package metric_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestEveryServerMetricIsProducedExactlyOnce(t *testing.T) {
	knownMetrics := make(map[metric.MetricID]struct{}, len(metric.Metrics))
	for _, item := range metric.Metrics {
		if _, exists := knownMetrics[item.ID]; exists {
			t.Fatalf("metric %q is declared more than once", item.ID)
		}
		knownMetrics[item.ID] = struct{}{}
	}

	producedBy := make(map[metric.MetricID][]metric.TaskID)
	for _, task := range metric.Tasks {
		for _, yield := range task.Yields {
			if _, exists := knownMetrics[yield.Metric]; !exists {
				t.Errorf("task %q yields undeclared metric %q", task.ID, yield.Metric)
			}
			if len(yield.Columns) == 0 {
				t.Errorf("task %q yield %q has no source columns", task.ID, yield.Metric)
			}
			declaredDimensions := metricDimensions(yield.Metric)
			for _, dimension := range yield.Dimensions {
				if !contains(yield.Columns, dimension) {
					t.Errorf("task %q yield %q dimension %q is not a source column", task.ID, yield.Metric, dimension)
				}
				if !contains(declaredDimensions, dimension) {
					t.Errorf("task %q yield %q dimension %q is not declared by the metric", task.ID, yield.Metric, dimension)
				}
			}
			producedBy[yield.Metric] = append(producedBy[yield.Metric], task.ID)
		}
	}

	tests := make([]struct {
		name     string
		producer metric.MetricProducer
		got      []metric.TaskID
	}, 0, len(metric.Metrics))
	for _, item := range metric.Metrics {
		tests = append(tests, struct {
			name     string
			producer metric.MetricProducer
			got      []metric.TaskID
		}{name: string(item.ID), producer: item.Producer, got: producedBy[item.ID]})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.producer {
			case metric.ProducerServerTask:
				if len(tt.got) != 1 {
					t.Errorf("server metric is produced by %v, want exactly one task", tt.got)
				}
			case metric.ProducerAgent, metric.ProducerControlPlane:
				if len(tt.got) != 0 {
					t.Errorf("non-server metric is produced by %v, want no server task", tt.got)
				}
			default:
				t.Errorf("unknown producer %q", tt.producer)
			}
		})
	}
}

func TestTasksUseClosedCapabilitiesAndValidIntervals(t *testing.T) {
	known := make(map[metric.CapabilityID]struct{}, len(metric.Capabilities))
	for _, capability := range metric.Capabilities {
		if _, exists := known[capability.ID]; exists {
			t.Fatalf("capability %q is declared more than once", capability.ID)
		}
		known[capability.ID] = struct{}{}
		if capability.Probe == "" {
			t.Errorf("capability %q has no probe", capability.ID)
		}
	}

	seenTasks := make(map[metric.TaskID]struct{}, len(metric.Tasks))
	for _, task := range metric.Tasks {
		if _, exists := seenTasks[task.ID]; exists {
			t.Fatalf("task %q is declared more than once", task.ID)
		}
		seenTasks[task.ID] = struct{}{}
		if task.SQL == "" {
			t.Errorf("task %q has no SQL", task.ID)
		}
		if task.Interval < metric.MinimumTaskInterval {
			t.Errorf("task %q interval = %s, below minimum %s", task.ID, task.Interval, metric.MinimumTaskInterval)
		}
		for _, required := range task.Requires {
			if _, exists := known[required]; !exists {
				t.Errorf("task %q requires undeclared capability %q", task.ID, required)
			}
		}
	}

	if opportunity, ok := taskByID(metric.TaskQueryStatistics); !ok {
		t.Fatalf("opportunity task %q is missing", metric.TaskQueryStatistics)
	} else if len(opportunity.Yields) != 0 {
		t.Fatalf("opportunity task %q yields P0 metrics: %+v", opportunity.ID, opportunity.Yields)
	}
}

func TestMetricListsMatchGeneratedContracts(t *testing.T) {
	doc := loadSpec(t)
	capabilitySchema := doc.Components.Schemas["CapabilitySnapshotEntry"]
	if capabilitySchema == nil || capabilitySchema.Value == nil {
		t.Fatal("CapabilitySnapshotEntry schema is missing")
	}
	capabilityIDProperty := capabilitySchema.Value.Properties["capability_id"]
	if capabilityIDProperty == nil || capabilityIDProperty.Value == nil {
		t.Fatal("CapabilitySnapshotEntry.capability_id schema is missing")
	}
	assertSetEqual(t, "capability enum", capabilityIDs(), enumStrings(capabilityIDProperty.Value.Enum))

	collectionSchema := doc.Components.Schemas["CollectionTaskState"]
	if collectionSchema == nil || collectionSchema.Value == nil {
		t.Fatal("CollectionTaskState schema is missing")
	}
	taskIDProperty := collectionSchema.Value.Properties["task_id"]
	if taskIDProperty == nil || taskIDProperty.Value == nil {
		t.Fatal("CollectionTaskState.task_id schema is missing")
	}
	assertSetEqual(t, "collection task enum", taskIDs(), enumStrings(taskIDProperty.Value.Enum))

	operation := doc.Paths.Find("/api/v1/instances/{id}/metrics/series").GetOperation("GET")
	if operation == nil {
		t.Fatal("metric series GET operation is missing")
	}
	metricParameter := operation.Parameters.GetByInAndName("query", "metric")
	if metricParameter == nil || metricParameter.Schema == nil || metricParameter.Schema.Value == nil {
		t.Fatal("metric query parameter schema is missing")
	}
	assertSetEqual(t, "metric query enum", metricIDs(), enumStrings(metricParameter.Schema.Value.Items.Value.Enum))

	agentSchema := doc.Components.Schemas["AgentReport"]
	if agentSchema == nil || agentSchema.Value == nil {
		t.Fatal("AgentReport schema is missing")
	}
	metricsProperty := agentSchema.Value.Properties["metrics"]
	if metricsProperty == nil || metricsProperty.Value == nil || metricsProperty.Value.Items == nil || metricsProperty.Value.Items.Value == nil {
		t.Fatal("AgentReport.metrics schema is missing")
	}
	metricProperty := metricsProperty.Value.Items.Value.Properties["metric"]
	if metricProperty == nil || metricProperty.Value == nil {
		t.Fatal("AgentReport.metrics[].metric schema is missing")
	}
	assertSetEqual(t, "agent metric enum", agentMetricIDs(), enumStrings(metricProperty.Value.Enum))

	assertEnumMatches(t, doc, "MetricEngine", engineValues())
	assertEnumMatches(t, doc, "SemanticSlot", semanticSlotIDs())
	assertEnumMatches(t, doc, "MetricLevel", metricLevelValues())
	assertEnumMatches(t, doc, "MetricAggregation", metricAggregationValues())

	catalogSchema := doc.Components.Schemas["MetricCatalogEntry"]
	if catalogSchema == nil || catalogSchema.Value == nil {
		t.Fatal("MetricCatalogEntry schema is missing")
	}
	for _, property := range []string{"metric_id", "engine", "unit", "display_name", "semantic_slot", "level", "aggregation"} {
		if catalogSchema.Value.Properties[property] == nil {
			t.Errorf("MetricCatalogEntry is missing the %q property", property)
		}
	}
}

// 指标目录已经从 metric_id 的 CHECK 枚举搬进 metric_catalog，行由 migrations 从这份字典同步；
// 因此这里改成盯住对外契约里的四个枚举，DDL 与字典是否一致由 migrations 的集成测试断言。
func assertEnumMatches(t *testing.T, doc *openapi3.T, schemaName string, want []string) {
	t.Helper()
	schema := doc.Components.Schemas[schemaName]
	if schema == nil || schema.Value == nil {
		t.Fatalf("%s schema is missing", schemaName)
	}
	assertSetEqual(t, schemaName+" enum", want, enumStrings(schema.Value.Enum))
}

func semanticSlotIDs() []string {
	ids := make([]string, 0, len(metric.SemanticSlots))
	for _, slot := range metric.SemanticSlots {
		ids = append(ids, string(slot.ID))
	}
	return ids
}

func engineValues() []string {
	values := make([]string, 0, len(metric.Engines))
	for _, engine := range metric.Engines {
		values = append(values, string(engine))
	}
	return values
}

func metricLevelValues() []string {
	values := make([]string, 0, len(metric.MetricLevels))
	for _, level := range metric.MetricLevels {
		values = append(values, string(level))
	}
	return values
}

func metricAggregationValues() []string {
	values := make([]string, 0, len(metric.MetricAggregations))
	for _, aggregation := range metric.MetricAggregations {
		values = append(values, string(aggregation))
	}
	return values
}

func metricIDs() []string {
	ids := make([]string, 0, len(metric.Metrics))
	for _, item := range metric.Metrics {
		ids = append(ids, string(item.ID))
	}
	return ids
}

func agentMetricIDs() []string {
	ids := make([]string, 0)
	for _, item := range metric.Metrics {
		if item.Producer == metric.ProducerAgent {
			ids = append(ids, string(item.ID))
		}
	}
	return ids
}

func taskIDs() []string {
	ids := make([]string, 0, len(metric.Tasks))
	for _, task := range metric.Tasks {
		ids = append(ids, string(task.ID))
	}
	return ids
}

func capabilityIDs() []string {
	ids := make([]string, 0, len(metric.Capabilities))
	for _, capability := range metric.Capabilities {
		ids = append(ids, string(capability.ID))
	}
	return ids
}

func enumStrings(values []interface{}) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			panic("OpenAPI enum contains a non-string value")
		}
		result = append(result, text)
	}
	return result
}

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile(filepath.Join(projectRoot(t), "api/openapi.bundled.yaml"))
	if err != nil {
		t.Fatalf("load OpenAPI spec: %v", err)
	}
	return doc
}

func taskByID(id metric.TaskID) (metric.Task, bool) {
	for _, task := range metric.Tasks {
		if task.ID == id {
			return task, true
		}
	}
	return metric.Task{}, false
}

func metricDimensions(id metric.MetricID) []string {
	for _, item := range metric.Metrics {
		if item.ID == id {
			return item.Dimensions
		}
	}
	return nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertSetEqual(t *testing.T, name string, want, got []string) {
	t.Helper()
	want = append([]string(nil), want...)
	got = append([]string(nil), got...)
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
}
