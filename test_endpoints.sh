#!/bin/bash

# Test script for cache-manager endpoints
# Run this after starting the server with docker-compose up

BASE_URL="http://localhost:8080"
USER_ID=1

echo "════════════════════════════════════════════════════════════════"
echo "  Cache Manager POC - Endpoint Testing Script"
echo "════════════════════════════════════════════════════════════════"
echo ""

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Helper function to make requests
test_endpoint() {
    local method=$1
    local endpoint=$2
    local description=$3
    
    echo -e "${BLUE}[$method]${NC} $endpoint"
    echo -e "${YELLOW}     → $description${NC}"
    
    if [ "$method" = "GET" ]; then
        curl -s "$BASE_URL$endpoint" | jq '.'
    else
        curl -s -X "$method" "$BASE_URL$endpoint" | jq '.'
    fi
    
    echo ""
}

echo -e "${GREEN}1. Testing Mode-Specific Endpoints${NC}"
echo "───────────────────────────────────────────────────────────────"
echo ""

test_endpoint "GET" "/users/both-levels/$USER_ID" \
    "Fetch user using BOTH L1 and L2 caches"

test_endpoint "GET" "/users/l1-only/$USER_ID" \
    "Fetch user using L1 cache ONLY"

test_endpoint "GET" "/users/l2-only/$USER_ID" \
    "Fetch user using L2 cache ONLY"

echo ""
echo -e "${GREEN}2. Testing Per-Call Override Endpoints${NC}"
echo "───────────────────────────────────────────────────────────────"
echo ""

# Clear cache first
test_endpoint "DELETE" "/cache/clear/$USER_ID" \
    "Clear all caches for user"

test_endpoint "POST" "/users/set-l1-only/$USER_ID" \
    "Set user in L1 ONLY (override)"

test_endpoint "GET" "/cache/stats/$USER_ID" \
    "Check cache status (should be in L1 only)"

test_endpoint "DELETE" "/cache/clear/$USER_ID" \
    "Clear all caches for user"

test_endpoint "POST" "/users/set-l2-only/$USER_ID" \
    "Set user in L2 ONLY (override)"

test_endpoint "GET" "/cache/stats/$USER_ID" \
    "Check cache status (should be in L2 only)"

echo ""
echo -e "${GREEN}3. Testing Cache Warmup Behavior${NC}"
echo "───────────────────────────────────────────────────────────────"
echo ""

test_endpoint "DELETE" "/cache/clear/$USER_ID" \
    "Clear all caches"

test_endpoint "POST" "/users/set-l2-only/$USER_ID" \
    "Set user in L2 only"

echo -e "${YELLOW}     → Now fetching with both-levels mode (should warm L1)${NC}"
test_endpoint "GET" "/users/both-levels/$USER_ID" \
    "Fetch with both-levels (triggers L1 warmup)"

test_endpoint "GET" "/cache/stats/$USER_ID" \
    "Check cache status (L1 should now be warmed)"

echo ""
echo -e "${GREEN}4. Testing Override GET Endpoints${NC}"
echo "───────────────────────────────────────────────────────────────"
echo ""

test_endpoint "DELETE" "/cache/clear/$USER_ID" \
    "Clear all caches"

test_endpoint "GET" "/users/override-l1/$USER_ID" \
    "Fetch and cache ONLY in L1 (override)"

test_endpoint "GET" "/cache/stats/$USER_ID" \
    "Verify only L1 has the data"

test_endpoint "DELETE" "/cache/clear/$USER_ID" \
    "Clear all caches"

test_endpoint "GET" "/users/override-l2/$USER_ID" \
    "Fetch and cache ONLY in L2 (override)"

test_endpoint "GET" "/cache/stats/$USER_ID" \
    "Verify only L2 has the data"

echo ""
echo -e "${GREEN}5. Testing Cache Hit Rates${NC}"
echo "───────────────────────────────────────────────────────────────"
echo ""

test_endpoint "DELETE" "/cache/clear/$USER_ID" \
    "Clear all caches"

echo -e "${YELLOW}First call (cache miss):${NC}"
test_endpoint "GET" "/users/both-levels/$USER_ID" \
    "Should show from_cache: false"

echo -e "${YELLOW}Second call (cache hit from L1):${NC}"
test_endpoint "GET" "/users/both-levels/$USER_ID" \
    "Should show from_cache: true"

echo ""
echo -e "${GREEN}6. Standard Endpoints${NC}"
echo "───────────────────────────────────────────────────────────────"
echo ""

test_endpoint "GET" "/users/$USER_ID" \
    "Standard user fetch (uses both-levels cache)"

test_endpoint "POST" "/users/refresh/$USER_ID" \
    "Refresh user data (clears all caches)"

echo ""
echo "════════════════════════════════════════════════════════════════"
echo "  Testing Complete!"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "Summary of Endpoints:"
echo ""
echo "Mode-Specific:"
echo "  GET /users/both-levels/:id   - Uses both L1 and L2"
echo "  GET /users/l1-only/:id        - Uses L1 only"
echo "  GET /users/l2-only/:id        - Uses L2 only"
echo ""
echo "Per-Call Overrides:"
echo "  GET  /users/override-l1/:id   - Fetch & cache in L1 only"
echo "  GET  /users/override-l2/:id   - Fetch & cache in L2 only"
echo "  POST /users/set-l1-only/:id   - Force set in L1 only"
echo "  POST /users/set-l2-only/:id   - Force set in L2 only"
echo ""
echo "Cache Management:"
echo "  GET    /cache/stats/:id       - View cache status"
echo "  DELETE /cache/clear/:id       - Clear all caches"
echo ""
echo "Standard:"
echo "  GET  /users/:id               - Get user (both-levels)"
echo "  POST /users/refresh/:id       - Refresh & clear cache"
echo ""

