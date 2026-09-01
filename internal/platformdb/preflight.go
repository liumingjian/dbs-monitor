package platformdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/liumingjian/dbs-monitor/internal/db"
)

const requiredPostgresMajorVersion = 17

// Keep this at the PostgreSQL patch level used for release validation.
const recommendedPostgresVersionNum = 170010

type Code string

const (
	CodeVersionUnsupported   Code = "PLATFORM_DATABASE_VERSION_UNSUPPORTED"
	CodeEncodingUnsupported  Code = "PLATFORM_DATABASE_ENCODING_UNSUPPORTED"
	CodeSchemaCreateDenied   Code = "PLATFORM_DATABASE_SCHEMA_CREATE_DENIED"
	CodeSchemaNotClean       Code = "PLATFORM_DATABASE_SCHEMA_NOT_CLEAN"
	CodeTLSInactive          Code = "PLATFORM_DATABASE_TLS_INACTIVE"
	CodeLocaleNonstandard    Code = "PLATFORM_DATABASE_LOCALE_NONSTANDARD"
	CodeTimeZoneNonUTC       Code = "PLATFORM_DATABASE_TIMEZONE_NON_UTC"
	CodeInstanceShared       Code = "PLATFORM_DATABASE_INSTANCE_SHARED"
	CodeMinorVersionOutdated Code = "PLATFORM_DATABASE_MINOR_VERSION_OUTDATED"
)

type Finding struct {
	Code    Code
	Message string
}

type Report struct {
	Fatal    []Finding
	Warnings []Finding
}

func (report Report) FatalError() error {
	if len(report.Fatal) == 0 {
		return nil
	}
	messages := make([]string, 0, len(report.Fatal))
	for _, finding := range report.Fatal {
		messages = append(messages, fmt.Sprintf("%s: %s", finding.Code, finding.Message))
	}
	return fmt.Errorf("platform database prerequisites rejected: %s", strings.Join(messages, "; "))
}

type Facts struct {
	ServerVersionNum   int
	Encoding           string
	CanCreateSchema    bool
	UnexpectedObjects  []string
	TLSActive          bool
	LocaleProvider     string
	Collation          string
	CharacterType      string
	TimeZone           string
	OtherDatabaseCount int
}

func Evaluate(facts Facts) Report {
	var report Report
	majorVersion := postgresMajorVersion(facts.ServerVersionNum)
	if majorVersion != requiredPostgresMajorVersion {
		report.Fatal = append(report.Fatal, Finding{
			Code: CodeVersionUnsupported,
			Message: fmt.Sprintf("PostgreSQL major version is %d (server_version_num=%d); version %d is required",
				majorVersion, facts.ServerVersionNum, requiredPostgresMajorVersion),
		})
	}
	if !strings.EqualFold(facts.Encoding, "UTF8") {
		report.Fatal = append(report.Fatal, Finding{
			Code:    CodeEncodingUnsupported,
			Message: fmt.Sprintf("database encoding is %q; UTF8 is required", facts.Encoding),
		})
	}
	if !facts.CanCreateSchema {
		report.Fatal = append(report.Fatal, Finding{
			Code:    CodeSchemaCreateDenied,
			Message: "current role cannot CREATE in schema dbsmon or the schema does not exist",
		})
	}
	if len(facts.UnexpectedObjects) > 0 {
		report.Fatal = append(report.Fatal, Finding{
			Code:    CodeSchemaNotClean,
			Message: fmt.Sprintf("schema dbsmon contains non-platform objects: %s", strings.Join(facts.UnexpectedObjects, ", ")),
		})
	}
	if !facts.TLSActive {
		report.Fatal = append(report.Fatal, Finding{
			Code:    CodeTLSInactive,
			Message: "pg_stat_ssl reports that the platform database connection is not encrypted",
		})
	}

	if !standardLocale(facts.LocaleProvider, facts.Collation, facts.CharacterType) {
		report.Warnings = append(report.Warnings, Finding{
			Code: CodeLocaleNonstandard,
			Message: fmt.Sprintf("database locale is provider=%q collation=%q ctype=%q; C or POSIX libc locale is recommended",
				facts.LocaleProvider, facts.Collation, facts.CharacterType),
		})
	}
	if !strings.EqualFold(facts.TimeZone, "UTC") {
		report.Warnings = append(report.Warnings, Finding{
			Code:    CodeTimeZoneNonUTC,
			Message: fmt.Sprintf("database session TimeZone is %q; UTC is recommended", facts.TimeZone),
		})
	}
	if facts.OtherDatabaseCount > 0 {
		report.Warnings = append(report.Warnings, Finding{
			Code:    CodeInstanceShared,
			Message: fmt.Sprintf("PostgreSQL instance has %d other non-system database(s); a dedicated instance is recommended", facts.OtherDatabaseCount),
		})
	}
	if majorVersion == requiredPostgresMajorVersion && facts.ServerVersionNum < recommendedPostgresVersionNum {
		report.Warnings = append(report.Warnings, Finding{
			Code: CodeMinorVersionOutdated,
			Message: fmt.Sprintf("PostgreSQL minor version is behind the recommended patch level (server_version_num=%d, recommended>=%d)",
				facts.ServerVersionNum, recommendedPostgresVersionNum),
		})
	}
	return report
}

func postgresMajorVersion(serverVersionNum int) int {
	return serverVersionNum / 10000
}

func standardLocale(provider, collation, characterType string) bool {
	if provider != "c" {
		return false
	}
	isStandard := func(value string) bool {
		return strings.EqualFold(value, "C") || strings.EqualFold(value, "POSIX")
	}
	return isStandard(collation) && isStandard(characterType)
}

func Check(ctx context.Context, database db.DBTX) (Report, error) {
	var facts Facts
	if err := database.QueryRow(ctx, preflightQuery, platformRelations, platformFunctions, platformTriggers).Scan(
		&facts.ServerVersionNum,
		&facts.Encoding,
		&facts.CanCreateSchema,
		&facts.UnexpectedObjects,
		&facts.TLSActive,
		&facts.LocaleProvider,
		&facts.Collation,
		&facts.CharacterType,
		&facts.TimeZone,
		&facts.OtherDatabaseCount,
	); err != nil {
		return Report{}, fmt.Errorf("query platform database prerequisites: %w", err)
	}
	return Evaluate(facts), nil
}

var platformRelations = []string{
	"goose_db_version",
	"goose_db_version_id_seq",
	"app_user",
	"user_session",
	"instance",
	"instance_collect_state",
	"metric_semantic_slot",
	"metric_catalog",
	"metric_series",
	"metric_series_series_id_seq",
	"metric_sample",
	"alert_instance",
	"instance_collection_config",
	"collection_task_config",
	"instance_collection_task_state",
	"instance_collection_connection_state",
	"alert_rule",
	"alert_rule_version",
	"alert_event",
	"alert_event_id_seq",
	"instance_capability_snapshot",
	"alert_rule_scope_instance",
	"alert_rule_evaluation_state",
	"alert_trigger_snapshot",
	"alert_trigger_snapshot_session",
	"long_query_sample_snapshot",
	"long_query_sample",
	"instance_session_snapshot",
	"instance_session_snapshot_entry",
	"instance_identity",
	"performance_event",
	"query_statistics_snapshot",
	"query_statistics_snapshot_entry",
	"notification_policy",
	"alert_rule_template",
	"smtp_channel",
	"notification_delivery",
	"notification_attempt",
	"notification_attempt_id_seq",
	"webhook_target",
	"notification_contact",
	"notification_contact_group",
	"notification_contact_group_member",
	"notification_policy_contact",
	"notification_policy_contact_group",
	"notification_policy_channel",
	"notification_policy_channel_id_seq",
	"maintenance_window",
	"maintenance_window_instance",
	"platform_event",
	"platform_event_id_seq",
}

var platformFunctions = []string{"protect_default_notification_policy"}
var platformTriggers = []string{"notification_policy_default_guard"}

const preflightQuery = `
WITH platform_schema_initialized AS (
    SELECT to_regclass('dbsmon.goose_db_version') IS NOT NULL AS initialized
),
unexpected_objects AS (
    SELECT cls.relname AS name
    FROM pg_class AS cls
    JOIN pg_namespace AS ns ON ns.oid = cls.relnamespace
    WHERE ns.nspname = 'dbsmon'
      AND cls.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
      AND (
          NOT (SELECT initialized FROM platform_schema_initialized)
          OR NOT (cls.relname = ANY ($1::text[]))
      )
      AND NOT (
          cls.relispartition
          AND EXISTS (
              SELECT 1
              FROM pg_inherits
              JOIN pg_class AS parent ON parent.oid = pg_inherits.inhparent
              JOIN pg_namespace AS parent_ns ON parent_ns.oid = parent.relnamespace
              WHERE pg_inherits.inhrelid = cls.oid
                AND parent_ns.nspname = 'dbsmon'
                AND parent.relname = 'metric_sample'
          )
      )
    UNION
    SELECT proc.proname AS name
    FROM pg_proc AS proc
    JOIN pg_namespace AS ns ON ns.oid = proc.pronamespace
    WHERE ns.nspname = 'dbsmon'
      AND (
          NOT (SELECT initialized FROM platform_schema_initialized)
          OR NOT (proc.proname = ANY ($2::text[]))
      )
    UNION
    SELECT typ.typname AS name
    FROM pg_type AS typ
    JOIN pg_namespace AS ns ON ns.oid = typ.typnamespace
    WHERE ns.nspname = 'dbsmon'
      AND typ.typrelid = 0
      AND typ.typelem = 0
    UNION
    SELECT coll.collname AS name
    FROM pg_collation AS coll
    JOIN pg_namespace AS ns ON ns.oid = coll.collnamespace
    WHERE ns.nspname = 'dbsmon'
    UNION
    SELECT trig.tgname AS name
    FROM pg_trigger AS trig
    JOIN pg_class AS cls ON cls.oid = trig.tgrelid
    JOIN pg_namespace AS ns ON ns.oid = cls.relnamespace
    WHERE ns.nspname = 'dbsmon'
      AND NOT trig.tgisinternal
      AND (
          NOT (SELECT initialized FROM platform_schema_initialized)
          OR NOT (trig.tgname = ANY ($3::text[]))
      )
)
SELECT
    current_setting('server_version_num')::integer,
    pg_encoding_to_char(db.encoding),
    CASE
        WHEN to_regnamespace('dbsmon') IS NULL THEN false
        ELSE has_schema_privilege(current_user, 'dbsmon', 'CREATE')
    END,
    ARRAY(SELECT name FROM unexpected_objects ORDER BY name),
    COALESCE((SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()), false),
    db.datlocprovider::text,
    db.datcollate,
    db.datctype,
    current_setting('TimeZone'),
    (
        SELECT count(*)::integer
        FROM pg_database AS other_db
        WHERE other_db.datallowconn
          AND NOT other_db.datistemplate
          AND other_db.datname NOT IN (current_database(), 'postgres')
    )
FROM pg_database AS db
WHERE db.datname = current_database()`
