CREATE ROLE sec8_monitor LOGIN PASSWORD 'sec-8-instance-credential-secret';
GRANT pg_monitor TO sec8_monitor;
GRANT CONNECT ON DATABASE monitored TO sec8_monitor;
