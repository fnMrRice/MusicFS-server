// Package inprocess 为受信任的编译内插件提供独立的内存通信通道。
package inprocess

import (
	"context"
	"io"
	"sync"
)

type Runner func(context.Context, io.ReadCloser, io.WriteCloser) error

// Worker 保留与进程插件相同的流边界，每次启动拥有独立上下文和状态。
type Worker struct {
	Input  io.WriteCloser
	Output io.ReadCloser
	Done   <-chan struct{}
	cancel context.CancelFunc
	close  func()
	once   sync.Once
}

func Start(parent context.Context, run Runner) *Worker {
	ctx, cancel := context.WithCancel(parent)
	input, send := io.Pipe()
	receive, output := io.Pipe()
	done := make(chan struct{})
	w := &Worker{Input: send, Output: receive, Done: done, cancel: cancel}
	w.close = func() {
		_ = input.Close()
		_ = send.Close()
		_ = receive.Close()
		_ = output.Close()
	}
	go func() {
		defer close(done)
		defer w.Close()
		_ = run(ctx, input, output)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = w.Close()
		case <-done:
		}
	}()
	return w
}

// Close 同时中止读写，避免取消时阻塞在没有读取方的内存管道上。
func (w *Worker) Close() error {
	w.once.Do(func() {
		w.cancel()
		w.close()
	})
	return nil
}
