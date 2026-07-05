package store

import (
	"context"
	"log"
	"sync"
)

// AsyncUsageWriter 将用量写入从请求路径解耦：主线程只入队，后台 goroutine 调用 Store。
type AsyncUsageWriter struct {
	store Store
	ch    chan UsageRecord
	wg    sync.WaitGroup
	cancel context.CancelFunc
}

// NewAsyncUsageWriter 启动消费者；buffer 为满时 Enqueue 会丢弃并打日志（不阻塞 MCP）。
func NewAsyncUsageWriter(s Store, buffer int) *AsyncUsageWriter {
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
			log.Printf("touch key usage failed: %v", err)
		}
		return
	}
	if err := w.store.RecordUsage(context.Background(), rec); err != nil {
		log.Printf("usage record write failed: %v", err)
	}
}

// Enqueue 非阻塞入队；channel 已满时丢弃本条记录。
func (w *AsyncUsageWriter) Enqueue(rec UsageRecord) {
	select {
	case w.ch <- rec:
	default:
		if rec.TouchKey {
			log.Printf("touch key usage dropped (buffer full) key=%s", rec.KeyID)
			return
		}
		log.Printf("usage record dropped (buffer full) key=%s tool=%s", rec.KeyID, rec.ToolName)
	}
}

// Close 取消后台循环并等待排空或放弃剩余队列（见 run 中 default 分支）。
// 必须在 Store.Close 之前调用：run 内部用 context.Background() 写入，若先关 Store 会触发数据库错误。
func (w *AsyncUsageWriter) Close() {
	w.cancel()
	w.wg.Wait()
}