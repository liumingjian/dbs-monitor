package metric_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestMetricDictionaryMatchesP0Overview(t *testing.T) {
	want := readP0Overview(t)
	got := make(map[metric.MetricID]metricFlags, len(metric.Metrics))
	tests := make([]struct {
		name string
		got  metricFlags
		want metricFlags
	}, 0, len(metric.Metrics))
	for _, item := range metric.Metrics {
		flags := metricFlags{
			standard:  item.Standard,
			enhanced:  item.EnhancedCandidate,
			alertable: item.Alertability,
		}
		got[item.ID] = flags
		tests = append(tests, struct {
			name string
			got  metricFlags
			want metricFlags
		}{name: string(item.ID), got: flags, want: want[item.ID]})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Fatalf("metric flags = %+v, want %+v", tt.got, tt.want)
			}
		})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metric dictionary differs from docs/design/01-pg-mvp-metric-dictionary.md §3:\n got: %v\nwant: %v", got, want)
	}
}

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

	for _, ids := range migrationMetricLists(t) {
		assertSetEqual(t, "migration metric constraint", metricIDs(), ids)
	}
}

type metricFlags struct {
	standard  bool
	enhanced  bool
	alertable metric.Alertability
}

func readP0Overview(t *testing.T) map[metric.MetricID]metricFlags {
	t.Helper()
	contents := readProjectFile(t, "docs/design/01-pg-mvp-metric-dictionary.md")
	lines := strings.Split(contents, "\n")
	inside := false
	result := make(map[metric.MetricID]metricFlags)
	for _, line := range lines {
		if strings.TrimSpace(line) == "## 3. P0 指标总览" {
			inside = true
			continue
		}
		if inside && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			break
		}
		if !inside || !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := markdownCells(line)
		if len(cells) != 6 || cells[1] == "指标 ID" || strings.HasPrefix(cells[0], "---") {
			continue
		}
		result[metric.MetricID(strings.Trim(cells[1], "`"))] = metricFlags{
			standard:  cells[3] == "是",
			enhanced:  cells[4] == "是",
			alertable: alertabilityFor(t, cells[5]),
		}
	}
	return result
}

func alertabilityFor(t *testing.T, value string) metric.Alertability {
	t.Helper()
	switch strings.Trim(value, "*") {
	case "是":
		return metric.AlertabilityYes
	case "否", "否，仅辅助展示":
		return metric.AlertabilityNo
	case "视情况":
		return metric.AlertabilityConditional
	default:
		t.Fatalf("unknown alertability value %q", value)
		return ""
	}
}

func markdownCells(line string) []string {
	parts := strings.Split(strings.TrimSpace(line), "|")
	if len(parts) < 3 {
		return nil
	}
	parts = parts[1 : len(parts)-1]
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
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

func migrationMetricLists(t *testing.T) [][]string {
	t.Helper()
	pattern := regexp.MustCompile(`(?s)metric_id\s+IN\s*\((.*?)\)`)
	entries, err := os.ReadDir(filepath.Join(projectRoot(t), "migrations"))
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var result [][]string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		contents := readProjectFile(t, filepath.Join("migrations", entry.Name()))
		for _, match := range pattern.FindAllStringSubmatch(contents, -1) {
			values := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(match[1], -1)
			ids := make([]string, 0, len(values))
			for _, value := range values {
				ids = append(ids, value[1])
			}
			result = append(result, ids)
		}
	}
	if len(result) == 0 {
		t.Fatal("no metric_id IN constraint found in migrations")
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

func readProjectFile(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(projectRoot(t), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}

func projectRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
}
