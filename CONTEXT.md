# PostgreSQL Monitoring Platform

This context names the domain facts shared by collection, alerting, and the operator-facing control plane. It deliberately excludes implementation choices such as worker counts, timeout values, and connection-pool settings.

## Language

**Collection task**:
A declared unit of server-direct collection that produces one or more metrics for one monitored instance on its own schedule.
_Avoid_: Metric job, collector query

**Task run**:
The obligation to attempt one collection task for one instance at a particular due time. A missed task run is recorded as missing data and is never replayed as historical observation.
_Avoid_: Global collection round, backlog item

**Reachable**:
The fact that a new database client can connect, authenticate, and complete the lightweight probe within its deadline. It does not imply that an existing monitoring session is usable, nor does an existing usable session imply reachability.
_Avoid_: Database healthy, collector connected

**Collection-source integrity watermark**:
The latest time at which every enabled and applicable collection task for a source had satisfied its most recent due obligation. One successful task cannot advance the watermark while another due task is failed, skipped, or backed off.
_Avoid_: Latest sample time, any-task success time

**PG credential**:
The username and password the server uses to authenticate to one monitored PostgreSQL instance. The username is part of the credential but is not secret; the password is.
_Avoid_: Connection string, DSN

**Agent enrollment**:
The configuration fact that an instance is expected to have an Agent. Explicit token issuance starts enrollment, and only disabling the Agent ends it.
_Avoid_: Token exists, Agent online

**Agent token revocation**:
Invalidating an Agent bearer token while preserving Agent enrollment and the expectation that the Agent remains online.
_Avoid_: Disable Agent

**Agent disablement**:
Ending Agent enrollment for an instance while preserving its historical host samples. It makes Agent-only metrics and rules structurally not applicable.
_Avoid_: Revoke token, Agent offline
