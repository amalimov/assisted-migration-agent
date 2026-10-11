#!/bin/sh
set -eu
mkdir -p /var/lib/agent/v1.4.3/linux_amd64
cp -n /app/extensions/v1.4.3/linux_amd64/excel.duckdb_extension /var/lib/agent/v1.4.3/linux_amd64/
exec /app/agent "$@"
