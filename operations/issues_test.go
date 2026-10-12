package operations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReportIssueContinuesWhenDiagnosticsCannotBeWritten(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ROBOCORP_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "temp"), []byte("blocks temp directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := filepath.Join(home, "report.json")
	if err := os.WriteFile(report, []byte(`{"platform":"test"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ReportIssue("", "", report, nil, true); err == nil {
		t.Fatal("ReportIssue succeeded despite the blocked temporary directory")
	}
}
