#!/bin/bash
# Check Stress Test Metrics from Prometheus
#
# Usage:
#   ./check_metrics.sh [PROMETHEUS_URL]
#
# Example:
#   ./check_metrics.sh http://localhost:9090
#

set -euo pipefail

# Configuration
PROMETHEUS_URL="${1:-http://192.168.57.50:9090}"
API_ENDPOINT="${PROMETHEUS_URL}/api/v1/query"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}    Stress Test Metrics${NC}"
echo -e "${BLUE}======================================${NC}"
echo -e "${GREEN}Prometheus:${NC} $PROMETHEUS_URL"
echo -e "${BLUE}======================================${NC}"
echo ""

# Helper function to query Prometheus
query_prometheus() {
    local query="$1"
    local result
    result=$(curl -s -G "$API_ENDPOINT" --data-urlencode "query=$query" 2>/dev/null)
    echo "$result"
}

# Check if Prometheus is accessible
if ! curl -s "$PROMETHEUS_URL/-/healthy" &>/dev/null; then
    echo -e "${RED}✗ Cannot connect to Prometheus at $PROMETHEUS_URL${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Connected to Prometheus${NC}"
echo ""

# Query sequence violations
echo -e "${YELLOW}==> Sequence Violations${NC}"
VIOLATIONS=$(query_prometheus 'sum(qywx_stress_sequence_violations_total)' | jq -r '.data.result[0].value[1] // "0"')
echo -e "  Total violations: ${RED}$VIOLATIONS${NC}"

if [ "$VIOLATIONS" != "0" ]; then
    echo -e "  ${RED}⚠ Detailed violation info available in logs (not in metrics to avoid high cardinality)${NC}"
    echo -e "  ${YELLOW}Hint: grep \"Sequence violation\" /var/log/qywx/consumer.log${NC}"
fi
echo ""

# Query messages processed
echo -e "${YELLOW}==> Messages Processed${NC}"
TOTAL=$(query_prometheus 'sum(qywx_stress_messages_processed_total)' | jq -r '.data.result[0].value[1] // "0"')
OK=$(query_prometheus 'sum(qywx_stress_messages_processed_total{result="ok"})' | jq -r '.data.result[0].value[1] // "0"')
ERROR=$(query_prometheus 'sum(qywx_stress_messages_processed_total{result="sequence_error"})' | jq -r '.data.result[0].value[1] // "0"')

echo -e "  Total processed:  ${GREEN}$TOTAL${NC}"
echo -e "  Successful (ok):  ${GREEN}$OK${NC}"
echo -e "  Errors:           ${RED}$ERROR${NC}"
echo ""

# Query processing duration
echo -e "${YELLOW}==> Processing Duration (p50, p95, p99)${NC}"
P50=$(query_prometheus 'histogram_quantile(0.50, sum(rate(qywx_stress_processing_duration_seconds_bucket[5m])) by (le))' | jq -r '.data.result[0].value[1] // "N/A"')
P95=$(query_prometheus 'histogram_quantile(0.95, sum(rate(qywx_stress_processing_duration_seconds_bucket[5m])) by (le))' | jq -r '.data.result[0].value[1] // "N/A"')
P99=$(query_prometheus 'histogram_quantile(0.99, sum(rate(qywx_stress_processing_duration_seconds_bucket[5m])) by (le))' | jq -r '.data.result[0].value[1] // "N/A"')

echo -e "  p50: ${GREEN}${P50}s${NC}"
echo -e "  p95: ${YELLOW}${P95}s${NC}"
echo -e "  p99: ${RED}${P99}s${NC}"
echo ""

# Summary
echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}    Summary${NC}"
echo -e "${BLUE}======================================${NC}"

if [ "$VIOLATIONS" == "0" ]; then
    echo -e "${GREEN}✓ No sequence violations detected!${NC}"
else
    echo -e "${RED}✗ Found $VIOLATIONS sequence violations${NC}"
fi

if [ "$TOTAL" != "0" ]; then
    SUCCESS_RATE=$(echo "scale=2; $OK * 100 / $TOTAL" | bc)
    echo -e "${GREEN}Success rate: ${SUCCESS_RATE}%${NC}"
fi

echo -e "${BLUE}======================================${NC}"
