#!/bin/sh
# Required core integration gate. Use disposable PostgreSQL and Redis only.
set -eu
: "${DATABASE_TEST_POSTGRES:?set a disposable PostgreSQL DSN}"
: "${QUEUE_TEST_REDIS:?set a disposable queue Redis URL}"
: "${RATE_LIMIT_TEST_REDIS:?set a disposable limiter Redis URL}"
: "${CACHE_TEST_REDIS:?set a disposable cache Redis URL}"
export APP_ENV_FILE=''
go test -race -count=1 ./...
