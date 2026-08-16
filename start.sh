#!/bin/bash

# Start the proxy-subs-backend service
# This script starts the application in daemon mode using nohup

APP_NAME="proxy-subs-backend"
APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_BIN="$APP_DIR/$APP_NAME"
DB_FILE="$APP_DIR/data/proxy-subs.db"
WEB_DIR="$APP_DIR/web"
LOG_FILE="$APP_DIR/nohup.out"

# Check if binary exists
if [ ! -f "$APP_BIN" ]; then
    echo "Error: $APP_BIN not found"
    exit 1
fi

# The web console stays outside the binary so it can be updated independently.
if [ ! -f "$WEB_DIR/index.html" ]; then
    echo "Error: Web console $WEB_DIR/index.html not found"
    exit 1
fi

# Check if already running
if pgrep -f "$APP_NAME" > /dev/null; then
    echo "$APP_NAME is already running"
    exit 0
fi

# Start the application with nohup
echo "Starting $APP_NAME..."
mkdir -p "$APP_DIR/data"
nohup "$APP_BIN" -db "$DB_FILE" -web-dir "$WEB_DIR" > "$LOG_FILE" 2>&1 &

# Get the PID
PID=$!
sleep 1

# Verify if process is still running
if ps -p $PID > /dev/null; then
    echo "$APP_NAME started successfully (PID: $PID)"
    exit 0
else
    echo "Failed to start $APP_NAME. Check $LOG_FILE for details"
    exit 1
fi
