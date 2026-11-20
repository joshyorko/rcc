package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const fixtureModule = "github.com/joshyorko/rcc/developer/deadcodeinventory/testdata"

func TestReachabilityInventory(t *testing.T) {
	packagePath := fixtureModule + "/reachability"
	report := loadInventory(t, filepath.Join("testdata", "reachability"))

	if report.SchemaVersion != schemaVersion {
		t.Fatalf("schema version = %d, want %d", report.SchemaVersion, schemaVersion)
	}
	wantNames := []string{
		"GeneratedOnly",
		"LinkedOnly",
		"Live",
		"NeverUsed",
		"RemoteLinked",
		"Shared",
		"Task.Run",
		"TestUsedByTestOnly",
		"UsedByTestOnly",
		"deadHelper",
		"localLinked",
		"main",
	}
	if len(report.Symbols) != len(wantNames) {
		t.Fatalf("got %d symbols (%s), want %d", len(report.Symbols), symbolNames(report.Symbols), len(wantNames))
	}
	byName := make(map[string]symbol, len(report.Symbols))
	for _, symbol := range report.Symbols {
		if symbol.Package != packagePath {
			t.Errorf("symbol %s package = %q, want %q", symbol.Name, symbol.Package, packagePath)
		}
		if _, duplicate := byName[symbol.Name]; duplicate {
			t.Errorf("duplicate declaration for %s", symbol.Name)
		}
		byName[symbol.Name] = symbol
	}
	for _, name := range wantNames {
		if _, found := byName[name]; !found {
			t.Errorf("missing declaration %s", name)
		}
	}

	live := byName["Live"]
	if !live.Exported || live.Generated || live.TestFile || live.Marker || live.Linkname {
		t.Errorf("unexpected Live facts: %+v", live)
	}
	assertPosition(t, "Live", live.Position, "reachability.go")
	assertReferences(t, "Live", packagePath, live.References,
		"NeverUsed|reference|reachability.go|false",
		"main|call|reachability.go|false",
	)

	usedByTest := byName["UsedByTestOnly"]
	assertReferences(t, "UsedByTestOnly", packagePath, usedByTest.References,
		"TestUsedByTestOnly|call|reachability_test.go|true",
	)
	if testFunction := byName["TestUsedByTestOnly"]; !testFunction.TestFile {
		t.Error("TestUsedByTestOnly was not marked as declared in a test file")
	}

	neverUsed := byName["NeverUsed"]
	if len(neverUsed.References) != 0 {
		t.Errorf("NeverUsed references = %+v, want none", neverUsed.References)
	}
	assertCalls(t, "NeverUsed", packagePath, neverUsed.Calls,
		"Live|reference|reachability.go|false",
		"Shared|call|reachability.go|false",
		"deadHelper|call|reachability.go|false",
	)

	deadHelper := byName["deadHelper"]
	if deadHelper.Exported {
		t.Error("deadHelper was marked exported")
	}
	assertReferences(t, "deadHelper", packagePath, deadHelper.References,
		"NeverUsed|call|reachability.go|false",
	)
	assertReferences(t, "Shared", packagePath, byName["Shared"].References,
		"NeverUsed|call|reachability.go|false",
		"main|call|reachability.go|false",
	)

	generated := byName["GeneratedOnly"]
	if !generated.Generated {
		t.Error("GeneratedOnly was not marked generated")
	}
	assertPosition(t, "GeneratedOnly", generated.Position, "generated.go")
	if linked := byName["LinkedOnly"]; !linked.Linkname {
		t.Error("LinkedOnly was not marked as a go:linkname declaration")
	}
	if linked := byName["RemoteLinked"]; !linked.Linkname {
		t.Error("RemoteLinked was not marked through a two-argument go:linkname directive")
	}
	if linked := byName["localLinked"]; !linked.Linkname {
		t.Error("localLinked was not marked as a go:linkname declaration")
	}

	method := byName["Task.Run"]
	if method.Name != "Task.Run" || strings.Contains(method.Name, "*") {
		t.Errorf("method name = %q, want receiver base type without a pointer star", method.Name)
	}
	if !method.Exported || method.Marker {
		t.Errorf("unexpected Task.Run exported/marker facts: %+v", method)
	}
	if want := []string{packagePath + ".Runner.Run"}; !reflect.DeepEqual(method.InterfaceObligations, want) {
		t.Errorf("Task.Run obligations = %v, want %v", method.InterfaceObligations, want)
	}
}

func TestSelectionDoesNotLimitGraphEdges(t *testing.T) {
	packagePath := fixtureModule + "/reachability"
	report := loadInventory(t, filepath.Join("testdata", "reachability"),
		packagePath+"|Live",
		packagePath+"|NeverUsed",
	)
	if len(report.Symbols) != 2 {
		t.Fatalf("got %d selected symbols, want Live and NeverUsed", len(report.Symbols))
	}
	byName := make(map[string]symbol, len(report.Symbols))
	for _, symbol := range report.Symbols {
		byName[symbol.Name] = symbol
	}
	assertReferences(t, "Live", packagePath, byName["Live"].References,
		"NeverUsed|reference|reachability.go|false",
		"main|call|reachability.go|false",
	)
	assertCalls(t, "NeverUsed", packagePath, byName["NeverUsed"].Calls,
		"Live|reference|reachability.go|false",
		"Shared|call|reachability.go|false",
		"deadHelper|call|reachability.go|false",
	)
}

func TestPlatformInventoryUsesSelectedTarget(t *testing.T) {
	packagePath := fixtureModule + "/platform"
	selected := packagePath + "|PlatformOnly"
	root := filepath.Join("testdata", "platform")

	t.Run("linux", func(t *testing.T) {
		setTarget(t, "linux")
		report := loadInventory(t, root, selected)
		if len(report.Symbols) != 0 {
			t.Fatalf("linux symbols = %+v, want PlatformOnly to be absent", report.Symbols)
		}
	})

	t.Run("windows", func(t *testing.T) {
		setTarget(t, "windows")
		report := loadInventory(t, root, selected)
		if len(report.Symbols) != 1 {
			t.Fatalf("got %d windows symbols, want PlatformOnly", len(report.Symbols))
		}
		platformOnly := report.Symbols[0]
		assertPosition(t, "PlatformOnly", platformOnly.Position, "platform_windows.go")
		assertReferences(t, "PlatformOnly", packagePath, platformOnly.References,
			"init|call|platform_windows.go|false",
		)
	})
}

func TestMarkerInterfaceObligation(t *testing.T) {
	packagePath := fixtureModule + "/marker"
	report := loadInventory(t, filepath.Join("testdata", "marker"), packagePath+"|Token.Mark")
	if len(report.Symbols) != 1 {
		t.Fatalf("got %d symbols, want Token.Mark", len(report.Symbols))
	}
	method := report.Symbols[0]
	if !method.Marker {
		t.Error("empty interface implementation was not marked as a marker method")
	}
	if want := []string{packagePath + ".Marker.Mark"}; !reflect.DeepEqual(method.InterfaceObligations, want) {
		t.Errorf("Token.Mark obligations = %v, want %v", method.InterfaceObligations, want)
	}
}

func TestLocalAndAnonymousInterfaceObligations(t *testing.T) {
	packagePath := fixtureModule + "/marker"
	for _, test := range []struct {
		name       string
		method     string
		obligation string
	}{
		{name: "anonymous", method: "LocalToken.LocalMark", obligation: "anonymous@"},
		{name: "local named", method: "NamedToken.NamedMark", obligation: "local.NamedMark"},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := loadInventory(t, filepath.Join("testdata", "marker"), packagePath+"|"+test.method)
			if len(report.Symbols) != 1 {
				t.Fatalf("got %d symbols, want %s", len(report.Symbols), test.method)
			}
			method := report.Symbols[0]
			if !method.Marker {
				t.Error("empty local interface implementation was not marked as a marker method")
			}
			joined := strings.Join(method.InterfaceObligations, "\n")
			if !strings.Contains(joined, test.obligation) {
				t.Errorf("%s obligations = %v, want substring %q", test.method, method.InterfaceObligations, test.obligation)
			}
		})
	}
}

func TestPackageLoadErrorsAreReported(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("testdata", "reachability"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = run([]string{"-root", root, "--", "./does-not-exist"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run succeeded for a missing package pattern")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want no JSON after a loading error", stdout.String())
	}
	if !strings.Contains(stderr.String(), "does-not-exist") {
		t.Errorf("stderr = %q, want package loading diagnostic", stderr.String())
	}
}

func loadInventory(t *testing.T, root string, selected ...string) inventory {
	t.Helper()
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	arguments := []string{"-root", absoluteRoot, "-tags", ""}
	for _, symbol := range selected {
		arguments = append(arguments, "-symbol", symbol)
	}
	arguments = append(arguments, "--", ".")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(arguments, &stdout, &stderr); err != nil {
		t.Fatalf("run failed: %v\nstderr:\n%s", err, stderr.String())
	}
	var report inventory
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if report.Symbols == nil {
		t.Fatal("symbols is null, want a JSON array")
	}
	return report
}

func setTarget(t *testing.T, goos string) {
	t.Helper()
	t.Setenv("GOOS", goos)
	t.Setenv("GOARCH", "amd64")
	t.Setenv("CGO_ENABLED", "0")
}

func assertPosition(t *testing.T, name string, got position, wantFile string) {
	t.Helper()
	if got.File != wantFile || got.Line < 1 || got.Column < 1 {
		t.Errorf("%s position = %+v, want file %q with one-based line and column", name, got, wantFile)
	}
}

func assertReferences(t *testing.T, name, packagePath string, references []reference, want ...string) {
	t.Helper()
	got := make([]string, 0, len(references))
	for _, reference := range references {
		if reference.CallerPackage != packagePath {
			t.Errorf("%s reference caller package = %q, want %q", name, reference.CallerPackage, packagePath)
		}
		if reference.Position.Line < 1 || reference.Position.Column < 1 {
			t.Errorf("%s reference has invalid position: %+v", name, reference.Position)
		}
		got = append(got, fmt.Sprintf("%s|%s|%s|%t", reference.CallerName, reference.Kind, reference.Position.File, reference.Test))
	}
	assertStringSet(t, name+" references", got, want)
}

func assertCalls(t *testing.T, name, packagePath string, calls []call, want ...string) {
	t.Helper()
	got := make([]string, 0, len(calls))
	for _, call := range calls {
		if call.CalleePackage != packagePath {
			t.Errorf("%s call callee package = %q, want %q", name, call.CalleePackage, packagePath)
		}
		if call.Position.Line < 1 || call.Position.Column < 1 {
			t.Errorf("%s call has invalid position: %+v", name, call.Position)
		}
		if call.CalleePosition.File != "reachability.go" || call.CalleePosition.Line < 1 || call.CalleePosition.Column < 1 {
			t.Errorf("%s call has invalid callee position: %+v", name, call.CalleePosition)
		}
		got = append(got, fmt.Sprintf("%s|%s|%s|%t", call.CalleeName, call.Kind, call.Position.File, call.Test))
	}
	assertStringSet(t, name+" calls", got, want)
}

func assertStringSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func symbolNames(symbols []symbol) []string {
	names := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		names = append(names, symbol.Name)
	}
	return names
}
