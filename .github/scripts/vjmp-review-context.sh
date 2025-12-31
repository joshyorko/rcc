#!/usr/bin/env bash
#
# vjmp-review-context.sh - Gather Go code review context for the vjmp reviewer agent
#
# "Always profile, before and after." - Juha P. (vjmp)
#
# This script collects comprehensive context about Go code changes to feed
# into the vjmp tech lead reviewer agent. It performs static analysis,
# dependency auditing, and change detection that would make Jippo proud.
#
# Usage:
#   ./vjmp-review-context.sh [--json] [--base <branch>] [--files <file1,file2,...>]
#
# Options:
#   --json          Output as JSON (default: human-readable)
#   --base <branch> Compare against this branch (default: main)
#   --files <list>  Comma-separated list of specific files to review
#   --staged        Review only staged changes
#   --help          Show this help message
#

set -euo pipefail

# Colors for human-readable output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Configuration
OUTPUT_FORMAT="human"
BASE_BRANCH="main"
SPECIFIC_FILES=""
STAGED_ONLY=false
REPO_ROOT=""

# Dragon directories - areas where Jippo has seen things go wrong
DRAGON_DIRS=("htfs" "conda" "operations" "common" "pathlib" "shell" "blobs")

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
        --staged)
            STAGED_ONLY=true
            shift
            ;;
        --help|-h)
            head -30 "$0" | tail -20
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
    local files=""
    
    if [[ -n "$SPECIFIC_FILES" ]]; then
        echo "$SPECIFIC_FILES" | tr ',' '\n' | grep '\.go$' || true
    elif [[ "$STAGED_ONLY" == "true" ]]; then
        git diff --cached --name-only --diff-filter=ACMR | grep '\.go$' || true
    else
        # Compare against base branch
        git diff --name-only "${BASE_BRANCH}...HEAD" 2>/dev/null | grep '\.go$' || \
        git diff --name-only HEAD~1 2>/dev/null | grep '\.go$' || true
    fi
}

get_new_dependencies() {
    # Check for new imports in go.mod
    local base_deps new_deps
    
    if git show "${BASE_BRANCH}:go.mod" &>/dev/null; then
        base_deps=$(git show "${BASE_BRANCH}:go.mod" 2>/dev/null | grep -E '^\s+[a-z]' | awk '{print $1}' | sort -u)
        new_deps=$(cat go.mod | grep -E '^\s+[a-z]' | awk '{print $1}' | sort -u)
        comm -13 <(echo "$base_deps") <(echo "$new_deps") 2>/dev/null || true
    fi
}

check_gofmt() {
    local files="$1"
    local unformatted=""
    
    for file in $files; do
        if [[ -f "$file" ]]; then
            if ! gofmt -l "$file" | grep -q .; then
                : # File is formatted
            else
                unformatted="$unformatted $file"
            fi
        fi
    done
    
    echo "$unformatted" | xargs
}

check_govet() {
    # Run go vet on changed packages
    local files="$1"
    local packages=""
    
    for file in $files; do
        if [[ -f "$file" ]]; then
            packages="$packages ./$(dirname "$file")/..."
        fi
    done
    
    if [[ -n "$packages" ]]; then
        # shellcheck disable=SC2086
        go vet $packages 2>&1 || true
    fi
}

check_receiver_names() {
    # Find receiver names that aren't "it" - a vjmp code smell
    local files="$1"
    local violations=""
    
    for file in $files; do
        if [[ -f "$file" ]]; then
            # Match func (x *Type) or func (x Type) where x != it
            local bad_receivers
            bad_receivers=$(grep -nE 'func \([a-hj-z][a-zA-Z]* \*?[A-Z]' "$file" 2>/dev/null || true)
            if [[ -n "$bad_receivers" ]]; then
                violations="$violations
$file:
$bad_receivers"
            fi
        fi
    done
    
    echo "$violations"
}

check_fail_pattern() {
    # Check for proper use of fail package
    local files="$1"
    local issues=""
    
    for file in $files; do
        if [[ -f "$file" ]]; then
            # Functions returning error should have named return with defer fail.Around
            local funcs_with_error
            funcs_with_error=$(grep -n 'func.*error\s*{' "$file" 2>/dev/null || true)
            
            if [[ -n "$funcs_with_error" ]]; then
                # Check if file imports fail package
                if ! grep -q '"github.com/joshyorko/rcc/fail"' "$file" 2>/dev/null; then
                    issues="$issues
$file: Functions returning error but fail package not imported"
                fi
            fi
        fi
    done
    
    echo "$issues"
}

check_dragons() {
    # Identify files in dragon directories
    local files="$1"
    local dragons=""
    
    for file in $files; do
        for dragon_dir in "${DRAGON_DIRS[@]}"; do
            if [[ "$file" == "$dragon_dir/"* ]]; then
                dragons="$dragons $file"
                break
            fi
        done
    done
    
    echo "$dragons" | xargs
}

check_platform_specific() {
    # Check for platform-specific code patterns in wrong files
    local files="$1"
    local issues=""
    
    for file in $files; do
        if [[ -f "$file" ]]; then
            # Skip actual platform files
            if [[ "$file" == *"_windows.go" ]] || [[ "$file" == *"_darwin.go" ]] || [[ "$file" == *"_linux.go" ]]; then
                continue
            fi
            
            # Check for platform-specific imports in shared files
            if grep -qE 'golang.org/x/sys/(windows|unix)' "$file" 2>/dev/null; then
                issues="$issues
$file: Platform-specific import in shared file"
            fi
            
            # Check for runtime.GOOS conditionals (might need platform file)
            local goos_checks
            goos_checks=$(grep -c 'runtime.GOOS' "$file" 2>/dev/null | tr -d '[:space:]' || echo "0")
            if [[ "$goos_checks" =~ ^[0-9]+$ ]] && [[ "$goos_checks" -gt 2 ]]; then
                issues="$issues
$file: Multiple runtime.GOOS checks - consider platform-specific files"
            fi
        fi
    done
    
    echo "$issues"
}

get_test_coverage() {
    # Check if tests exist for changed files
    local files="$1"
    local missing_tests=""
    
    for file in $files; do
        if [[ -f "$file" ]] && [[ "$file" != *"_test.go" ]]; then
            local test_file="${file%.go}_test.go"
            if [[ ! -f "$test_file" ]]; then
                missing_tests="$missing_tests $file"
            fi
        fi
    done
    
    echo "$missing_tests" | xargs
}

check_changelog() {
    # Check if changelog was updated
    if git diff --name-only "${BASE_BRANCH}...HEAD" 2>/dev/null | grep -q 'docs/changelog.md'; then
        echo "updated"
    else
        echo "not_updated"
    fi
}

get_commit_categories() {
    # Extract commit message categories
    git log --oneline "${BASE_BRANCH}...HEAD" 2>/dev/null | head -20 || true
}

count_lines_changed() {
    local files="$1"
    local total=0
    
    for file in $files; do
        if [[ -f "$file" ]]; then
            local lines
            lines=$(git diff "${BASE_BRANCH}...HEAD" -- "$file" 2>/dev/null | grep -c '^[+-]' || echo "0")
            total=$((total + lines))
        fi
    done
    
    echo "$total"
}

get_import_analysis() {
    # Analyze imports in changed files
    local files="$1"
    local third_party=""
    local internal=""
    
    for file in $files; do
        if [[ -f "$file" ]]; then
            # Extract third-party imports (not stdlib, not local)
            local imports
            imports=$(grep -E '^\s+"[a-z]+\.[a-z]+' "$file" 2>/dev/null | grep -v 'github.com/joshyorko/rcc' || true)
            if [[ -n "$imports" ]]; then
                third_party="$third_party
$file:
$imports"
            fi
        fi
    done
    
    echo "$third_party"
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
            echo -e "${YELLOW}No Go files changed to review.${NC}"
        fi
        exit 0
    fi
    
    # Collect all the data
    local file_count
    file_count=$(echo "$changed_files" | wc -l | xargs)
    local lines_changed
    lines_changed=$(count_lines_changed "$changed_files")
    local unformatted
    unformatted=$(check_gofmt "$changed_files")
    local vet_output
    vet_output=$(check_govet "$changed_files" 2>&1 || true)
    local receiver_issues
    receiver_issues=$(check_receiver_names "$changed_files")
    local fail_issues
    fail_issues=$(check_fail_pattern "$changed_files")
    local dragon_files
    dragon_files=$(check_dragons "$changed_files")
    local platform_issues
    platform_issues=$(check_platform_specific "$changed_files")
    local missing_tests
    missing_tests=$(get_test_coverage "$changed_files")
    local new_deps
    new_deps=$(get_new_dependencies)
    local changelog_status
    changelog_status=$(check_changelog)
    local commits
    commits=$(get_commit_categories)
    local import_analysis
    import_analysis=$(get_import_analysis "$changed_files")
    
    if [[ "$OUTPUT_FORMAT" == "json" ]]; then
        # JSON output for programmatic consumption
        cat <<EOF
{
  "status": "success",
  "repo_root": "$REPO_ROOT",
  "base_branch": "$BASE_BRANCH",
  "summary": {
    "files_changed": $file_count,
    "lines_changed": $lines_changed,
    "changelog_updated": $([ "$changelog_status" == "updated" ] && echo "true" || echo "false")
  },
  "changed_files": $(echo "$changed_files" | jq -R -s -c 'split("\n") | map(select(length > 0))'),
  "dragon_files": $(echo "$dragon_files" | xargs -n1 2>/dev/null | jq -R -s -c 'split("\n") | map(select(length > 0))' || echo '[]'),
  "issues": {
    "unformatted": $(echo "$unformatted" | xargs -n1 2>/dev/null | jq -R -s -c 'split("\n") | map(select(length > 0))' || echo '[]'),
    "go_vet": $(echo "$vet_output" | jq -R -s -c .),
    "receiver_names": $(echo "$receiver_issues" | jq -R -s -c .),
    "fail_pattern": $(echo "$fail_issues" | jq -R -s -c .),
    "platform_specific": $(echo "$platform_issues" | jq -R -s -c .),
    "missing_tests": $(echo "$missing_tests" | xargs -n1 2>/dev/null | jq -R -s -c 'split("\n") | map(select(length > 0))' || echo '[]')
  },
  "dependencies": {
    "new": $(echo "$new_deps" | jq -R -s -c 'split("\n") | map(select(length > 0))'),
    "third_party_imports": $(echo "$import_analysis" | jq -R -s -c .)
  },
  "commits": $(echo "$commits" | jq -R -s -c 'split("\n") | map(select(length > 0))')
}
EOF
    else
        # Human-readable output
        echo -e "${BOLD}${CYAN}╔══════════════════════════════════════════════════════════════╗${NC}"
        echo -e "${BOLD}${CYAN}║         vjmp Review Context - \"There are dragons here\"       ║${NC}"
        echo -e "${BOLD}${CYAN}╚══════════════════════════════════════════════════════════════╝${NC}"
        echo ""
        
        echo -e "${BOLD}📊 Summary${NC}"
        echo -e "   Files changed: ${BLUE}$file_count${NC}"
        echo -e "   Lines changed: ${BLUE}$lines_changed${NC}"
        echo -e "   Base branch:   ${BLUE}$BASE_BRANCH${NC}"
        echo -e "   Changelog:     $([ "$changelog_status" == "updated" ] && echo -e "${GREEN}✓ Updated${NC}" || echo -e "${RED}✗ Not updated${NC}")"
        echo ""
        
        echo -e "${BOLD}📁 Changed Go Files${NC}"
        echo "$changed_files" | while read -r f; do
            if [[ -n "$f" ]]; then
                echo -e "   ${BLUE}$f${NC}"
            fi
        done
        echo ""
        
        if [[ -n "$dragon_files" ]]; then
            echo -e "${BOLD}🐉 Dragon Territory (High-Risk Areas)${NC}"
            for f in $dragon_files; do
                echo -e "   ${RED}$f${NC} - Here be dragons!"
            done
            echo ""
        fi
        
        if [[ -n "$unformatted" ]]; then
            echo -e "${BOLD}${RED}⚠️  Unformatted Files (run gofmt)${NC}"
            for f in $unformatted; do
                echo -e "   ${RED}$f${NC}"
            done
            echo ""
        fi
        
        if [[ -n "$vet_output" ]] && [[ "$vet_output" != *"no packages"* ]]; then
            echo -e "${BOLD}${YELLOW}🔍 Go Vet Output${NC}"
            echo "$vet_output" | sed 's/^/   /'
            echo ""
        fi
        
        if [[ -n "$receiver_issues" ]]; then
            echo -e "${BOLD}${YELLOW}📝 Receiver Naming Issues (should be 'it')${NC}"
            echo "$receiver_issues" | sed 's/^/   /'
            echo ""
        fi
        
        if [[ -n "$platform_issues" ]]; then
            echo -e "${BOLD}${YELLOW}💻 Platform-Specific Code Issues${NC}"
            echo "$platform_issues" | sed 's/^/   /'
            echo ""
        fi
        
        if [[ -n "$missing_tests" ]]; then
            echo -e "${BOLD}${YELLOW}🧪 Files Missing Tests${NC}"
            for f in $missing_tests; do
                echo -e "   ${YELLOW}$f${NC}"
            done
            echo ""
        fi
        
        if [[ -n "$new_deps" ]]; then
            echo -e "${BOLD}${RED}📦 New Dependencies (review carefully!)${NC}"
            echo "$new_deps" | while read -r dep; do
                if [[ -n "$dep" ]]; then
                    echo -e "   ${RED}$dep${NC}"
                fi
            done
            echo -e "   ${YELLOW}\"More dependencies you have, more likely some enterprise security"
            echo -e "    tool will find something to complain about\" - vjmp${NC}"
            echo ""
        fi
        
        if [[ -n "$import_analysis" ]]; then
            echo -e "${BOLD}📥 Third-Party Imports in Changed Files${NC}"
            echo "$import_analysis" | sed 's/^/   /'
            echo ""
        fi
        
        echo -e "${BOLD}📝 Recent Commits${NC}"
        echo "$commits" | head -10 | while read -r commit; do
            if [[ -n "$commit" ]]; then
                echo -e "   $commit"
            fi
        done
        echo ""
        
        echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo -e "${BOLD}\"Have you noticed the --pprof option? It is there for a reason.\"${NC}"
        echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    fi
}

main "$@"
