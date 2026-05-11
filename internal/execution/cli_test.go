package execution

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeSDKEngineExecuteUsesSDKBridge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake executable is POSIX-only")
	}

	binDir := t.TempDir()
	writeFakeExecutable(t, filepath.Join(binDir, "node"), `#!/bin/sh
script="$3"
case "$script" in
  *"@anthropic-ai/claude-agent-sdk"*) ;;
  *) echo "unexpected script" >&2; exit 1 ;;
esac
case "$script" in
  *"process.stdout.write"*) cat >/dev/null; printf '{"finalOutput":"sdk response","error":""}' ;;
  *) exit 0 ;;
esac
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	engine := NewClaudeSDKEngine("sonnet")
	require.NoError(t, engine.Initialize(context.Background()))
	resp, err := engine.Execute(context.Background(), &ExecutionRequest{
		Message: "explain this",
		Resources: []ResourceFile{{
			Path:    "main.go",
			Content: []byte("package main\n"),
		}},
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.ErrorMsg)
	assert.Equal(t, "sonnet", resp.ModelID)
	assert.Equal(t, "sdk response", resp.FinalOutput)
	assert.Contains(t, resp.WorkspaceFiles, "main.go")

	workspace := resp.WorkspaceDir
	require.NoError(t, engine.Shutdown(context.Background()))
	_, statErr := os.Stat(workspace)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestNodeSDKEngineInitializeRequiresNode(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	engine := NewCodexSDKEngine("gpt-5.1-codex")
	err := engine.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "codex-sdk requires node in PATH")
}

func TestNodeSDKEngineInitializeRequiresPackage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake executable is POSIX-only")
	}

	binDir := t.TempDir()
	writeFakeExecutable(t, filepath.Join(binDir, "node"), `#!/bin/sh
echo "Cannot find package" >&2
exit 1
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	engine := NewCodexSDKEngine("gpt-5.1-codex")
	err := engine.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "npm package @openai/codex-sdk is required")
}

func TestLimitBuffer(t *testing.T) {
	buf := &limitBuffer{limit: 5}
	n, err := buf.Write([]byte("hello world"))
	require.NoError(t, err)
	assert.Equal(t, len("hello world"), n)
	assert.True(t, buf.truncated)
	assert.Equal(t, "hello", strings.TrimSpace(buf.String()))
}

func writeFakeExecutable(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
}
