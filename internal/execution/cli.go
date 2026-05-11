package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/microsoft/waza/internal/models"
)

const cliOutputLimit = 1 << 20

type NodeSDKEngine struct {
	engineType    string
	modelID       string
	packageName   string
	script        string
	nodePath      string
	workspace     string
	keepWorkspace bool
	initCalled    atomic.Bool
}

func NewClaudeSDKEngine(modelID string) *NodeSDKEngine {
	return &NodeSDKEngine{
		engineType:  "claude-sdk",
		modelID:     modelID,
		packageName: "@anthropic-ai/claude-agent-sdk",
		script:      claudeAgentSDKScript,
	}
}

func NewCodexSDKEngine(modelID string) *NodeSDKEngine {
	return &NodeSDKEngine{
		engineType:  "codex-sdk",
		modelID:     modelID,
		packageName: "@openai/codex-sdk",
		script:      codexSDKScript,
	}
}

func (e *NodeSDKEngine) SetKeepWorkspace(keep bool) {
	e.keepWorkspace = keep
}

func (e *NodeSDKEngine) Initialize(ctx context.Context) error {
	path, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("%s requires node in PATH", e.engineType)
	}
	if err := checkNodePackage(ctx, path, e.packageName); err != nil {
		return err
	}
	e.nodePath = path
	e.initCalled.Store(true)
	return nil
}

func (e *NodeSDKEngine) Execute(ctx context.Context, req *ExecutionRequest) (*ExecutionResponse, error) {
	if !e.initCalled.Load() {
		return nil, fmt.Errorf("engine was not initialized. Initialize needs to be called before Execute")
	}
	if req == nil {
		return nil, fmt.Errorf("nil req was passed to NodeSDKEngine.Execute")
	}

	start := time.Now()
	workspaceDir, err := e.prepareWorkspace(req)
	if err != nil {
		return nil, err
	}

	if req.Timeout > 0 {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, req.Timeout)
			defer cancel()
		}
	}

	stdout := &limitBuffer{limit: cliOutputLimit}
	stderr := &limitBuffer{limit: cliOutputLimit}
	input, err := json.Marshal(nodeSDKRequest{
		Model:        e.modelID,
		WorkspaceDir: workspaceDir,
		Prompt:       req.Message,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal %s request: %w", e.engineType, err)
	}
	cmd := exec.CommandContext(ctx, e.nodePath, "--input-type=module", "-e", e.script)
	cmd.Dir = workspaceDir
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = cmd.Run()
	output := stdout.String()
	if stdout.truncated {
		output += "\n[stdout truncated]"
	}
	errorMsg := stderr.String()
	if stderr.truncated {
		errorMsg += "\n[stderr truncated]"
	}
	if err != nil {
		if errorMsg == "" {
			errorMsg = err.Error()
		} else {
			errorMsg = errorMsg + "\n" + err.Error()
		}
	}
	var sdkResp nodeSDKResponse
	if output != "" {
		if parseErr := json.Unmarshal([]byte(output), &sdkResp); parseErr == nil {
			output = sdkResp.FinalOutput
			if sdkResp.Error != "" {
				if errorMsg == "" {
					errorMsg = sdkResp.Error
				} else {
					errorMsg = errorMsg + "\n" + sdkResp.Error
				}
			}
		} else if err == nil {
			errorMsg = fmt.Sprintf("failed to parse %s SDK response: %v", e.engineType, parseErr)
		}
	}

	return &ExecutionResponse{
		FinalOutput:    output,
		Events:         []copilot.SessionEvent{},
		ModelID:        e.modelID,
		DurationMs:     time.Since(start).Milliseconds(),
		ToolCalls:      []models.ToolCall{},
		ErrorMsg:       errorMsg,
		Success:        err == nil,
		WorkspaceDir:   workspaceDir,
		WorkspaceFiles: captureWorkspaceFiles(workspaceDir),
	}, nil
}

func (e *NodeSDKEngine) prepareWorkspace(req *ExecutionRequest) (string, error) {
	if req.WorkspaceDir != "" {
		e.workspace = req.WorkspaceDir
		return req.WorkspaceDir, nil
	}
	if e.workspace != "" {
		if err := os.RemoveAll(e.workspace); err != nil {
			return "", fmt.Errorf("failed to remove old %s workspace %s: %w", e.engineType, e.workspace, err)
		}
		e.workspace = ""
	}
	tmpDir, err := os.MkdirTemp("", "waza-"+e.engineType+"-*")
	if err != nil {
		return "", fmt.Errorf("failed to create %s workspace: %w", e.engineType, err)
	}
	e.workspace = tmpDir
	if err := setupWorkspaceResources(e.workspace, req.Resources); err != nil {
		return "", fmt.Errorf("failed to setup %s workspace resources: %w", e.engineType, err)
	}
	return e.workspace, nil
}

func (e *NodeSDKEngine) Shutdown(ctx context.Context) error {
	if e.workspace == "" || e.keepWorkspace {
		return nil
	}
	if err := os.RemoveAll(e.workspace); err != nil {
		return fmt.Errorf("failed to remove %s workspace %s: %w", e.engineType, e.workspace, err)
	}
	e.workspace = ""
	return nil
}

func (e *NodeSDKEngine) SessionUsage(sessionID string) *models.UsageStats {
	return nil
}

func checkNodePackage(ctx context.Context, nodePath, packageName string) error {
	script := fmt.Sprintf("await import(%q)", packageName)
	cmd := exec.CommandContext(ctx, nodePath, "--input-type=module", "-e", script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("npm package %s is required for this engine: %s", packageName, msg)
	}
	return nil
}

type nodeSDKRequest struct {
	Model        string `json:"model"`
	WorkspaceDir string `json:"workspaceDir"`
	Prompt       string `json:"prompt"`
}

type nodeSDKResponse struct {
	FinalOutput string `json:"finalOutput"`
	Error       string `json:"error,omitempty"`
}

type limitBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		b.truncated = true
		return len(p), nil
	}
	remaining := b.limit - b.Buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.truncated = true
		_, _ = b.Buffer.Write(p[:remaining])
		return len(p), nil
	}
	_, _ = b.Buffer.Write(p)
	return len(p), nil
}

const claudeAgentSDKScript = `
import { query } from "@anthropic-ai/claude-agent-sdk";

const chunks = [];
for await (const chunk of process.stdin) chunks.push(chunk);
const input = JSON.parse(Buffer.concat(chunks).toString("utf8"));

let finalOutput = "";
let error = "";
try {
  for await (const message of query({
    prompt: input.prompt,
    options: {
      cwd: input.workspaceDir,
      model: input.model,
    },
  })) {
    if (message?.type === "result") {
      if (typeof message.result === "string") finalOutput = message.result;
      if (message.subtype && message.subtype !== "success") error = message.subtype;
    } else if (message?.type === "assistant" && Array.isArray(message.message?.content)) {
      for (const block of message.message.content) {
        if (block?.type === "text" && typeof block.text === "string") finalOutput += block.text;
      }
    }
  }
} catch (err) {
  error = err instanceof Error ? err.message : String(err);
}
process.stdout.write(JSON.stringify({ finalOutput, error }));
`

const codexSDKScript = `
import { Codex } from "@openai/codex-sdk";

const chunks = [];
for await (const chunk of process.stdin) chunks.push(chunk);
const input = JSON.parse(Buffer.concat(chunks).toString("utf8"));

let finalOutput = "";
let error = "";
try {
  const codex = new Codex();
  const thread = codex.startThread({
    model: input.model,
    workingDirectory: input.workspaceDir,
    skipGitRepoCheck: true,
  });
  const turn = await thread.run(input.prompt);
  finalOutput = turn.finalResponse || "";
} catch (err) {
  error = err instanceof Error ? err.message : String(err);
}
process.stdout.write(JSON.stringify({ finalOutput, error }));
`
