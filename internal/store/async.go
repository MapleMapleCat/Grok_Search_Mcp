package store

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
)

// AsyncUsageWriter 将用量写入从请求路径解耦：主线程只入队，后台 goroutine 调用 Store。
type AsyncUsageWriter struct {
	store  Store
	ch     chan UsageRecord
	wg     sync.WaitGroup
	cancel context.CancelFunc

	// 可观测计数：缓冲丢弃与写库失败/成功（原子累加，便于运维与测试断言）。
	droppedRecords atomic.Uint64
	droppedTouches atomic.Uint64
	writeFailures  atomic.Uint64
	writeSuccesses atomic.Uint64
}

// AsyncUsageWriterStats 是异步用量写入器的快照统计。
type AsyncUsageWriterStats struct {
	DroppedRecords uint64
	DroppedTouches uint64
	WriteFailures  uint64
	WriteSuccesses uint64
	QueueLength    int
	QueueCapacity  int
}

// NewAsyncUsageWriter 启动消费者；buffer 为满时 Enqueue 会丢弃并打日志（不阻塞 MCP）。
func NewAsyncUsageWriter(s Store, buffer int) *AsyncUsageWriter {
	if buffer <= 0 {
		buffer = 256
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &AsyncUsageWriter{
		store:  s,
		ch:     make(chan UsageRecord, buffer),
		cancel: cancel,
	}
	w.wg.Add(1)
	go w.run(ctx)
	return w
}

func (w *AsyncUsageWriter) run(ctx context.Context) {
	defer w.wg.Done()
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case rec := <-w.ch:
					w.write(rec)
				default:
					return
				}
			}
		case rec := <-w.ch:
			w.write(rec)
		}
	}
}

func (w *AsyncUsageWriter) write(rec UsageRecord) {
	if rec.TouchKey {
		if err := w.store.TouchKeyUsage(context.Background(), rec.KeyID); err != nil {
			failures := w.writeFailures.Add(1)
			log.Printf("touch key usage failed key=%s failures=%d: %v", rec.KeyID, failures, err)
			return
		}
		w.writeSuccesses.Add(1)
		return
	}
	if err := w.store.RecordUsage(context.Background(), rec); err != nil {
		failures := w.writeFailures.Add(1)
		log.Printf("usage record write failed key=%s tool=%s failures=%d: %v", rec.KeyID, rec.ToolName, failures, err)
		return
	}
	w.writeSuccesses.Add(1)
}

// Enqueue 非阻塞入队；channel 已满时丢弃本条记录并累加计数。
func (w *AsyncUsageWriter) Enqueue(rec UsageRecord) {
	select {
	case w.ch <- rec:
	default:
		if rec.TouchKey {
			dropped := w.droppedTouches.Add(1)
			log.Printf("touch key usage dropped (buffer full) key=%s dropped_touches=%d queue_cap=%d",
				rec.KeyID, dropped, cap(w.ch))
			return
		}
		dropped := w.droppedRecords.Add(1)
		log.Printf("usage record dropped (buffer full) key=%s tool=%s dropped_records=%d queue_cap=%d",
			rec.KeyID, rec.ToolName, dropped, cap(w.ch))
	}
}

// Stats 返回丢弃/写库计数与当前队列深度快照。
func (w *AsyncUsageWriter) Stats() AsyncUsageWriterStats {
	return AsyncUsageWriterStats{
		DroppedRecords: w.droppedRecords.Load(),
		DroppedTouches: w.droppedTouches.Load(),
		WriteFailures:  w.writeFailures.Load(),
		WriteSuccesses: w.writeSuccesses.Load(),
		QueueLength:    len(w.ch),
		QueueCapacity:  cap(w.ch),
	}
}

// Close 取消后台循环并等待排空或放弃剩余队列（见 run 中 default 分支）。
// 必须在 Store.Close 之前调用：run 内部用 context.Background() 写入，若先关 Store 会触发数据库错误。
func (w *AsyncUsageWriter) Close() {
	w.cancel()
	w.wg.Wait()
}
