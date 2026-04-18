#!/bin/bash

# AI Proxy Build Script (Best Practices)
# Features: Versioning, Binary Optimization, Cross-Compilation, Error Handling

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

cd backend || { echo -e "${RED}backend directory not found${NC}"; exit 1; }

# Configuration
APP_NAME="ai-revolver"
OUTPUT_DIR="../build"
MAIN_FILE="main.go"

# Build Metadata
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)

echo -e "${CYAN}--- Building ${APP_NAME} ---${NC}"
echo -e "${CYAN}Version: ${VERSION}${NC}"
echo -e "${CYAN}Commit:  ${COMMIT}${NC}"
echo -e "${CYAN}Time:    ${BUILD_TIME}${NC}"

# Ensure output directory exists
mkdir -p "${OUTPUT_DIR}"

# Go Build Flags
# -s: Omit the symbol table and debug information
# -w: Omit the DWARF symbol table
# These flags significantly reduce binary size
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}"

# 1. Clean previous build
echo -e "${YELLOW}Cleaning old binaries...${NC}"
rm -f "${OUTPUT_DIR}/${APP_NAME}"*

# 2. Linting (New Step)
if [[ "$1" != "--no-test" ]]; then
    echo -e "${YELLOW}Running linters (golangci-lint)...${NC}"
    if command -v golangci-lint &> /dev/null; then
        if ! golangci-lint run; then
            echo -e "${RED}Linting FAILED! Aborting build.${NC}"
            exit 1
        fi
    else
        echo -e "${YELLOW}golangci-lint not found, running via go run (this may take a while)...${NC}"
        if ! go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run; then
            echo -e "${RED}Linting FAILED! Aborting build.${NC}"
            exit 1
        fi
    fi
    echo -e "${GREEN}Linting passed.${NC}"

    # 3. Run Tests
    echo -e "${YELLOW}Running tests...${NC}"
    if ! go test ./... -short; then
        echo -e "${RED}Tests FAILED! Aborting build.${NC}"
        exit 1
    fi
    echo -e "${GREEN}Tests passed.${NC}"
fi

# 4. Perform Build
echo -e "${YELLOW}Building for current OS/Architecture...${NC}"

# CGO_ENABLED=1 because we use modernc.org/sqlite which is a pure Go port, 
# but build-base was required in Dockerfile. We'll stick to CGO_ENABLED=0 if possible 
# for better portability (static binary).
# Since go.mod shows modernc.org/sqlite, it SHOULD work with CGO_ENABLED=0.
CGO_ENABLED=0 go build -ldflags "${LDFLAGS}" -o "${OUTPUT_DIR}/${APP_NAME}" "${MAIN_FILE}"

if [[ $? -eq 0 ]]; then
    echo -e "${GREEN}Build SUCCESSFUL: ${OUTPUT_DIR}/${APP_NAME}${NC}"
    ls -lh "${OUTPUT_DIR}/${APP_NAME}"
else
    echo -e "${RED}Build FAILED!${NC}"
    exit 1
fi

# 5. Cross-Compilation (Example: Linux AMD64)
if [[ "$1" == "--all" ]]; then
    echo -e "${YELLOW}Building for Linux AMD64...${NC}"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "${LDFLAGS}" -o "${OUTPUT_DIR}/${APP_NAME}-linux-amd64" "${MAIN_FILE}"
    
    echo -e "${YELLOW}Building for Windows AMD64...${NC}"
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "${LDFLAGS}" -o "${OUTPUT_DIR}/${APP_NAME}-windows-amd64.exe" "${MAIN_FILE}"
    
    echo -e "${GREEN}All builds complete.${NC}"
fi

echo -e "${CYAN}--- Finished ---${NC}"
