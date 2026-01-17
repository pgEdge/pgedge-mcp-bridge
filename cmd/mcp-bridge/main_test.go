package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgEdge/pgedge-mcp-bridge/pkg/version"
)

// ===========================================================================
// Test Helpers
// ===========================================================================

// captureOutput captures stdout and stderr during the execution of a function.
// It returns the captured stdout, stderr, and the result of the function.
func captureOutput(t *testing.T, fn func() int) (stdout string, stderr string, exitCode int) {
	t.Helper()

	// Save original stdout and stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	// Create pipes for capturing output
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}

	// Redirect stdout and stderr to our pipes
	os.Stdout = wOut
	os.Stderr = wErr

	// Run the function
	exitCode = fn()

	// Close write ends to signal EOF
	wOut.Close()
	wErr.Close()

	// Read captured output
	var outBuf, errBuf bytes.Buffer
	io.Copy(&outBuf, rOut)
	io.Copy(&errBuf, rErr)

	// Restore original stdout and stderr
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	return outBuf.String(), errBuf.String(), exitCode
}

// withArgs temporarily sets os.Args for a test and restores it afterward.
// It also resets the flag package state.
func withArgs(t *testing.T, args []string, fn func() int) int {
	t.Helper()

	// Save original args
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	// Reset flag package state
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	// Set new args
	os.Args = args

	return fn()
}

// ===========================================================================
// Help Flag Tests
// ===========================================================================

func TestRun_HelpFlag_ShowsUsageAndExits0(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{"long help flag", []string{"mcp-bridge", "--help"}},
		{"short help flag", []string{"mcp-bridge", "-h"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, exitCode := captureOutput(t, func() int {
				return withArgs(t, tc.args, run)
			})

			// Check exit code
			if exitCode != 0 {
				t.Errorf("expected exit code 0, got %d", exitCode)
			}

			// Help output goes to stderr (via flag.Usage)
			combinedOutput := stdout + stderr

			// Check for expected help content
			expectedStrings := []string{
				"Usage:",
				"MCP HTTP Bridge",
				"Options:",
				"-c, --config",
				"-v, --version",
				"-h, --help",
				"Examples:",
			}

			for _, expected := range expectedStrings {
				if !strings.Contains(combinedOutput, expected) {
					t.Errorf("expected help output to contain '%s', got:\n%s", expected, combinedOutput)
				}
			}
		})
	}
}

func TestRun_HelpFlag_ShowsConfigOption(t *testing.T) {
	stdout, stderr, _ := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "--help"}, run)
	})

	combinedOutput := stdout + stderr

	// Verify config option is documented
	if !strings.Contains(combinedOutput, "config") {
		t.Errorf("expected help to mention config option, got:\n%s", combinedOutput)
	}

	if !strings.Contains(combinedOutput, "Path to configuration file") {
		t.Errorf("expected help to describe config option, got:\n%s", combinedOutput)
	}
}

// ===========================================================================
// Version Flag Tests
// ===========================================================================

func TestRun_VersionFlag_ShowsVersionAndExits0(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{"long version flag", []string{"mcp-bridge", "--version"}},
		{"short version flag", []string{"mcp-bridge", "-v"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, exitCode := captureOutput(t, func() int {
				return withArgs(t, tc.args, run)
			})

			// Check exit code
			if exitCode != 0 {
				t.Errorf("expected exit code 0, got %d", exitCode)
			}

			// Version output goes to stdout
			combinedOutput := stdout + stderr

			// Check for expected version content
			expectedVersion := version.Info()
			if !strings.Contains(combinedOutput, expectedVersion) {
				t.Errorf("expected version output to contain '%s', got:\n%s", expectedVersion, combinedOutput)
			}
		})
	}
}

func TestRun_VersionFlag_ContainsVersionString(t *testing.T) {
	stdout, stderr, _ := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "-v"}, run)
	})

	combinedOutput := stdout + stderr

	// Verify version string contains expected components
	if !strings.Contains(combinedOutput, "mcp-bridge") {
		t.Errorf("expected version output to contain 'mcp-bridge', got:\n%s", combinedOutput)
	}

	if !strings.Contains(combinedOutput, version.Short()) {
		t.Errorf("expected version output to contain version '%s', got:\n%s", version.Short(), combinedOutput)
	}
}

// ===========================================================================
// Missing Config File Tests
// ===========================================================================

func TestRun_MissingConfigFile_ShowsErrorAndExits1(t *testing.T) {
	// Create a temporary directory with no config file
	tmpDir := t.TempDir()

	// Change to temp directory so FindConfigFile won't find anything
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(oldWd)

	stdout, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge"}, run)
	})

	// Check exit code
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// Check for error message in stderr
	combinedOutput := stdout + stderr

	if !strings.Contains(combinedOutput, "Error") {
		t.Errorf("expected error message in output, got:\n%s", combinedOutput)
	}

	// Should mention config file
	if !strings.Contains(combinedOutput, "config") {
		t.Errorf("expected output to mention config, got:\n%s", combinedOutput)
	}
}

func TestRun_SpecifiedMissingConfigFile_ShowsErrorAndExits1(t *testing.T) {
	nonexistentPath := "/nonexistent/path/to/config.yaml"

	stdout, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "-c", nonexistentPath}, run)
	})

	// Check exit code
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// Check for error message
	combinedOutput := stdout + stderr

	if !strings.Contains(combinedOutput, "Error") {
		t.Errorf("expected error message in output, got:\n%s", combinedOutput)
	}
}

func TestRun_MissingConfigFile_LongFlag(t *testing.T) {
	nonexistentPath := "/nonexistent/config.yaml"

	_, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "--config", nonexistentPath}, run)
	})

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	if !strings.Contains(stderr, "Error") {
		t.Errorf("expected error message in stderr, got:\n%s", stderr)
	}
}

// ===========================================================================
// Invalid Config File Tests
// ===========================================================================

func TestRun_InvalidConfigFile_ShowsErrorAndExits1(t *testing.T) {
	// Create a temporary invalid config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid_config.yaml")

	// Write invalid YAML content
	invalidContent := `
this is not: valid: yaml: content
  - broken indentation
`
	if err := os.WriteFile(configPath, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	stdout, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "-c", configPath}, run)
	})

	// Check exit code
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// Check for error message
	combinedOutput := stdout + stderr

	if !strings.Contains(combinedOutput, "Error") {
		t.Errorf("expected error message in output, got:\n%s", combinedOutput)
	}
}

func TestRun_EmptyConfigFile_ShowsErrorAndExits1(t *testing.T) {
	// Create an empty config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "empty_config.yaml")

	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write empty config: %v", err)
	}

	stdout, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "-c", configPath}, run)
	})

	// Check exit code
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// Check for error message about missing mode
	combinedOutput := stdout + stderr

	if !strings.Contains(combinedOutput, "Error") {
		t.Errorf("expected error message in output, got:\n%s", combinedOutput)
	}
}

func TestRun_ConfigMissingMode_ShowsErrorAndExits1(t *testing.T) {
	// Create a config file without mode
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "no_mode_config.yaml")

	content := `
server:
  listen: ":8080"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	stdout, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "-c", configPath}, run)
	})

	// Check exit code
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// Check for error message about mode
	combinedOutput := stdout + stderr

	if !strings.Contains(combinedOutput, "Error") {
		t.Errorf("expected error message in output, got:\n%s", combinedOutput)
	}
}

func TestRun_ConfigInvalidMode_ShowsErrorAndExits1(t *testing.T) {
	// Create a config file with invalid mode
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid_mode_config.yaml")

	content := `mode: invalid_mode`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	stdout, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "-c", configPath}, run)
	})

	// Check exit code
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// Check for error message
	combinedOutput := stdout + stderr

	if !strings.Contains(combinedOutput, "Error") {
		t.Errorf("expected error message in output, got:\n%s", combinedOutput)
	}
}

func TestRun_ConfigServerModeWithoutServerConfig_ShowsErrorAndExits1(t *testing.T) {
	// Create a config file with server mode but no server config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "server_no_config.yaml")

	content := `mode: server`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	stdout, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "-c", configPath}, run)
	})

	// Check exit code
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// Check for error message
	combinedOutput := stdout + stderr

	if !strings.Contains(combinedOutput, "Error") {
		t.Errorf("expected error message in output, got:\n%s", combinedOutput)
	}
}

func TestRun_ConfigClientModeWithoutClientConfig_ShowsErrorAndExits1(t *testing.T) {
	// Create a config file with client mode but no client config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "client_no_config.yaml")

	content := `mode: client`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	stdout, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "-c", configPath}, run)
	})

	// Check exit code
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// Check for error message
	combinedOutput := stdout + stderr

	if !strings.Contains(combinedOutput, "Error") {
		t.Errorf("expected error message in output, got:\n%s", combinedOutput)
	}
}

// ===========================================================================
// Flag Combination Tests
// ===========================================================================

func TestRun_HelpFlagTakesPrecedence(t *testing.T) {
	// Help flag should take precedence over version flag
	stdout, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "--help", "--version"}, run)
	})

	// Should exit 0 for help
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	// Should show help output, not version
	combinedOutput := stdout + stderr

	if !strings.Contains(combinedOutput, "Usage:") {
		t.Errorf("expected help output with 'Usage:', got:\n%s", combinedOutput)
	}
}

// ===========================================================================
// Config Path Flag Tests
// ===========================================================================

func TestRun_ConfigFlagShortForm(t *testing.T) {
	nonexistentPath := "/nonexistent/short/config.yaml"

	_, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "-c", nonexistentPath}, run)
	})

	// Should fail because file doesn't exist
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// Should show error
	if !strings.Contains(stderr, "Error") {
		t.Errorf("expected error message, got:\n%s", stderr)
	}
}

func TestRun_ConfigFlagLongForm(t *testing.T) {
	nonexistentPath := "/nonexistent/long/config.yaml"

	_, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "--config", nonexistentPath}, run)
	})

	// Should fail because file doesn't exist
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// Should show error
	if !strings.Contains(stderr, "Error") {
		t.Errorf("expected error message, got:\n%s", stderr)
	}
}

// ===========================================================================
// Table-Driven Tests
// ===========================================================================

func TestRun_FlagsTableDriven(t *testing.T) {
	testCases := []struct {
		name             string
		args             []string
		expectedExitCode int
		outputContains   []string
	}{
		{
			name:             "help flag",
			args:             []string{"mcp-bridge", "-h"},
			expectedExitCode: 0,
			outputContains:   []string{"Usage:", "Options:"},
		},
		{
			name:             "version flag",
			args:             []string{"mcp-bridge", "-v"},
			expectedExitCode: 0,
			outputContains:   []string{"mcp-bridge"},
		},
		{
			name:             "nonexistent config",
			args:             []string{"mcp-bridge", "-c", "/nonexistent/config.yaml"},
			expectedExitCode: 1,
			outputContains:   []string{"Error"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, exitCode := captureOutput(t, func() int {
				return withArgs(t, tc.args, run)
			})

			if exitCode != tc.expectedExitCode {
				t.Errorf("expected exit code %d, got %d", tc.expectedExitCode, exitCode)
			}

			combinedOutput := stdout + stderr
			for _, expected := range tc.outputContains {
				if !strings.Contains(combinedOutput, expected) {
					t.Errorf("expected output to contain '%s', got:\n%s", expected, combinedOutput)
				}
			}
		})
	}
}

func TestRun_InvalidConfigsTableDriven(t *testing.T) {
	testCases := []struct {
		name           string
		configContent  string
		outputContains string
	}{
		{
			name:           "empty config",
			configContent:  "",
			outputContains: "Error",
		},
		{
			name:           "no mode",
			configContent:  "server:\n  listen: ':8080'",
			outputContains: "Error",
		},
		{
			name:           "invalid mode",
			configContent:  "mode: invalid",
			outputContains: "Error",
		},
		{
			name:           "server mode without server config",
			configContent:  "mode: server",
			outputContains: "Error",
		},
		{
			name:           "client mode without client config",
			configContent:  "mode: client",
			outputContains: "Error",
		},
		{
			name:           "malformed yaml",
			configContent:  "mode: server\n  broken: yaml",
			outputContains: "Error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")

			if err := os.WriteFile(configPath, []byte(tc.configContent), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			stdout, stderr, exitCode := captureOutput(t, func() int {
				return withArgs(t, []string{"mcp-bridge", "-c", configPath}, run)
			})

			if exitCode != 1 {
				t.Errorf("expected exit code 1, got %d", exitCode)
			}

			combinedOutput := stdout + stderr
			if !strings.Contains(combinedOutput, tc.outputContains) {
				t.Errorf("expected output to contain '%s', got:\n%s", tc.outputContains, combinedOutput)
			}
		})
	}
}

// ===========================================================================
// Edge Cases
// ===========================================================================

func TestRun_UnreadableConfigFile(t *testing.T) {
	// Skip on Windows where file permissions work differently
	if os.Getenv("GOOS") == "windows" {
		t.Skip("skipping on Windows")
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "unreadable.yaml")

	// Create a file with no read permissions
	if err := os.WriteFile(configPath, []byte("mode: server"), 0000); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	// Ensure we clean up the permissions so the file can be removed
	defer os.Chmod(configPath, 0644)

	_, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "-c", configPath}, run)
	})

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	if !strings.Contains(stderr, "Error") {
		t.Errorf("expected error message for unreadable file, got:\n%s", stderr)
	}
}

func TestRun_DirectoryAsConfigFile(t *testing.T) {
	tmpDir := t.TempDir()

	_, stderr, exitCode := captureOutput(t, func() int {
		return withArgs(t, []string{"mcp-bridge", "-c", tmpDir}, run)
	})

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	if !strings.Contains(stderr, "Error") {
		t.Errorf("expected error message for directory as config, got:\n%s", stderr)
	}
}
