package acp

import (
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"sync/atomic"

	acpsdk "github.com/coder/acp-go-sdk"
)

// Process 包装了底层通过 acp-go-sdk 驱动的子进程
type Process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	status int32 // 0: init, 1: running, 2: stopped

	Agent acpsdk.Agent

	streamMu sync.Mutex
	streams  map[string]chan acpsdk.SessionNotification // sessionID -> event channel
}

// BridgeClient 实现了 acp-go-sdk 的 Client 接口
type BridgeClient struct {
	proc *Process
}

// StartProcess 拉起一个 ACP 子进程，并初始化 acp-go-sdk 客户端连接
func StartProcess(model string) (*Process, error) {
	args := []string{"--acp"}
	if model != "" {
		args = append(args, "--model", model)
	}

	cmd := exec.Command("gemini", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe error: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe error: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start gemini --acp: %w", err)
	}

	p := &Process{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		status:  1,
		streams: make(map[string]chan acpsdk.SessionNotification),
	}

	bc := &BridgeClient{proc: p}
	// 创建连接：以 client 身份连接 agent
	conn := acpsdk.NewClientSideConnection(bc, stdin, stdout)
	p.Agent = conn

	go p.waitLoop()

	return p, nil
}

func (p *Process) Stop() {
	if !atomic.CompareAndSwapInt32(&p.status, 1, 2) {
		return
	}
	p.cmd.Process.Kill()
}

func (p *Process) waitLoop() {
	err := p.cmd.Wait()
	atomic.StoreInt32(&p.status, 2)
	log.Printf("ACP process exited: %v", err)

	p.streamMu.Lock()
	for _, ch := range p.streams {
		close(ch)
	}
	p.streams = make(map[string]chan acpsdk.SessionNotification)
	p.streamMu.Unlock()
}

// SubscribeStream 注册某个 session 的流式监听通道
func (p *Process) SubscribeStream(sessionID string) <-chan acpsdk.SessionNotification {
	p.streamMu.Lock()
	defer p.streamMu.Unlock()

	ch := make(chan acpsdk.SessionNotification, 100)
	p.streams[sessionID] = ch
	return ch
}

// UnsubscribeStream 注销监听
func (p *Process) UnsubscribeStream(sessionID string) {
	p.streamMu.Lock()
	defer p.streamMu.Unlock()
	if ch, ok := p.streams[sessionID]; ok {
		close(ch)
		delete(p.streams, sessionID)
	}
}

// ---- 实现 acpsdk.Client 接口 ----

func (bc *BridgeClient) SessionUpdate(ctx context.Context, params acpsdk.SessionNotification) error {
	sessionID := string(params.SessionId)

	bc.proc.streamMu.Lock()
	ch, ok := bc.proc.streams[sessionID]
	bc.proc.streamMu.Unlock()

	if ok {
		select {
		case ch <- params:
		default:
			log.Printf("warning: stream chan full for session %s", sessionID)
		}
	}
	return nil
}

func (bc *BridgeClient) RequestPermission(ctx context.Context, params acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	if len(params.Options) > 0 {
		return acpsdk.RequestPermissionResponse{
			Outcome: acpsdk.NewRequestPermissionOutcomeSelected(params.Options[0].OptionId),
		}, nil
	}
	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.NewRequestPermissionOutcomeCancelled(),
	}, nil
}

// 其他未实现的方法

func (bc *BridgeClient) ReadTextFile(ctx context.Context, params acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	return acpsdk.ReadTextFileResponse{}, acpsdk.NewMethodNotFound("fs.readTextFile")
}

func (bc *BridgeClient) WriteTextFile(ctx context.Context, params acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	return acpsdk.WriteTextFileResponse{}, acpsdk.NewMethodNotFound("fs.writeTextFile")
}

func (bc *BridgeClient) CreateTerminal(ctx context.Context, params acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{}, acpsdk.NewMethodNotFound("terminal.create")
}

func (bc *BridgeClient) KillTerminalCommand(ctx context.Context, params acpsdk.KillTerminalCommandRequest) (acpsdk.KillTerminalCommandResponse, error) {
	return acpsdk.KillTerminalCommandResponse{}, acpsdk.NewMethodNotFound("terminal.kill")
}

func (bc *BridgeClient) TerminalOutput(ctx context.Context, params acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, acpsdk.NewMethodNotFound("terminal.output")
}

func (bc *BridgeClient) ReleaseTerminal(ctx context.Context, params acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, acpsdk.NewMethodNotFound("terminal.release")
}

func (bc *BridgeClient) WaitForTerminalExit(ctx context.Context, params acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, acpsdk.NewMethodNotFound("terminal.wait")
}
