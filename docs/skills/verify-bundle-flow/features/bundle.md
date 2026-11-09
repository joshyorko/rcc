# Bundled task and artifacts

RCC imports bundled environments, extracts source into a temporary workarea, resolves the selected task, captures output when enabled, executes, and copies artifacts to the caller's project. Setup failure refuses execution. A failed child still gets output finalization; successful child completion alone does not establish command success.

Source binding at `cf7a47e7b9430db4abebae68776f65f3e309624e`:

- `cmd/robotRunFromBundle.go:robotRunFromBundleCmd`: artifact versus legacy-source preparation; explicit strict mode rejects source-only bundles; task lookup precedes deferred artifact finalization.
- `cmd/robotBundleOutput.go:executeArtifactBundleTask`: prepare writers, execute in source workarea, close writers before return.
- `cmd/robotBundleOutput.go:prepareBundleOutputWriters`: separate stdout/stderr capture using exclusive creation; capture-disabled mode still validates the artifact directory.
- `cmd/robotBundleOutput.go:bundleArtifactPath` and `finalizeBundleArtifactsTo`: reject workarea escapes and invoke safe copy-back.
- `common/exit.go:Exit`: panic-based command exit allows registered finalization defers to run after task errors.

Live recipe: run the focused helper suite in the skill's Drive section. `cmd/robotBundleOutput_test.go:TestPrepareBundleOutputWritersCaptureSeparateStreams` exercises capture; `TestPrepareBundleOutputWritersRejectUnsafeArtifactDirectories` and `TestFinalizeBundleArtifactsRejectsSymlinkEscapes` check boundaries. `cmd/bundle_utils_test.go:TestCopyBundleArtifactsRejectsExistingTargetWithoutOverwriting` and `TestCopyBundleFileFailureRemovesPartialTarget` exercise finalization failures. These fixtures do not drive the full CLI or establish runtime cancellation behavior. Whole-command acceptance requires the candidate/native-archive fixture specified by `TestBundleTrustRealCLI`.
