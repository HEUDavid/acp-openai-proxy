package gemini

import (
	"log"
	"sync"
	"time"
)

// Manager 管理不同模型的 Worker 生命期
type Manager struct {
	mu      sync.Mutex
	workers map[string]*Worker

	loading sync.Map // model -> *sync.Mutex
}

// NewManager 创建并启动一个新的模型 Worker 调度器
func NewManager() *Manager {
	m := &Manager{
		workers: make(map[string]*Worker),
	}
	go m.gcLoop()
	return m
}

func (m *Manager) GetWorker(model string) (*Worker, error) {
	m.mu.Lock()
	w, ok := m.workers[model]
	if ok {
		m.mu.Unlock()
		w.Touch()
		return w, nil
	}
	m.mu.Unlock()

	// 获取或创建该模型的加载锁，防止并发拉起多个同样的子进程
	actual, _ := m.loading.LoadOrStore(model, &sync.Mutex{})
	lm := actual.(*sync.Mutex)

	lm.Lock()
	defer lm.Unlock()

	// Double check
	m.mu.Lock()
	w, ok = m.workers[model]
	m.mu.Unlock()
	if ok {
		w.Touch()
		return w, nil
	}

	log.Printf("[pool] Cold starting new ACP worker for model: '%s'", model)
	w, err := NewWorker(model)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.workers[model] = w
	m.mu.Unlock()
	return w, nil
}

// gcLoop 每分钟检查并回收长期空闲的 Worker
func (m *Manager) gcLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.cleanIdleWorkers(30 * time.Minute)
	}
}

func (m *Manager) cleanIdleWorkers(maxIdle time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for model, w := range m.workers {
		w.mu.Lock()
		idle := now.Sub(w.lastUsedAt)
		w.mu.Unlock()

		if idle > maxIdle {
			log.Printf("[pool] Worker for model '%s' idle for %v, shutting down...", model, idle)
			w.Stop()
			delete(m.workers, model)
			m.loading.Delete(model)
		}
	}
}
