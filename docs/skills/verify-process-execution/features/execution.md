# Execution and command resolution

The caller leases an admitted materialization, resolves the command, prepares supervision, and starts one child. Cancellation and attachment failure terminate and reap that child before deferred cleanup. Nonzero child exit remains a child result; infrastructure and cleanup failures remain errors.

Source binding at `cf7a47e7b9430db4abebae68776f65f3e309624e`:

- `environmentlifecycle/execute.go:LocalMaterializer.ExecutionHandle`: unchanged stored lease, live owner identity, matching ready record, and command selection.
- `environmentlifecycle/execute.go:executeWithSignalsAndStreamsInDirectory`: lease, preparation, start, attach, wait, supervisor close, release; start failure still releases the lease.
- `environmentlifecycle/execute.go:executionResult`: separate child exit code from infrastructure failure.
- `environmentlifecycle/executable_resolution.go:resolveMaterializedExecutable` and `findExecutableCandidate`: bare names use only environment PATH; relative paths reject traversal and symlink escape; explicit absolute paths are intentionally accepted.
- `environmentlifecycle/execution_supervisor_linux.go:prepare`: direct-child parent-death signal; `terminate` kills the direct child.
- `environmentlifecycle/execution_supervisor_windows.go:newExecutionSupervisor` and `attach`: kill-on-close job with an attachment failure branch.

Live recipe: run both Go commands in the skill's Drive section. They exercise success, nonzero exit, spawn failure, stream request/response, lease lifetime, signal forwarding, stale ownership, and executable lookup. Python availability is a prerequisite for the Python/source-CWD cases; POSIX signals are unavailable on Windows. Report skips explicitly. Cancellation with inherited grandchild streams and caller death have separate tests in `environmentlifecycle/lease_test.go:TestExecuteCancellationDoesNotWaitForGrandchildInheritedStreams` and `environmentlifecycle/execute_process_test.go:TestExecutionCallerDeathAfterSpawn`; the model does not claim platform-complete acceptance from the baseline recipe.
