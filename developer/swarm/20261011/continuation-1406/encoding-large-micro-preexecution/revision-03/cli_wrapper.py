"""Proposed top-level wrapper for future measurement runners.

Keep SystemExit outside the exception handler: sys.exit(0) is a BaseException
and must not be mistaken for an unexpected supervisor failure.
"""
import traceback


def supervisor_exit_code(supervisor):
    try:
        return supervisor()
    except Exception:
        traceback.print_exc()
        return 1


if __name__ == "__main__":
    # A future runner should invoke: sys.exit(supervisor_exit_code(supervisor))
    # with sys.exit outside this helper's try/except.
    raise SystemExit("import supervisor_exit_code and bind the actual supervisor")
