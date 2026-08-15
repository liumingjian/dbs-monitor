package platformhealth

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"sync"
	"time"
)

type Status string

const (
	StatusOK       Status = "OK"
	StatusDegraded Status = "DEGRADED"
	StatusFailed   Status = "FAILED"
	StatusUnknown  Status = "UNKNOWN"
)

type Source string

const (
	SourceServerProcess        Source = "SERVER_PROCESS"
	SourcePlatformDatabase     Source = "PLATFORM_DATABASE"
	SourceCollectionScheduler  Source = "COLLECTION_SCHEDULER"
	SourcePartitionMaintenance Source = "PARTITION_MAINTENANCE"
	SourceTLSCertificate       Source = "TLS_CERTIFICATE"
	SourceAgentIngress         Source = "AGENT_INGRESS"
	SourceDisk                 Source = "DISK"
	SourceCredentialKeyring    Source = "CREDENTIAL_KEYRING"
)

type DiskLevel string

const (
	DiskNormal    DiskLevel = "NORMAL"
	DiskWarning   DiskLevel = "WARNING"
	DiskCritical  DiskLevel = "CRITICAL"
	DiskEmergency DiskLevel = "EMERGENCY"
)

type DiskThresholds struct {
	Warning    float64
	Critical   float64
	Emergency  float64
	Hysteresis float64
}

func DefaultDiskThresholds() DiskThresholds {
	return DiskThresholds{
		Warning:    80,
		Critical:   90,
		Emergency:  95,
		Hysteresis: 2,
	}
}

func (thresholds DiskThresholds) Validate() error {
	for _, value := range [...]float64{
		thresholds.Warning,
		thresholds.Critical,
		thresholds.Emergency,
		thresholds.Hysteresis,
	} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("disk thresholds must be finite")
		}
	}
	if thresholds.Warning <= 0 || thresholds.Warning >= thresholds.Critical ||
		thresholds.Critical >= thresholds.Emergency || thresholds.Emergency > 100 {
		return errors.New("disk thresholds must be ordered between 0 and 100")
	}
	if thresholds.Hysteresis < 0 || thresholds.Hysteresis >= thresholds.Warning {
		return errors.New("disk hysteresis must be non-negative and below the warning threshold")
	}
	return nil
}

func ClassifyDiskLevel(usagePercent float64, previousLevel DiskLevel, thresholds DiskThresholds) DiskLevel {
	switch {
	case usagePercent >= thresholds.Emergency:
		return DiskEmergency
	case previousLevel == DiskEmergency && usagePercent >= thresholds.Emergency-thresholds.Hysteresis:
		return DiskEmergency
	case usagePercent >= thresholds.Critical:
		return DiskCritical
	case (previousLevel == DiskEmergency || previousLevel == DiskCritical) && usagePercent >= thresholds.Critical-thresholds.Hysteresis:
		return DiskCritical
	case usagePercent >= thresholds.Warning:
		return DiskWarning
	case previousLevel != DiskNormal && usagePercent >= thresholds.Warning-thresholds.Hysteresis:
		return DiskWarning
	default:
		return DiskNormal
	}
}

var sourceOrder = []Source{
	SourceServerProcess,
	SourcePlatformDatabase,
	SourceCollectionScheduler,
	SourcePartitionMaintenance,
	SourceTLSCertificate,
	SourceAgentIngress,
	SourceDisk,
	SourceCredentialKeyring,
}

type SourceSnapshot struct {
	Source                Source     `json:"source"`
	Status                Status     `json:"status"`
	Code                  string     `json:"code"`
	Version               *string    `json:"version,omitempty"`
	StartedAt             *time.Time `json:"started_at,omitempty"`
	ExpiresAt             *time.Time `json:"expires_at,omitempty"`
	ProbeCapacity         *int       `json:"probe_capacity,omitempty"`
	ProbeActive           *int       `json:"probe_active,omitempty"`
	QueryCapacity         *int       `json:"query_capacity,omitempty"`
	QueryActive           *int       `json:"query_active,omitempty"`
	Pending               *int       `json:"pending,omitempty"`
	SkippedBackpressure   *int64     `json:"skipped_backpressure,omitempty"`
	Backoff               *int64     `json:"backoff,omitempty"`
	ConsecutiveFailures   *int       `json:"consecutive_failures,omitempty"`
	PrebuildDaysRemaining *int       `json:"prebuild_days_remaining,omitempty"`
	DiskLevel             *DiskLevel `json:"disk_level,omitempty"`
	DiskUsagePercent      *float64   `json:"disk_usage_percent,omitempty"`
	DiskWarningPercent    *float64   `json:"disk_warning_percent,omitempty"`
	DiskCriticalPercent   *float64   `json:"disk_critical_percent,omitempty"`
	DiskEmergencyPercent  *float64   `json:"disk_emergency_percent,omitempty"`
	DiskHysteresisPoints  *float64   `json:"disk_hysteresis_points,omitempty"`
}

type Snapshot struct {
	Status      Status           `json:"status"`
	Sources     []SourceSnapshot `json:"sources"`
	AssembledAt time.Time        `json:"assembled_at"`
}

type SchedulerFacts struct {
	ProbeCapacity       int
	ProbeActive         int
	QueryCapacity       int
	QueryActive         int
	Pending             int
	SkippedBackpressure int64
	Backoff             int64
	RefreshFailed       bool
}

type CredentialFacts struct {
	Available   bool
	FailureCode string
}

type PartitionFacts struct {
	ConsecutiveFailures   int
	PrebuildDaysRemaining int
	WriteFailed           bool
}

func AggregateStatus(statuses []Status) Status {
	if len(statuses) == 0 {
		return StatusUnknown
	}
	result := StatusOK
	for _, status := range statuses {
		switch status {
		case StatusFailed:
			return StatusFailed
		case StatusUnknown:
			result = StatusUnknown
		case StatusDegraded:
			if result == StatusOK {
				result = StatusDegraded
			}
		case StatusOK:
		default:
			result = StatusUnknown
		}
	}
	return result
}

func SchedulerSource(facts SchedulerFacts) SourceSnapshot {
	status := StatusOK
	code := "SCHEDULER_RUNNING"
	if facts.RefreshFailed {
		status = StatusFailed
		code = "SCHEDULER_REFRESH_FAILED"
	} else if facts.Pending > 0 || facts.SkippedBackpressure > 0 {
		status = StatusDegraded
		code = "SCHEDULER_BACKPRESSURE"
	}
	return SourceSnapshot{
		Source: SourceCollectionScheduler, Status: status, Code: code,
		ProbeCapacity: intPointer(facts.ProbeCapacity), ProbeActive: intPointer(facts.ProbeActive),
		QueryCapacity: intPointer(facts.QueryCapacity), QueryActive: intPointer(facts.QueryActive),
		Pending: intPointer(facts.Pending), SkippedBackpressure: int64Pointer(facts.SkippedBackpressure),
		Backoff: int64Pointer(facts.Backoff),
	}
}

func CertificateSource(now time.Time, expiresAt *time.Time) SourceSnapshot {
	result := SourceSnapshot{Source: SourceTLSCertificate, Status: StatusUnknown, Code: "CERTIFICATE_UNAVAILABLE"}
	if expiresAt == nil {
		return result
	}
	expires := expiresAt.UTC()
	result.ExpiresAt = &expires
	switch {
	case !now.UTC().Before(expires):
		result.Status = StatusFailed
		result.Code = "CERTIFICATE_EXPIRED"
	case !expires.After(now.UTC().Add(30 * 24 * time.Hour)):
		result.Status = StatusDegraded
		result.Code = "CERTIFICATE_EXPIRING"
	default:
		result.Status = StatusOK
		result.Code = "CERTIFICATE_VALID"
	}
	return result
}

func DatabaseSource(err error) SourceSnapshot {
	if err != nil {
		return SourceSnapshot{Source: SourcePlatformDatabase, Status: StatusFailed, Code: "PLATFORM_DATABASE_UNREACHABLE"}
	}
	return SourceSnapshot{Source: SourcePlatformDatabase, Status: StatusOK, Code: "PLATFORM_DATABASE_REACHABLE"}
}

func CredentialSource(facts CredentialFacts) SourceSnapshot {
	if !facts.Available {
		return SourceSnapshot{Source: SourceCredentialKeyring, Status: StatusUnknown, Code: "CREDENTIAL_KEYRING_UNAVAILABLE"}
	}
	if facts.FailureCode != "" {
		return SourceSnapshot{Source: SourceCredentialKeyring, Status: StatusFailed, Code: facts.FailureCode}
	}
	return SourceSnapshot{Source: SourceCredentialKeyring, Status: StatusOK, Code: "CREDENTIAL_KEYRING_READY"}
}

func PartitionSource(facts PartitionFacts) SourceSnapshot {
	result := SourceSnapshot{
		Source: SourcePartitionMaintenance, Status: StatusOK, Code: "PARTITIONS_READY",
		ConsecutiveFailures: intPointer(facts.ConsecutiveFailures), PrebuildDaysRemaining: intPointer(facts.PrebuildDaysRemaining),
	}
	if facts.WriteFailed {
		result.Status = StatusFailed
		result.Code = "PARTITION_WRITE_FAILED"
	} else if facts.ConsecutiveFailures > 0 || facts.PrebuildDaysRemaining < 7 {
		result.Status = StatusDegraded
		result.Code = "PARTITION_MAINTENANCE_FAILED"
	}
	return result
}

func DiskSource(usagePercent float64, previousLevel DiskLevel, thresholds DiskThresholds) SourceSnapshot {
	if math.IsNaN(usagePercent) || math.IsInf(usagePercent, 0) || usagePercent < 0 || usagePercent > 100 {
		return DiskUnavailableSource(previousLevel)
	}
	level := ClassifyDiskLevel(usagePercent, previousLevel, thresholds)
	result := SourceSnapshot{
		Source:               SourceDisk,
		Status:               StatusOK,
		Code:                 "DISK_NORMAL",
		DiskLevel:            &level,
		DiskUsagePercent:     float64Pointer(usagePercent),
		DiskWarningPercent:   float64Pointer(thresholds.Warning),
		DiskCriticalPercent:  float64Pointer(thresholds.Critical),
		DiskEmergencyPercent: float64Pointer(thresholds.Emergency),
		DiskHysteresisPoints: float64Pointer(thresholds.Hysteresis),
	}
	switch level {
	case DiskWarning:
		result.Status = StatusDegraded
		result.Code = "DISK_WARNING_WATERMARK"
	case DiskCritical:
		result.Status = StatusDegraded
		result.Code = "DISK_CRITICAL_WATERMARK"
	case DiskEmergency:
		result.Status = StatusFailed
		result.Code = "DISK_EMERGENCY_WATERMARK"
	}
	return result
}

func DiskUnavailableSource(lastKnownLevel DiskLevel) SourceSnapshot {
	return SourceSnapshot{
		Source:    SourceDisk,
		Status:    StatusUnknown,
		Code:      "DISK_USAGE_UNAVAILABLE",
		DiskLevel: &lastKnownLevel,
	}
}

type Store struct {
	mu              sync.RWMutex
	sources         map[Source]SourceSnapshot
	snapshot        Snapshot
	logger          *log.Logger
	failureObserver func(FailureFact)
}

type FailureFact struct {
	Source     Source
	Code       string
	ObservedAt time.Time
}

func NewStore(version string, startedAt time.Time, logger *log.Logger) *Store {
	sources := make(map[Source]SourceSnapshot, len(sourceOrder))
	for _, source := range sourceOrder {
		sources[source] = SourceSnapshot{Source: source, Status: StatusUnknown, Code: "FACT_UNAVAILABLE"}
	}
	started := startedAt.UTC()
	sources[SourceServerProcess] = SourceSnapshot{
		Source: SourceServerProcess, Status: StatusOK, Code: "SERVER_PROCESS_RUNNING",
		Version: &version, StartedAt: &started,
	}
	store := &Store{sources: sources, logger: logger}
	store.snapshot = store.assemble(started)
	return store
}

func (store *Store) Update(now time.Time, source SourceSnapshot) {
	store.mu.Lock()
	previous := cloneSnapshot(store.snapshot)
	store.sources[source.Source] = source
	store.snapshot = store.assemble(now.UTC())
	current := cloneSnapshot(store.snapshot)
	observer := store.failureObserver
	store.mu.Unlock()

	store.writeChangeEvent(previous, current)
	observeNewFailures(observer, previous, current)
}

func (store *Store) PublishSummary(now time.Time) {
	store.mu.Lock()
	previous := cloneSnapshot(store.snapshot)
	store.snapshot = store.assemble(now.UTC())
	snapshot := cloneSnapshot(store.snapshot)
	observer := store.failureObserver
	store.mu.Unlock()
	store.writeChangeEvent(previous, snapshot)
	observeNewFailures(observer, previous, snapshot)
	store.writeJournal(summaryEvent{Event: "platform_health_summary", Snapshot: snapshot})
}

func (store *Store) SetFailureObserver(observer func(FailureFact)) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.failureObserver = observer
}

func (store *Store) Current() Snapshot {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return cloneSnapshot(store.snapshot)
}

func (store *Store) Source(source Source) SourceSnapshot {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.sources[source]
}

func (store *Store) DiskLevel() DiskLevel {
	store.mu.RLock()
	defer store.mu.RUnlock()
	level := store.sources[SourceDisk].DiskLevel
	if level == nil {
		return DiskNormal
	}
	return *level
}

func (store *Store) RejectSampleWrites() bool {
	return store.DiskLevel() == DiskEmergency
}

func (store *Store) assemble(now time.Time) Snapshot {
	if certificate := store.sources[SourceTLSCertificate]; certificate.ExpiresAt != nil {
		store.sources[SourceTLSCertificate] = CertificateSource(now, certificate.ExpiresAt)
	}
	sources := make([]SourceSnapshot, 0, len(sourceOrder))
	statuses := make([]Status, 0, len(sourceOrder))
	for _, source := range sourceOrder {
		fact := store.sources[source]
		sources = append(sources, fact)
		statuses = append(statuses, fact.Status)
	}
	return Snapshot{Status: AggregateStatus(statuses), Sources: sources, AssembledAt: now.UTC()}
}

func (store *Store) writeJournal(event any) {
	if store.logger == nil {
		return
	}
	encoded, err := json.Marshal(event)
	if err == nil {
		store.logger.Print(string(encoded))
	}
}

func (store *Store) writeChangeEvent(previous, current Snapshot) {
	changes := changedSources(previous.Sources, current.Sources)
	if len(changes) == 0 {
		return
	}
	store.writeJournal(changeEvent{
		Event:          "platform_health_change",
		AssembledAt:    current.AssembledAt,
		PreviousStatus: previous.Status,
		Status:         current.Status,
		Changes:        changes,
	})
}

type sourceChange struct {
	Source         Source `json:"source"`
	PreviousStatus Status `json:"previous_status"`
	Status         Status `json:"status"`
	Code           string `json:"code"`
}

type changeEvent struct {
	Event          string         `json:"event"`
	AssembledAt    time.Time      `json:"assembled_at"`
	PreviousStatus Status         `json:"previous_status"`
	Status         Status         `json:"status"`
	Changes        []sourceChange `json:"changes"`
}

type summaryEvent struct {
	Event string `json:"event"`
	Snapshot
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	result := snapshot
	result.Sources = append([]SourceSnapshot(nil), snapshot.Sources...)
	return result
}

func changedSources(previous, current []SourceSnapshot) []sourceChange {
	previousBySource := make(map[Source]SourceSnapshot, len(previous))
	for _, source := range previous {
		previousBySource[source.Source] = source
	}
	changes := make([]sourceChange, 0)
	for _, source := range current {
		before := previousBySource[source.Source]
		if before.Status == source.Status && before.Code == source.Code {
			continue
		}
		changes = append(changes, sourceChange{
			Source: source.Source, PreviousStatus: before.Status, Status: source.Status, Code: source.Code,
		})
	}
	return changes
}

func observeNewFailures(observer func(FailureFact), previous, current Snapshot) {
	if observer == nil {
		return
	}
	previousBySource := make(map[Source]Status, len(previous.Sources))
	for _, source := range previous.Sources {
		previousBySource[source.Source] = source.Status
	}
	for _, source := range current.Sources {
		if source.Status == StatusFailed && previousBySource[source.Source] != StatusFailed {
			observer(FailureFact{Source: source.Source, Code: source.Code, ObservedAt: current.AssembledAt})
		}
	}
}

func intPointer(value int) *int             { return &value }
func int64Pointer(value int64) *int64       { return &value }
func float64Pointer(value float64) *float64 { return &value }
