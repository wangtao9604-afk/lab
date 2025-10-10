#!/bin/bash
# Stress Test Script for qywx Message Queue
#
# Usage:
#   ./run_stress.sh [TOTAL_REQUESTS] [CONCURRENT]
#
# Examples:
#   ./run_stress.sh              # Default: 10000 requests, 10 concurrent
#   ./run_stress.sh 5000 20      # 5000 requests, 20 concurrent
#

set -euo pipefail

# Configuration
PRODUCER_URL="${PRODUCER_URL:-http://localhost:11112/stress}"
TOTAL_REQUESTS="${1:-10000}"
CONCURRENT="${2:-10}"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Statistics
SUCCESS_COUNT=0
FAILURE_COUNT=0
START_TIME=$(date +%s)

echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}    qywx Stress Test${NC}"
echo -e "${BLUE}======================================${NC}"
echo -e "${GREEN}Target URL:${NC}     $PRODUCER_URL"
echo -e "${GREEN}Total Requests:${NC} $TOTAL_REQUESTS"
echo -e "${GREEN}Concurrency:${NC}    $CONCURRENT"
echo -e "${BLUE}======================================${NC}"
echo ""

# Progress tracking
print_progress() {
    local current=$1
    local total=$2
    local percent=$((current * 100 / total))
    local bar_length=50
    local filled=$((percent * bar_length / 100))
    local empty=$((bar_length - filled))

    printf "\r${YELLOW}Progress:${NC} ["
    printf "%${filled}s" | tr ' ' '='
    printf "%${empty}s" | tr ' ' '-'
    printf "] %3d%% (%d/%d)" "$percent" "$current" "$total"
}

# Send single request
send_request() {
    if curl -s -X POST "$PRODUCER_URL" -o /dev/null -w "%{http_code}" | grep -q "200"; then
        return 0
    else
        return 1
    fi
}

# Main stress test loop
echo -e "${GREEN}Starting stress test...${NC}"
echo ""

for i in $(seq 1 "$TOTAL_REQUESTS"); do
    # Send request in background
    {
        if send_request; then
            ((SUCCESS_COUNT++)) || true
        else
            ((FAILURE_COUNT++)) || true
        fi
    } &

    # Control concurrency
    if [ $((i % CONCURRENT)) -eq 0 ]; then
        wait
        print_progress "$i" "$TOTAL_REQUESTS"
    fi
done

# Wait for remaining requests
wait
print_progress "$TOTAL_REQUESTS" "$TOTAL_REQUESTS"
echo ""

# Calculate statistics
END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))
RPS=$((TOTAL_REQUESTS / DURATION))

echo ""
echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}    Test Results${NC}"
echo -e "${BLUE}======================================${NC}"
echo -e "${GREEN}Total Requests:${NC}  $TOTAL_REQUESTS"
echo -e "${GREEN}Successful:${NC}      $SUCCESS_COUNT"
echo -e "${RED}Failed:${NC}          $FAILURE_COUNT"
echo -e "${GREEN}Duration:${NC}        ${DURATION}s"
echo -e "${GREEN}Requests/sec:${NC}    $RPS"
echo -e "${BLUE}======================================${NC}"
echo ""

if [ "$FAILURE_COUNT" -gt 0 ]; then
    echo -e "${YELLOW}⚠ Some requests failed. Check producer logs for details.${NC}"
    exit 1
else
    echo -e "${GREEN}✓ All requests completed successfully!${NC}"
    exit 0
fi
