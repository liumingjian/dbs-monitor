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
