package acp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestProcessInitialize(t *testing.T) {
	// 启动一个没有特定模型参数的默认子进程测试
	p, err := StartProcess("")
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}
	defer p.Stop()

	// 1. 测试 Initialize
	initParams := InitializeParams{
		ProtocolVersion:    1,
		ClientCapabilities: map[string]struct{}{},
		ClientInfo: ClientInfo{
			Name:    "acp-test",
			Version: "1.0.0",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	respRaw, err := p.SendRequest(ctx, "initialize", initParams)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	var resp ResponseBase
	if err := json.Unmarshal(respRaw, &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("ACP returned error: %s", resp.Error.Message)
	}

	t.Logf("Initialize Response: %s", string(respRaw))

	// 解析看看是不是拿到了 agentInfo
	var initResult map[string]interface{}
	json.Unmarshal(resp.Result, &initResult)
	if info, ok := initResult["agentInfo"].(map[string]interface{}); ok {
		t.Logf("Agent Name: %v, Version: %v", info["name"], info["version"])
	} else {
		t.Errorf("Missing agentInfo in initialize response")
	}
}
