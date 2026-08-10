package metric

import (
	"encoding/json"
	"time"
)

type CapabilityStatus string

const (
	CapabilityPresent       CapabilityStatus = "PRESENT"
	CapabilityMissing       CapabilityStatus = "MISSING"
	CapabilityNotApplicable CapabilityStatus = "NOT_APPLICABLE"
	CapabilityUnknown       CapabilityStatus = "UNKNOWN"

	CapabilitySnapshotTTL = 5 * time.Minute
)

func ProjectCapabilitySnapshot(states map[CapabilityID]CapabilityStatus, observedAt, now time.Time) map[CapabilityID]CapabilityStatus {
	if observedAt.IsZero() || now.Sub(observedAt) > CapabilitySnapshotTTL || !completeCapabilitySnapshot(states) {
		return allCapabilityStates(CapabilityUnknown)
	}
	result := make(map[CapabilityID]CapabilityStatus, len(Capabilities))
	for _, capability := range Capabilities {
		result[capability.ID] = states[capability.ID]
	}
	return result
}

func DecodeCapabilitySnapshot(encoded []byte) (map[CapabilityID]CapabilityStatus, error) {
	var states map[CapabilityID]CapabilityStatus
	if err := json.Unmarshal(encoded, &states); err != nil {
		return nil, err
	}
	return states, nil
}

func CapabilityAffectedMetricCount(capabilityID CapabilityID) int {
	count := 0
	for _, task := range Tasks {
		if !taskRequires(task, capabilityID) {
			continue
		}
		count += len(task.Yields)
	}
	return count
}

func MetricCapabilityBlockReason(metricID MetricID, states map[CapabilityID]CapabilityStatus) (string, bool) {
	for _, task := range Tasks {
		for _, yield := range task.Yields {
			if yield.Metric == metricID {
				return TaskCapabilityBlockReason(task, states)
			}
		}
	}
	return "", false
}

func TaskCapabilityBlockReason(task Task, states map[CapabilityID]CapabilityStatus) (string, bool) {
	for _, required := range task.Requires {
		status := states[required]
		if status == CapabilityPresent {
			continue
		}
		if status == CapabilityMissing {
			switch required {
			case CapabilityRolePGMonitor:
				return "PERMISSION_DENIED", true
			case CapabilityExtensionPGStatStatements:
				return "EXTENSION_MISSING", true
			}
		}
		if status == CapabilityNotApplicable {
			switch required {
			case CapabilityTopologyHasReplication:
				return "NOT_APPLICABLE_ROLE", true
			case CapabilityTopologyHasSlot:
				return "FEATURE_DISABLED", true
			}
		}
		return "COLLECTION_FAILED", true
	}
	return "", false
}

func completeCapabilitySnapshot(states map[CapabilityID]CapabilityStatus) bool {
	if len(states) != len(Capabilities) {
		return false
	}
	for _, capability := range Capabilities {
		switch states[capability.ID] {
		case CapabilityPresent, CapabilityMissing, CapabilityNotApplicable, CapabilityUnknown:
		default:
			return false
		}
	}
	return true
}

func allCapabilityStates(status CapabilityStatus) map[CapabilityID]CapabilityStatus {
	result := make(map[CapabilityID]CapabilityStatus, len(Capabilities))
	for _, capability := range Capabilities {
		result[capability.ID] = status
	}
	return result
}

func taskRequires(task Task, capabilityID CapabilityID) bool {
	for _, required := range task.Requires {
		if required == capabilityID {
			return true
		}
	}
	return false
}
