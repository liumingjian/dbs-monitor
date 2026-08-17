#!/bin/sh
# 官方镜像 entrypoint 只追加 host all all all，PostgreSQL 的 all 数据库关键字
# 不匹配 replication 连接；跨容器 pg_basebackup 需要显式放行。
echo 'host replication all all scram-sha-256' >> "$PGDATA/pg_hba.conf"
