#!/bin/env python3
"""
Dead code detector for Go projects.

Finds functions and methods that are potentially unused (only referenced at their definition).

Usage:
    python3 scripts/deadcode.py [options]

Options:
    --verbose, -v       Show detailed progress and statistics
    --max-refs N        Show functions with at most N references (default: 1)
    --include-exported  Include exported (capitalized) functions
    --include-tests     Include Test* functions
    --summary           Show only summary statistics
"""

import argparse
import os
import pathlib
import re
import sys
from collections import defaultdict

# Match function definitions: func FuncName(...) or func (receiver) MethodName(...)
FUNC_PATTERN = re.compile(r"^\s*func\s+(?:\([^)]+\)\s+)?(\w+)")
# Match word boundaries for function usage
WORD_BOUNDARY = re.compile(r"\b(\w+)\b")


def log(msg, verbose=True):
    """Print message if verbose mode is enabled."""
    if verbose:
        print(f"[INFO] {msg}", file=sys.stderr)


def read_file(filename):
    """Read file and yield (line_number, line_content) tuples."""
    try:
        with open(filename, encoding="utf-8") as source:
            for index, line in enumerate(source):
                yield index + 1, line
    except (IOError, UnicodeDecodeError) as e:
        print(f"[WARN] Could not read {filename}: {e}", file=sys.stderr)


def find_files(where, pattern):
    """Find all files matching pattern, excluding vendor and test directories."""
    excluded_dirs = {"vendor", ".git", "testdata", "mocks"}
    results = []
    for path in pathlib.Path(where).rglob(pattern):
        # Skip excluded directories
        if any(excluded in path.parts for excluded in excluded_dirs):
            continue
        results.append(path.relative_to(where))
    return tuple(sorted(results))


def find_function_definitions(files, verbose=False):
    """Find all function/method definitions in Go files."""
    functions = defaultdict(list)  # function_name -> [(filename, line_number), ...]
    
    for filename in files:
        for number, line in read_file(filename):
            match = FUNC_PATTERN.match(line)
            if match:
                func_name = match.group(1)
                functions[func_name].append((str(filename), number))
    
    log(f"Found {len(functions)} unique function/method names", verbose)
    return functions


def count_references(files, function_names, verbose=False):
    """Count how many times each function name appears in the codebase."""
    counters = defaultdict(int)
    references = defaultdict(list)  # function_name -> [(filename:line, context), ...]
    
    # Create a set for O(1) lookup
    func_set = set(function_names)
    total_files = len(files)
    
    for i, filename in enumerate(files):
        if verbose and (i + 1) % 50 == 0:
            log(f"Scanning file {i + 1}/{total_files}: {filename}", verbose)
        
        for number, line in read_file(filename):
            # Find all word-like tokens in the line
            for match in WORD_BOUNDARY.finditer(line):
                word = match.group(1)
                if word in func_set:
                    counters[word] += 1
                    references[word].append((f"{filename}:{number}", line.strip()[:80]))
    
    log(f"Scanned {total_files} files for references", verbose)
    return counters, references


def process(args):
    """Main processing function."""
    verbose = args.verbose
    max_refs = args.max_refs
    include_exported = args.include_exported
    include_tests = args.include_tests
    summary_only = args.summary
    show_refs = args.show_refs
    all_low_usage = args.all_low_usage
    
    log("Starting dead code detection...", verbose)
    
    # Find all Go files
    files = find_files(os.getcwd(), "*.go")
    log(f"Found {len(files)} Go files", verbose)
    
    if not files:
        print("[ERROR] No Go files found in current directory", file=sys.stderr)
        return
    
    # Find function definitions
    functions = find_function_definitions(files, verbose)
    
    if not functions:
        print("[ERROR] No functions found", file=sys.stderr)
        return
    
    # Count references
    counters, references = count_references(files, functions.keys(), verbose)
    
    # Analyze results
    dead_candidates = []
    low_usage = []
    
    for func_name, definitions in sorted(functions.items()):
        # Skip test functions unless requested
        if not include_tests and func_name.startswith("Test"):
            continue
        
        # Skip exported functions unless requested (they might be used externally)
        is_exported = func_name[0].isupper()
        if not include_exported and is_exported:
            continue
        
        # Skip common patterns that are often false positives
        if func_name in ("init", "main", "String", "Error"):
            continue
        
        ref_count = counters.get(func_name, 0)
        def_count = len(definitions)
        
        # A function is "dead" if it's only referenced at its definition(s)
        # (each definition line contains the function name once)
        if ref_count <= def_count:
            dead_candidates.append((func_name, ref_count, definitions))
        elif ref_count <= max_refs + def_count:
            low_usage.append((func_name, ref_count, def_count, definitions))
    
    # Print results
    print("\n" + "=" * 70)
    print("DEAD CODE ANALYSIS REPORT")
    print("=" * 70)
    
    print(f"\nStatistics:")
    print(f"  - Go files scanned:      {len(files)}")
    print(f"  - Functions found:       {len(functions)}")
    print(f"  - Potentially dead:      {len(dead_candidates)}")
    print(f"  - Low usage (≤{max_refs} refs): {len(low_usage)}")
    
    if summary_only:
        return
    
    if dead_candidates:
        print(f"\n{'─' * 70}")
        print("POTENTIALLY DEAD CODE (only referenced at definition)")
        print(f"{'─' * 70}")
        for func_name, ref_count, definitions in dead_candidates:
            for filename, line_num in definitions:
                print(f"  {filename}:{line_num:<6} {func_name}")
            if show_refs and func_name in references:
                print(f"    References ({len(references[func_name])}):")
                for ref_loc, context in references[func_name]:
                    print(f"      {ref_loc}")
                    print(f"        → {context}")
                print()
    else:
        print("\n✓ No dead code candidates found!")
    
    if low_usage and not summary_only:
        print(f"\n{'─' * 70}")
        print(f"LOW USAGE FUNCTIONS (≤{max_refs} references beyond definition)")
        print(f"{'─' * 70}")
        display_count = len(low_usage) if all_low_usage else min(20, len(low_usage))
        for func_name, ref_count, def_count, definitions in low_usage[:display_count]:
            actual_refs = ref_count - def_count
            for filename, line_num in definitions:
                print(f"  {filename}:{line_num:<6} {func_name} ({actual_refs} refs)")
            if show_refs and func_name in references:
                print(f"    All references ({len(references[func_name])}):")
                for ref_loc, context in references[func_name]:
                    print(f"      {ref_loc}")
                    print(f"        → {context}")
                print()
        if not all_low_usage and len(low_usage) > 20:
            print(f"  ... and {len(low_usage) - 20} more (use --all-low-usage to show all)")
    
    print("\n" + "=" * 70)
    print("NOTE: Exported functions (capitalized) are excluded by default.")
    print("      Use --include-exported to include them.")
    print("      False positives may occur for reflection, interfaces, or external usage.")
    print("=" * 70 + "\n")


def main():
    parser = argparse.ArgumentParser(
        description="Detect potentially unused Go functions and methods.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__
    )
    parser.add_argument(
        "-v", "--verbose",
        action="store_true",
        help="Show detailed progress and statistics"
    )
    parser.add_argument(
        "--max-refs",
        type=int,
        default=1,
        help="Show functions with at most N references beyond definition (default: 1)"
    )
    parser.add_argument(
        "--include-exported",
        action="store_true",
        help="Include exported (capitalized) functions in analysis"
    )
    parser.add_argument(
        "--include-tests",
        action="store_true",
        help="Include Test* functions in analysis"
    )
    parser.add_argument(
        "--summary",
        action="store_true",
        help="Show only summary statistics, no detailed list"
    )
    parser.add_argument(
        "--show-refs",
        action="store_true",
        help="Show all references for each function (where it's used)"
    )
    parser.add_argument(
        "--all-low-usage",
        action="store_true",
        help="Show all low-usage functions (not just first 20)"
    )
    
    args = parser.parse_args()
    process(args)


if __name__ == "__main__":
    main()
