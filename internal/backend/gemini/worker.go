package gemini

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/yourname/acp-openai-proxy/internal/acp"
)

// Session 包装了一个从 Worker 派生出来的单次逻辑会话生命周期
type Session struct {
	ID     string
	Worker *Worker
}

// Close 结束这个短会话
func (s *Session) Close() {
	s.Worker.CloseSession(s.ID)
}

// Worker 代表一个保持活跃的模型进城实例
type Worker struct {
	model string
	proc  *acp.Process

	mu         sync.Mutex
	lastUsedAt time.Time
}

// NewWorker 启动子进程并执行 Initialize 握手
func NewWorker(model string) (*Worker, error) {
	proc, err := acp.StartProcess(model)
	if err != nil {
		return nil, err
	}

	w := &Worker{
		model:      model,
		proc:       proc,
		lastUsedAt: time.Now(),
	}

	if err := w.initialize(); err != nil {
		proc.Stop()
		return nil, fmt.Errorf("worker init failed: %v", err)
	}

	return w, nil
}

func (w *Worker) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	params := acpsdk.InitializeRequest{
		ProtocolVersion:    1,
		ClientCapabilities: acpsdk.ClientCapabilities{},
		ClientInfo: &acpsdk.Implementation{
			Name:    "openai-bridge",
			Version: "1.0.0",
		},
	}

	_, err := w.proc.Agent.Initialize(ctx, params)
	return err
}

// Touch 更新存活游标 (防止 LRU 剔除)
func (w *Worker) Touch() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastUsedAt = time.Now()
}

// CreateSession 每次大语言模型请求分配一个临时的 sessionId
func (w *Worker) CreateSession(ctx context.Context, cwd string) (*Session, error) {
	w.Touch()

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	res, err := w.proc.Agent.NewSession(reqCtx, acpsdk.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		return nil, err
	}

	return &Session{
		ID:     string(res.SessionId),
		Worker: w,
	}, nil
}

func (w *Worker) CloseSession(sessionID string) {
	err := w.proc.Agent.Cancel(context.Background(), acpsdk.CancelNotification{
		SessionId: acpsdk.SessionId(sessionID),
	})
	if err != nil {
		log.Printf("session/cancel err for %s: %v", sessionID, err)
	}

	// 为了不积压内存，停掉流监控
	w.proc.UnsubscribeStream(sessionID)
}

// Stop 停止这个预热实例
func (w *Worker) Stop() {
	w.proc.Stop()
}

// GetProcess 暴露底层句柄用于后续流式 prompt 发送
func (w *Worker) GetProcess() *acp.Process {
	return w.proc
}
