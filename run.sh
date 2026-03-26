#!/usr/bin/env bash
# run.sh — Overmind project runner
#
# Usage:
#   ./run.sh test          Run all unit tests (maven test)
#   ./run.sh build         Compile + package fat JAR + Go binary (maven package, skip tests)
#   ./run.sh start         OVERJAVA mode  — Java-primary, Java JAR only on :25565
#   ./run.sh overmind      OVERMIND mode  — Go engine on :19132 + Java JAR on :25565
#   ./run.sh bedrock       Start Go Dragonfly engine only (for development)
#   ./run.sh clean         Remove all build output (maven clean)
#   ./run.sh clean-build   clean + build
#   ./run.sh clean-test    clean + test
#
# The bundled Maven in ./apache-maven-3.9.6 is used automatically.
# Java 21+ and Go 1.24+ are required.

set -euo pipefail

# ── Paths ─────────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MVN="$SCRIPT_DIR/apache-maven-3.9.6/bin/mvn"
SERVER_JAR="$SCRIPT_DIR/JARS/overmind-java-1.0.0.jar"
SERVER_DEV_JAR="$SCRIPT_DIR/JARS/overmind-java-dev-1.0.0.jar"
BEDROCK_BIN="$SCRIPT_DIR/JARS/overmind-bedrock"
BEDROCK_DEV_BIN="$SCRIPT_DIR/JARS/overmind-bedrock-dev"

# ── Colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[overmind]${RESET} $*"; }
success() { echo -e "${GREEN}[overmind]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[overmind]${RESET} $*"; }
error()   { echo -e "${RED}[overmind]${RESET} $*" >&2; }

# ── Guards ────────────────────────────────────────────────────────────────────
check_java() {
    if ! command -v java &>/dev/null; then
        error "java not found on PATH. Java 21+ is required."
        exit 1
    fi
    local ver
    ver=$(java -version 2>&1 | awk -F '"' '/version/ {print $2}' | cut -d. -f1)
    if [[ "$ver" -lt 21 ]]; then
        error "Java 21+ required (found Java $ver)."
        exit 1
    fi
}

check_maven() {
    if [[ ! -x "$MVN" ]]; then
        error "Bundled Maven not found at $MVN"
        error "Re-extract apache-maven-3.9.6 into the project root."
        exit 1
    fi
}

check_go() {
    if ! command -v go &>/dev/null; then
        error "go not found on PATH. Go 1.25+ is required to build the Bedrock engine."
        exit 1
    fi
}

# ── Commands ──────────────────────────────────────────────────────────────────
cmd_clean() {
    info "Cleaning build output..."
    "$MVN" -f "$SCRIPT_DIR/pom.xml" clean
    success "Clean complete."
}

cmd_test() {
    info "Running unit tests..."
    "$MVN" -f "$SCRIPT_DIR/pom.xml" test
    success "All tests passed."
}

cmd_build() {
    info "Building fat JARs (skipping tests)..."
    mkdir -p "$SCRIPT_DIR/JARS"
    "$MVN" -f "$SCRIPT_DIR/pom.xml" package -DskipTests
    success "Build complete — JARs: $SERVER_JAR  $SERVER_DEV_JAR"

    info "Building Go Bedrock engines..."
    check_go
    (cd "$SCRIPT_DIR/bedrock"     && go build -o "$BEDROCK_BIN"     .)
    success "Build complete — bin: $BEDROCK_BIN"
    (cd "$SCRIPT_DIR/bedrock-dev" && go build -o "$BEDROCK_DEV_BIN" .)
    success "Build complete — bin: $BEDROCK_DEV_BIN"
}

cmd_start() {
    if [[ ! -f "$SERVER_JAR" ]]; then
        warn "Server JAR not found. Building first..."
        cmd_build
    fi
    info "Starting Overmind server (OVERJAVA mode)..."
    info "JAR : $SERVER_JAR"
    info "Port: 25565  (HTTP / Minecraft Java / proxied Bedrock)"
    echo ""
    exec java -jar "$SERVER_JAR"
}

cmd_overmind() {
    # Ensure both binaries exist
    if [[ ! -f "$SERVER_JAR" ]] || [[ ! -f "$BEDROCK_BIN" ]]; then
        warn "One or more binaries missing. Building first..."
        cmd_build
    fi

    info "Starting OVERMIND (Bedrock-primary) mode..."
    info "Go engine : $BEDROCK_BIN  → :19132 (Bedrock UDP) + :25566 (bridge)"
    info "Java JAR  : $SERVER_JAR   → :25565 (Java TCP)"
    echo ""

    # Start Go engine in background; capture its PID for cleanup
    "$BEDROCK_BIN" &
    BEDROCK_PID=$!
    info "Dragonfly engine started (PID $BEDROCK_PID)"

    # On EXIT / Ctrl-C / SIGTERM — kill the Go engine too
    trap 'info "Shutting down..."; kill "$BEDROCK_PID" 2>/dev/null; wait "$BEDROCK_PID" 2>/dev/null' EXIT INT TERM

    # Start Java server in foreground (its exit drives the whole lifecycle)
    java -jar "$SERVER_JAR"
}

cmd_bedrock() {
    if [[ ! -f "$BEDROCK_BIN" ]]; then
        warn "Go binary not found. Building first..."
        check_go
        mkdir -p "$SCRIPT_DIR/JARS"
        (cd "$SCRIPT_DIR/bedrock" && go build -o "$BEDROCK_BIN" .)
    fi
    info "Starting Bedrock engine (production, port :19132)..."
    exec "$BEDROCK_BIN"
}

cmd_bedrock_dev() {
    if [[ ! -f "$BEDROCK_DEV_BIN" ]]; then
        warn "bedrock-dev binary not found. Building first..."
        check_go
        mkdir -p "$SCRIPT_DIR/JARS"
        (cd "$SCRIPT_DIR/bedrock-dev" && go build -o "$BEDROCK_DEV_BIN" .)
        success "Build complete — bin: $BEDROCK_DEV_BIN"
    fi
    info "Starting Bedrock engine (dev — plugins + addons enabled, port :19132)..."
    exec "$BEDROCK_DEV_BIN"
}

cmd_overmind_dev() {
    # Ensure both dev binaries exist
    if [[ ! -f "$SERVER_DEV_JAR" ]] || [[ ! -f "$BEDROCK_DEV_BIN" ]]; then
        warn "One or more dev binaries missing. Building first..."
        cmd_build
    fi

    info "Starting OVERMIND-DEV (Bedrock-primary, dev mode)..."
    info "Go engine : $BEDROCK_DEV_BIN  → :19132 (plugins + addons) + :25566 (bridge)"
    info "Java JAR  : $SERVER_DEV_JAR   → :25565 (Java + plugin API)"
    echo ""

    "$BEDROCK_DEV_BIN" &
    BEDROCK_PID=$!
    info "Dragonfly-dev engine started (PID $BEDROCK_PID)"

    trap 'info "Shutting down..."; kill "$BEDROCK_PID" 2>/dev/null; wait "$BEDROCK_PID" 2>/dev/null' EXIT INT TERM

    java -jar "$SERVER_DEV_JAR"
}

usage() {
    echo -e "${BOLD}Overmind runner${RESET}"
    echo ""
    echo "  Production"
    echo "  $0 start           OVERJAVA mode — Java JAR only (:25565)"
    echo "  $0 overmind        OVERMIND mode — Go engine (:19132) + Java JAR (:25565)"
    echo "  $0 bedrock         Bedrock engine only (no Java bridge)"
    echo ""
    echo "  Development (plugins + addons enabled)"
    echo "  $0 overmind-dev    OVERMIND-DEV — bedrock-dev + java-dev (dual-service)"
    echo "  $0 bedrock-dev     Bedrock-dev engine only"
    echo ""
    echo "  Build / CI"
    echo "  $0 build           Build all JARs + Go binaries (skip tests)"
    echo "  $0 test            Run all unit tests"
    echo "  $0 clean           Remove all build output"
    echo "  $0 clean-build     clean + build"
    echo "  $0 clean-test      clean + test"
}

# ── Main ──────────────────────────────────────────────────────────────────────
check_java
check_maven

case "${1:-}" in
    test)           cmd_test ;;
    build)          cmd_build ;;
    start)          cmd_start ;;
    overmind)       cmd_overmind ;;
    overmind-dev)   cmd_overmind_dev ;;
    bedrock)        cmd_bedrock ;;
    bedrock-dev)    cmd_bedrock_dev ;;
    clean)          cmd_clean ;;
    clean-build)    cmd_clean; cmd_build ;;
    clean-test)     cmd_clean; cmd_test ;;
    ""|help|--help|-h) usage ;;
    *)
        error "Unknown command: $1"
        usage
        exit 1
        ;;
esac
