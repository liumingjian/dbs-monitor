# RT-C reproduction

This directory measures the three PostgreSQL storage thresholds assigned to T11. It is intentionally not part of `make check` or `make check-full`: the reference run loads about 453.6 million points and requires a disposable PostgreSQL 17 database.

## Reference shape

- 50 instances
- 35 series per instance (`1,750` total)
- one point per series every 10 seconds (`8,640` per day)
- 30 UTC days (`453,600,000` total points)
- one real series queried for 24 hours with the production one-minute `date_bin` shape
- 100 warmed query samples
- 1,000 real `instance_collect_state` read/write transactions, first idle and then under independent sample-write pressure

The three T11 thresholds are query P95 `<= 500 ms`, total partition size `<= 30%` of the 200 GB delivery disk and `<= 100 GB`, and control transaction P95 degradation `<= 2x`. The missing-partition SQLSTATE is already pinned to `23514` by `migrations/migrate_test.go`.

## Run

Create a disposable database, run the migrations, ensure its data filesystem has enough free space, then explicitly acknowledge the destructive load:

The script verifies PostgreSQL 17, executes the missing-partition probe against the measured database, and requires `RT_C_DATA_PATH` to equal that server's `data_directory`. The path must be local to the machine running the script so the pre-load free-space check and disk evidence describe the real PostgreSQL filesystem.

```sh
RT_C_DATABASE_URL='postgres://.../disposable_rt_c?sslmode=disable' \
RT_C_DATA_PATH=/path/from/show-data-directory \
RT_C_CONFIRM=load-450m-points \
sh scripts/rt-c/run.sh
```

The script refuses a database whose `metric_sample` table is not empty. Override knobs exist for a non-reference rehearsal, but only the defaults count as T11 evidence:

```sh
RT_C_DAYS=1 RT_C_SERIES=35 RT_C_QUERY_RUNS=30 RT_C_CONTROL_RUNS=100 \
RT_C_DATABASE_URL='postgres://.../disposable_rt_c?sslmode=disable' \
RT_C_DATA_PATH=/path/from/show-data-directory \
RT_C_DELIVERY_DISK_BYTES=1000000000 \
RT_C_CONFIRM=load-450m-points \
sh scripts/rt-c/run.sh
```

Each run writes an immutable timestamped directory under `results/rt-c/` containing the environment manifest, PostgreSQL version/settings, actual row counts, raw pgbench logs, query SQL, `EXPLAIN (ANALYZE, BUFFERS)`, per-partition heap/index/total sizes, and `summary.json`. The result is only `PASS` or `BREACHED`; a breach must follow T10 D9 rather than changing the storage engine in this script.
