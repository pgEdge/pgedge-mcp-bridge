package version

import (
	"runtime"
	"strings"
	"testing"
)

// ===========================================================================
// Info Function Tests
// ===========================================================================

func TestInfo_ReturnsExpectedFormat(t *testing.T) {
	info := Info()

	// Check that the info string contains expected components
	if !strings.Contains(info, "mcp-bridge") {
		t.Errorf("expected Info() to contain 'mcp-bridge', got: %s", info)
	}

	if !strings.Contains(info, Version) {
		t.Errorf("expected Info() to contain version '%s', got: %s", Version, info)
	}

	if !strings.Contains(info, GitCommit) {
		t.Errorf("expected Info() to contain commit '%s', got: %s", GitCommit, info)
	}

	if !strings.Contains(info, BuildTime) {
		t.Errorf("expected Info() to contain build time '%s', got: %s", BuildTime, info)
	}

	if !strings.Contains(info, runtime.GOOS) {
		t.Errorf("expected Info() to contain GOOS '%s', got: %s", runtime.GOOS, info)
	}

	if !strings.Contains(info, runtime.GOARCH) {
		t.Errorf("expected Info() to contain GOARCH '%s', got: %s", runtime.GOARCH, info)
	}
}

func TestInfo_DefaultValues(t *testing.T) {
	// Test with default values (not set via ldflags)
	info := Info()

	// Default Version is "dev"
	if !strings.Contains(info, "dev") {
		t.Logf("Version may have been set via ldflags, current value: %s", Version)
	}

	// Default BuildTime is "unknown"
	if !strings.Contains(info, "unknown") && BuildTime == "unknown" {
		t.Logf("BuildTime may have been set via ldflags, current value: %s", BuildTime)
	}
}

func TestInfo_FormatStructure(t *testing.T) {
	info := Info()

	// Verify the format structure: "mcp-bridge VERSION (commit: COMMIT, built: TIME, OS/ARCH)"
	if !strings.HasPrefix(info, "mcp-bridge ") {
		t.Errorf("expected Info() to start with 'mcp-bridge ', got: %s", info)
	}

	if !strings.Contains(info, "(commit:") {
		t.Errorf("expected Info() to contain '(commit:', got: %s", info)
	}

	if !strings.Contains(info, "built:") {
		t.Errorf("expected Info() to contain 'built:', got: %s", info)
	}

	// Check for OS/ARCH format
	osArch := runtime.GOOS + "/" + runtime.GOARCH
	if !strings.Contains(info, osArch) {
		t.Errorf("expected Info() to contain '%s', got: %s", osArch, info)
	}

	// Verify closing parenthesis
	if !strings.HasSuffix(info, ")") {
		t.Errorf("expected Info() to end with ')', got: %s", info)
	}
}

// ===========================================================================
// Short Function Tests
// ===========================================================================

func TestShort_ReturnsVersion(t *testing.T) {
	short := Short()

	if short != Version {
		t.Errorf("expected Short() to return '%s', got '%s'", Version, short)
	}
}

func TestShort_DefaultValue(t *testing.T) {
	short := Short()

	// Default Version is "dev"
	if Version == "dev" && short != "dev" {
		t.Errorf("expected Short() to return 'dev' for default version, got '%s'", short)
	}
}

func TestShort_NotEmpty(t *testing.T) {
	short := Short()

	if short == "" {
		t.Error("expected Short() to return non-empty string")
	}
}

// ===========================================================================
// Variable Tests
// ===========================================================================

func TestVersionVariables_Exist(t *testing.T) {
	// Verify that version variables are accessible
	_ = Version
	_ = BuildTime
	_ = GitCommit
}

func TestVersionVariables_HaveDefaults(t *testing.T) {
	// These tests verify the default values when not set via ldflags
	// In actual builds, these may be overridden

	// Just verify they are not empty strings
	if Version == "" {
		t.Error("expected Version to have a default value")
	}

	if BuildTime == "" {
		t.Error("expected BuildTime to have a default value")
	}

	if GitCommit == "" {
		t.Error("expected GitCommit to have a default value")
	}
}

// ===========================================================================
// Integration Tests
// ===========================================================================

func TestInfo_ContainsShort(t *testing.T) {
	info := Info()
	short := Short()

	// The full info should contain the short version
	if !strings.Contains(info, short) {
		t.Errorf("expected Info() '%s' to contain Short() '%s'", info, short)
	}
}

func TestVersionConsistency(t *testing.T) {
	// Multiple calls should return the same values
	info1 := Info()
	info2 := Info()
	short1 := Short()
	short2 := Short()

	if info1 != info2 {
		t.Errorf("expected consistent Info() results, got '%s' and '%s'", info1, info2)
	}

	if short1 != short2 {
		t.Errorf("expected consistent Short() results, got '%s' and '%s'", short1, short2)
	}
}

// ===========================================================================
// Table-Driven Tests for Various Scenarios
// ===========================================================================

func TestInfo_TableDriven(t *testing.T) {
	testCases := []struct {
		name     string
		contains string
	}{
		{"contains program name", "mcp-bridge"},
		{"contains version", Version},
		{"contains commit label", "commit:"},
		{"contains built label", "built:"},
		{"contains GOOS", runtime.GOOS},
		{"contains GOARCH", runtime.GOARCH},
	}

	info := Info()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(info, tc.contains) {
				t.Errorf("expected Info() to contain '%s', got: %s", tc.contains, info)
			}
		})
	}
}
