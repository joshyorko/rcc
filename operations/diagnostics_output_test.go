package operations

import (
	"errors"
	"testing"

	"github.com/joshyorko/rcc/common"
)

type diagnosticTestWriter struct {
	writeErr error
	closeErr error
	closed   bool
}

func (it *diagnosticTestWriter) Write(p []byte) (int, error) {
	if it.writeErr != nil {
		return 0, it.writeErr
	}
	return len(p), nil
}

func (it *diagnosticTestWriter) Close() error {
	it.closed = true
	return it.closeErr
}

func TestWriteDiagnosticsReturnsWriterErrors(t *testing.T) {
	want := errors.New("output unavailable")
	for _, jsonOutput := range []bool{false, true} {
		writer := &diagnosticTestWriter{writeErr: want}
		err := writeDiagnostics(writer, false, &common.DiagnosticStatus{Details: map[string]string{}, Checks: []*common.DiagnosticCheck{}}, jsonOutput, false)
		if !errors.Is(err, want) {
			t.Errorf("writeDiagnostics(json=%v) error = %v, want %v", jsonOutput, err, want)
		}
		if writer.closed {
			t.Errorf("writeDiagnostics(json=%v) closed caller-owned writer", jsonOutput)
		}
	}
}

func TestWriteDiagnosticsClosesFileAndPreservesWriteAndCloseErrors(t *testing.T) {
	writeErr := errors.New("output write failed")
	closeErr := errors.New("output close failed")
	writer := &diagnosticTestWriter{writeErr: writeErr, closeErr: closeErr}

	err := writeDiagnostics(writer, true, &common.DiagnosticStatus{Details: map[string]string{}, Checks: []*common.DiagnosticCheck{}}, true, false)

	if !errors.Is(err, writeErr) || !errors.Is(err, closeErr) {
		t.Fatalf("writeDiagnostics error = %v, want write and close errors", err)
	}
	if !writer.closed {
		t.Fatal("writeDiagnostics did not close the output file")
	}
}
