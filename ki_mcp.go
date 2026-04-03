// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
// MCP (Model Context Protocol) client integration.
// Allows the KI to use external tools via MCP servers configured in ~/.kish/config.yaml
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type MCPServer struct {
	Name      string   `yaml:"name"`
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args,omitempty"`
	Env       []string `yaml:"env,omitempty"`
	AutoStart bool     `yaml:"auto_start"`
}

type MCPClient struct {
	mu      sync.Mutex
	servers map[string]*mcpConnection
}

type mcpConnection struct {
	config  MCPServer
	process *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	running bool
}

var mcpClient *MCPClient

func initMCP(servers []MCPServer) {
	mcpClient = &MCPClient{
		servers: make(map[string]*mcpConnection),
	}
	for _, srv := range servers {
		mcpClient.servers[srv.Name] = &mcpConnection{config: srv}
		if srv.AutoStart {
			mcpClient.Start(srv.Name)
		}
	}
}

func (mc *MCPClient) Start(name string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	conn, ok := mc.servers[name]
	if !ok {
		return fmt.Errorf("mcp: unknown server '%s'", name)
	}
	if conn.running {
		return nil
	}

	cmd := exec.Command(conn.config.Command, conn.config.Args...)
	cmd.Env = append(os.Environ(), conn.config.Env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp: stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcp: start %s: %w", name, err)
	}

	conn.process = cmd
	conn.stdin = stdin
	conn.stdout = stdout
	conn.running = true

	vPrint(1, "MCP server '%s' started (pid %d)", name, cmd.Process.Pid)
	return nil
}

func (mc *MCPClient) Stop(name string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	conn, ok := mc.servers[name]
	if !ok || !conn.running {
		return nil
	}
	conn.stdin.Close()
	conn.process.Process.Kill()
	conn.process.Wait()
	conn.running = false
	vPrint(1, "MCP server '%s' stopped", name)
	return nil
}

func (mc *MCPClient) StopAll() {
	if mc == nil {
		return
	}
	for name := range mc.servers {
		mc.Stop(name)
	}
}

func (mc *MCPClient) Call(serverName, method string, params interface{}) (json.RawMessage, error) {
	mc.mu.Lock()
	conn, ok := mc.servers[serverName]
	mc.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("mcp: unknown server '%s'", serverName)
	}
	if !conn.running {
		if err := mc.Start(serverName); err != nil {
			return nil, err
		}
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	mc.mu.Lock()
	_, err = conn.stdin.Write(append(reqBytes, '\n'))
	mc.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("mcp: write to %s: %w", serverName, err)
	}

	buf := make([]byte, 65536)
	n, err := conn.stdout.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("mcp: read from %s: %w", serverName, err)
	}

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("mcp: parse response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp: %s error %d: %s", serverName, resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

func (mc *MCPClient) ListTools() []string {
	if mc == nil {
		return nil
	}
	var tools []string
	for name, conn := range mc.servers {
		status := "stopped"
		if conn.running {
			status = "running"
		}
		tools = append(tools, fmt.Sprintf("%s (%s) [%s]", name, conn.config.Command, status))
	}
	return tools
}

func (mc *MCPClient) FormatForPrompt() string {
	if mc == nil {
		return ""
	}
	tools := mc.ListTools()
	if len(tools) == 0 {
		return ""
	}
	return "Verfügbare MCP-Tools:\n" + strings.Join(tools, "\n")
}
