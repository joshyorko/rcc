#!/usr/bin/env bash
#
# primeagen-impl-context.sh - Gather implementation context for the Primeagen engineer agent
#
# "The best code is code that doesn't exist." - ThePrimeagen
#
# This script collects performance, complexity, and implementation context
# for the Go Primeagen Engineer agent. It focuses on shipping code fast
# while identifying performance pitfalls and unnecessary complexity.
#
# Usage:
#   ./primeagen-impl-context.sh [--json] [--base <branch>] [--files <file1,file2,...>] [--profile]
#
# Options:
#   --json          Output as JSON (default: human-readable)
#   --base <branch> Compare against this branch (default: main)
#   --files <list>  Comma-separated list of specific files to analyze
#   --profile       Run profiling analysis (slower but more detailed)
#   --help          Show this help message
#

set -euo pipefail

# Colors for human-readable output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Configuration
OUTPUT_FORMAT="human"
BASE_BRANCH="main"
SPECIFIC_FILES=""
RUN_PROFILE=false
REPO_ROOT=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --json)
            OUTPUT_FORMAT="json"
            shift
            ;;
        --base)
            BASE_BRANCH="$2"
            shift 2
            ;;
        --files)
            SPECIFIC_FILES="$2"
            shift 2
            ;;
        --profile)
            RUN_PROFILE=true
            shift
            ;;
        --help|-h)
            head -25 "$0" | tail -18
            exit 0
            ;;
        *)
            echo "Unknown option: $1" >&2
            exit 1
            ;;
    esac
done

# Find repo root
find_repo_root() {
    local dir="$PWD"
    while [[ "$dir" != "/" ]]; do
        if [[ -d "$dir/.git" ]] && [[ -f "$dir/go.mod" ]]; then
            echo "$dir"
            return 0
        fi
        dir="$(dirname "$dir")"
    done
    echo "Error: Not in a Go repository with git" >&2
    exit 1
}

REPO_ROOT=$(find_repo_root)
cd "$REPO_ROOT"

# ============================================================================
# Data Collection Functions
# ============================================================================

get_changed_go_files() {
    if [[ -n "$SPECIFIC_FILES" ]]; then
        echo "$SPECIFIC_FILES" | tr ',' '\n' | grep '\.go$' || true
    else
        git diff --name-only "${BASE_BRANCH}...HEAD" 2>/dev/null | grep '\.go$' || \
        git diff --name-only HEAD~1 2>/dev/null | grep '\.go$' || true
    fi
}

# Performance: Check for allocation-heavy patterns
check_allocations() {
    local files="$1"
    local issues=""

    for file in $files; do
        if [[ -f "$file" ]]; then
            # String concatenation in loops
            local concat_in_loop
            concat_in_loop=$(grep -n '\+=' "$file" 2>/dev/null | grep -E 'string|str' || true)
            if [[ -n "$concat_in_loop" ]]; then
                issues="$issues
$file: String concatenation (use strings.Builder):
$concat_in_loop"
            fi

            # Repeated make() calls that could be pooled
            local make_calls
            make_calls=$(grep -c 'make(\[\]byte' "$file" 2>/dev/null | tr -d '[:space:]' || echo "0")
            if [[ "$make_calls" =~ ^[0-9]+$ ]] && [[ "$make_calls" -gt 3 ]]; then
                issues="$issues
$file: Multiple []byte allocations ($make_calls) - consider sync.Pool"
            fi

            # append without pre-allocation
            local append_without_cap
            append_without_cap=$(grep -nE 'make\(\[\][a-zA-Z]+,\s*0\s*\)' "$file" 2>/dev/null || true)
            if [[ -n "$append_without_cap" ]]; then
                issues="$issues
$file: make() without capacity - consider pre-allocation:
$append_without_cap"
            fi
        fi
    done

    echo "$issues"
}

# Complexity: Check for over-engineering
check_complexity() {
    local files="$1"
    local issues=""

    for file in $files; do
        if [[ -f "$file" ]]; then
            # Too many interfaces in one file
            local interface_count
            interface_count=$(grep -c '^type.*interface' "$file" 2>/dev/null | tr -d '[:space:]' || echo "0")
            if [[ "$interface_count" =~ ^[0-9]+$ ]] && [[ "$interface_count" -gt 3 ]]; then
                issues="$issues
$file: Too many interfaces ($interface_count) - \"interfaces should be discovered, not designed\""
            fi

            # Deep nesting (more than 4 levels)
            local deep_nesting
            deep_nesting=$(awk '
                /\{/ { depth++ }
                /\}/ { depth-- }
                depth > 5 { print NR": deep nesting (depth="depth")"; found=1 }
                END { if (!found) exit 1 }
            ' "$file" 2>/dev/null || true)
            if [[ -n "$deep_nesting" ]]; then
                issues="$issues
$file: Deep nesting detected - flatten with early returns:
$deep_nesting"
            fi

            # Functions over 50 lines
            local long_funcs
            long_funcs=$(awk '
                /^func / { start=NR; name=$0 }
                /^\}/ && start {
                    len=NR-start
                    if (len > 50) print start": "substr(name,1,60)" ("len" lines)"
                    start=0
                }
            ' "$file" 2>/dev/null || true)
            if [[ -n "$long_funcs" ]]; then
                issues="$issues
$file: Long functions - \"if it scrolls, it's too long\":
$long_funcs"
            fi
        fi
    done

    echo "$issues"
}

# Simplicity: Check for unnecessary complexity
check_simplicity() {
    local files="$1"
    local issues=""

    for file in $files; do
        if [[ -f "$file" ]]; then
            # Reflection usage (performance anti-pattern)
            if grep -q 'reflect\.' "$file" 2>/dev/null; then
                issues="$issues
$file: Uses reflection - avoid in performance-critical code"
            fi

            # Empty interfaces (any)
            local empty_interface
            empty_interface=$(grep -c 'interface{}' "$file" 2>/dev/null | tr -d '[:space:]' || echo "0")
            if [[ "$empty_interface" =~ ^[0-9]+$ ]] && [[ "$empty_interface" -gt 2 ]]; then
                issues="$issues
$file: Multiple empty interfaces ($empty_interface) - consider type safety"
            fi

            # Channel creation without usage patterns
            local chan_without_select
            chan_without_select=$(grep -c 'make(chan' "$file" 2>/dev/null | tr -d '[:space:]' || echo "0")
            local select_count
            select_count=$(grep -c 'select {' "$file" 2>/dev/null | tr -d '[:space:]' || echo "0")
            if [[ "$chan_without_select" =~ ^[0-9]+$ ]] && [[ "$select_count" =~ ^[0-9]+$ ]]; then
                if [[ "$chan_without_select" -gt "$select_count" ]]; then
                    issues="$issues
$file: Channels without select ($chan_without_select chan, $select_count select) - missing timeout/cancel patterns"
                fi
            fi
        fi
    done

    echo "$issues"
}

# Dependencies: Check for bloat
check_dependencies() {
    local new_deps
    new_deps=""

    if git show "${BASE_BRANCH}:go.mod" &>/dev/null; then
        local base_deps new_deps_list
        base_deps=$(git show "${BASE_BRANCH}:go.mod" 2>/dev/null | grep -E '^\s+[a-z]' | awk '{print $1}' | sort -u)
        new_deps_list=$(cat go.mod | grep -E '^\s+[a-z]' | awk '{print $1}' | sort -u)
        new_deps=$(comm -13 <(echo "$base_deps") <(echo "$new_deps_list") 2>/dev/null || true)
    fi

    echo "$new_deps"
}

# Idioms: Check for non-idiomatic Go
check_idioms() {
    local files="$1"
    local issues=""

    for file in $files; do
        if [[ -f "$file" ]]; then
            # Getter/setter prefixes (Go prefers direct access or simple method names)
            local getters
            getters=$(grep -n 'func.*Get[A-Z]' "$file" 2>/dev/null || true)
            if [[ -n "$getters" ]]; then
                issues="$issues
$file: Uses Get prefix (prefer Name() over GetName()):
$getters"
            fi

            # Long parameter lists
            local long_params
            long_params=$(grep -nE 'func.*\([^)]{100,}\)' "$file" 2>/dev/null || true)
            if [[ -n "$long_params" ]]; then
                issues="$issues
$file: Long parameter lists - consider config struct:
$long_params"
            fi

            # Named return values not used with defer
            local named_returns
            named_returns=$(grep -n 'func.*) (' "$file" 2>/dev/null | head -5 || true)
            if [[ -n "$named_returns" ]]; then
                local has_defer
                has_defer=$(grep -c 'defer.*&' "$file" 2>/dev/null | tr -d '[:space:]' || echo "0")
                if [[ "$has_defer" == "0" ]]; then
                    issues="$issues
$file: Named return values without defer pattern - unnecessary unless using defer"
                fi
            fi
        fi
    done

    echo "$issues"
}

# Tests: Check for missing or poor tests
check_tests() {
    local files="$1"
    local issues=""

    for file in $files; do
        if [[ -f "$file" ]] && [[ "$file" != *"_test.go" ]]; then
            local test_file="${file%.go}_test.go"
            if [[ ! -f "$test_file" ]]; then
                issues="$issues
$file: Missing test file"
            else
                # Check for table-driven tests
                local has_table_tests
                has_table_tests=$(grep -c 'tests := \[\]struct' "$test_file" 2>/dev/null | tr -d '[:space:]' || echo "0")
                if [[ "$has_table_tests" == "0" ]]; then
                    issues="$issues
$test_file: No table-driven tests detected"
                fi
            fi
        fi
    done

    echo "$issues"
}

# Build: Check if it compiles and passes vet
check_build() {
    local build_output vet_output

    build_output=$(go build ./... 2>&1 || true)
    vet_output=$(go vet ./... 2>&1 || true)

    echo "BUILD:$build_output"
    echo "VET:$vet_output"
}

# Count lines changed
count_lines_changed() {
    local files="$1"
    local total=0

    for file in $files; do
        if [[ -f "$file" ]]; then
            local lines
            lines=$(git diff "${BASE_BRANCH}...HEAD" -- "$file" 2>/dev/null | grep -c '^[+-]' | tr -d '[:space:]' || echo "0")
            [[ -z "$lines" ]] && lines=0
            total=$((total + lines))
        fi
    done

    echo "$total"
}

# Ship readiness score (0-100)
calculate_ship_score() {
    local allocation_issues="$1"
    local complexity_issues="$2"
    local simplicity_issues="$3"
    local idiom_issues="$4"
    local test_issues="$5"
    local build_result="$6"

    local score=100

    # Deduct for issues
    [[ -n "$allocation_issues" ]] && score=$((score - 10))
    [[ -n "$complexity_issues" ]] && score=$((score - 15))
    [[ -n "$simplicity_issues" ]] && score=$((score - 10))
    [[ -n "$idiom_issues" ]] && score=$((score - 5))
    [[ -n "$test_issues" ]] && score=$((score - 20))
    [[ "$build_result" == *"error"* ]] && score=$((score - 30))

    [[ $score -lt 0 ]] && score=0
    echo "$score"
}

# ============================================================================
# Main Execution
# ============================================================================

main() {
    local changed_files
    changed_files=$(get_changed_go_files)

    if [[ -z "$changed_files" ]]; then
        if [[ "$OUTPUT_FORMAT" == "json" ]]; then
            echo '{"status": "no_changes", "message": "No Go files changed"}'
        else
            echo -e "${YELLOW}No Go files changed to analyze.${NC}"
        fi
        exit 0
    fi

    # Collect all the data
    local file_count
    file_count=$(echo "$changed_files" | wc -l | xargs)
    local lines_changed
    lines_changed=$(count_lines_changed "$changed_files")
    local allocation_issues
    allocation_issues=$(check_allocations "$changed_files")
    local complexity_issues
    complexity_issues=$(check_complexity "$changed_files")
    local simplicity_issues
    simplicity_issues=$(check_simplicity "$changed_files")
    local idiom_issues
    idiom_issues=$(check_idioms "$changed_files")
    local test_issues
    test_issues=$(check_tests "$changed_files")
    local new_deps
    new_deps=$(check_dependencies)
    local build_result
    build_result=$(check_build)
    local ship_score
    ship_score=$(calculate_ship_score "$allocation_issues" "$complexity_issues" "$simplicity_issues" "$idiom_issues" "$test_issues" "$build_result")

    if [[ "$OUTPUT_FORMAT" == "json" ]]; then
        # JSON output for programmatic consumption
        cat <<EOF
{
  "status": "success",
  "repo_root": "$REPO_ROOT",
  "base_branch": "$BASE_BRANCH",
  "ship_score": $ship_score,
  "summary": {
    "files_changed": $file_count,
    "lines_changed": $lines_changed
  },
  "changed_files": $(echo "$changed_files" | jq -R -s -c 'split("\n") | map(select(length > 0))'),
  "issues": {
    "allocations": $(echo "$allocation_issues" | jq -R -s -c .),
    "complexity": $(echo "$complexity_issues" | jq -R -s -c .),
    "simplicity": $(echo "$simplicity_issues" | jq -R -s -c .),
    "idioms": $(echo "$idiom_issues" | jq -R -s -c .),
    "tests": $(echo "$test_issues" | jq -R -s -c .)
  },
  "dependencies": {
    "new": $(echo "$new_deps" | jq -R -s -c 'split("\n") | map(select(length > 0))')
  }
}
EOF
    else
        # Human-readable output
        echo -e "${BOLD}${MAGENTA}╔══════════════════════════════════════════════════════════════╗${NC}"
        echo -e "${BOLD}${MAGENTA}║    Primeagen Implementation Context - \"Ship it or skip it\"   ║${NC}"
        echo -e "${BOLD}${MAGENTA}╚══════════════════════════════════════════════════════════════╝${NC}"
        echo ""

        # Ship score with color
        local score_color="$GREEN"
        [[ $ship_score -lt 70 ]] && score_color="$YELLOW"
        [[ $ship_score -lt 50 ]] && score_color="$RED"

        echo -e "${BOLD}Ship Score: ${score_color}$ship_score/100${NC}"
        echo ""

        echo -e "${BOLD}📊 Summary${NC}"
        echo -e "   Files changed: ${BLUE}$file_count${NC}"
        echo -e "   Lines changed: ${BLUE}$lines_changed${NC}"
        echo -e "   Base branch:   ${BLUE}$BASE_BRANCH${NC}"
        echo ""

        echo -e "${BOLD}📁 Changed Go Files${NC}"
        echo "$changed_files" | while read -r f; do
            if [[ -n "$f" ]]; then
                echo -e "   ${BLUE}$f${NC}"
            fi
        done
        echo ""

        if [[ -n "$allocation_issues" ]]; then
            echo -e "${BOLD}${RED}🔥 Allocation Issues${NC} (Performance)"
            echo "$allocation_issues" | sed 's/^/   /'
            echo ""
        fi

        if [[ -n "$complexity_issues" ]]; then
            echo -e "${BOLD}${YELLOW}🏗️  Complexity Issues${NC} (Over-engineering)"
            echo "$complexity_issues" | sed 's/^/   /'
            echo ""
        fi

        if [[ -n "$simplicity_issues" ]]; then
            echo -e "${BOLD}${YELLOW}🎯 Simplicity Issues${NC} (KISS violations)"
            echo "$simplicity_issues" | sed 's/^/   /'
            echo ""
        fi

        if [[ -n "$idiom_issues" ]]; then
            echo -e "${BOLD}${CYAN}📐 Idiom Issues${NC} (Non-idiomatic Go)"
            echo "$idiom_issues" | sed 's/^/   /'
            echo ""
        fi

        if [[ -n "$test_issues" ]]; then
            echo -e "${BOLD}${YELLOW}🧪 Test Issues${NC}"
            echo "$test_issues" | sed 's/^/   /'
            echo ""
        fi

        if [[ -n "$new_deps" ]]; then
            echo -e "${BOLD}${RED}📦 New Dependencies${NC} (Each import is a liability)"
            echo "$new_deps" | while read -r dep; do
                if [[ -n "$dep" ]]; then
                    echo -e "   ${RED}$dep${NC}"
                fi
            done
            echo ""
        fi

        echo -e "${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo -e "${BOLD}\"Simplicity is the ultimate sophistication. Ship it.\"${NC}"
        echo -e "${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    fi
}

main "$@"
