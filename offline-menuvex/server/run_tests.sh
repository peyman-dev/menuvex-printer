#!/bin/sh
# Runs the sync contract test-suite (SQLite-backed; production uses PostgreSQL).
set -e
cd "$(dirname "$0")"
python3 -m pytest tests -q "$@"
