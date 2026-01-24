#!/bin/bash

# Stop the proxy-subs-backend service

APP_NAME="proxy-subs-backend"

# Try to kill using pkill
if pgrep -f "$APP_NAME" > /dev/null; then
    echo "Stopping $APP_NAME..."
    pkill -f "$APP_NAME"
    
    # Wait a moment for the process to terminate
    sleep 2
    
    # Check if process is still running, force kill if necessary
    if pgrep -f "$APP_NAME" > /dev/null; then
        echo "Force killing $APP_NAME..."
        pkill -9 -f "$APP_NAME"
    fi
    
    echo "$APP_NAME stopped"
else
    echo "$APP_NAME is not running"
fi

exit 0
