package pool

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
	"rag_robot/internal/pkg/logger"
)

// Task 任务接口
type Task func(ctx context.Context) error

// WorkerPool 协程池
// 核心思路：
// 1. 预先创建固定数量的 worker goroutine
// 2. 任务提交到任务队列
// 3. worker 从队列中取任务执行
// 4. 避免无限创建 goroutine 导致资源耗尽
//
// 为什么需要协程池：
// 1. 控制并发数量，避免 goroutine 爆炸
// 2. 复用 goroutine，减少创建销毁开销
// 3. 任务排队，避免系统过载
type WorkerPool struct {
	workerCount int       // worker 数量
	taskQueue   chan Task // 任务队列
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewWorkerPool 创建协程池
// 参数：
//   - workerCount: worker 数量（建议设置为 CPU 核心数的 2-4 倍）
//   - queueSize: 任务队列大小（建议设置为 workerCount 的 10-100 倍）
//
// 返回：
//   - *WorkerPool: 协程池实例
func NewWorkerPool(workerCount int, queueSize int) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	pool := &WorkerPool{
		workerCount: workerCount,
		taskQueue:   make(chan Task, queueSize),
		ctx:         ctx,
		cancel:      cancel,
	}

	// 启动 worker
	pool.start()

	return pool
}

// start 启动所有 worker
func (p *WorkerPool) start() {
	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// worker 工作协程
func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()

	logger.Debug("Worker started", zap.Int("worker_id", id))

	for {
		select {
		case task, ok := <-p.taskQueue:
			if !ok {
				// 任务队列已关闭
				logger.Debug("Worker stopped (queue closed)", zap.Int("worker_id", id))
				return
			}

			// 执行任务
			if err := task(p.ctx); err != nil {
				logger.Error("Task execution failed",
					zap.Int("worker_id", id),
					zap.Error(err))
			}

		case <-p.ctx.Done():
			// 收到退出信号
			logger.Debug("Worker stopped (context cancelled)", zap.Int("worker_id", id))
			return
		}
	}
}

// Submit 提交任务
// 参数：
//   - task: 任务函数
//
// 返回：
//   - error: 错误信息（队列满时返回错误）
func (p *WorkerPool) Submit(task Task) error {
	select {
	case p.taskQueue <- task:
		return nil
	default:
		// 任务队列已满
		return ErrQueueFull
	}
}

// Shutdown 关闭协程池
// 等待所有任务执行完成
func (p *WorkerPool) Shutdown() {
	// 1. 关闭任务队列（不再接收新任务）
	close(p.taskQueue)

	// 2. 取消上下文（通知 worker 退出）
	p.cancel()

	// 3. 等待所有 worker 退出
	p.wg.Wait()

	logger.Info("WorkerPool shutdown complete")
}

var ErrQueueFull = fmt.Errorf("task queue is full")
