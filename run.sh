#!/bin/bash

# Jotter Docker Runner Script
# This script makes it easy to run Jotter with a custom jots directory

set -e

# Default values
JOTS_DIR="${JOTS_DIR:-./jots}"
PORT="${JOT_PORT:-7708}"

# Function to show usage
show_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -d, --dir DIR     Directory to store jots (default: ./jots)"
    echo "  -p, --port PORT   Port to run on (default: 8000)"
    echo "  -h, --help        Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                                   # Use default ./jots directory"
    echo "  $0 -d ~/Documents/my-jots            # Use custom directory"
    echo "  $0 -d /home/user/jots -p 3000        # Custom directory and port"
    echo "  $0 --dir ./my-notes --port 8080      # Long form options"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -d|--dir)
            JOTS_DIR="$2"
            shift 2
            ;;
        -p|--port)
            PORT="$2"
            shift 2
            ;;
        -h|--help)
            show_usage
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            show_usage
            exit 1
            ;;
    esac
done

# Convert relative path to absolute path
if [[ "$JOTS_DIR" != /* ]]; then
    JOTS_DIR="$(cd "$(dirname "$JOTS_DIR")" && pwd)/$(basename "$JOTS_DIR")"
fi

# Create the jots directory if it doesn't exist
mkdir -p "$JOTS_DIR"

echo "Starting Jotter..."
echo "Jots directory: $JOTS_DIR"
echo "Port: $PORT"
echo "Access at: http://localhost:$PORT"
echo ""

# Export environment variables for docker-compose
export JOTS_DIR="$JOTS_DIR"
export JOT_PORT="$PORT"

# Run docker-compose
docker-compose up -d

echo ""
echo "Jotter is running in the background!"
echo "To stop: docker-compose down"
echo "To view logs: docker-compose logs -f jotter"
