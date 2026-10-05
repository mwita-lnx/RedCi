package agentcfg

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/mwita-lnx/RedCi/agent/internal/client"
	"github.com/mwita-lnx/RedCi/agent/internal/journal"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// logSink batches log lines and posts them about once a second. On a failed
// post it queues the batch in the outbox so nothing is lost during an outage.
type logSink struct {
	ctx   context.Context
	cli   *client.Client
	jrnl  *journal.Journal
	jobID int64

	mu    sync.Mutex
	seq   int64
	buf   []protocol.LogLine
	done  chan struct{}
	once  sync.Once
	tickG sync.WaitGroup
}

func newLogSink(ctx context.Context, cli *client.Client, jrnl *journal.Journal, jobID int64) *logSink {
	s := &logSink{ctx: ctx, cli: cli, jrnl: jrnl, jobID: jobID, done: make(chan struct{})}
	s.tickG.Add(1)
	go s.ticker()
	return s
}

// emit records a line (safe for concurrent stdout/stderr goroutines).
func (s *logSink) emit(stream, line string) {
	s.mu.Lock()
	s.buf = append(s.buf, protocol.LogLine{
		Seq: s.seq, TS: time.Now().Unix(), Stream: stream, Line: line,
	})
	s.seq++
	s.mu.Unlock()
}

func (s *logSink) ticker() {
	defer s.tickG.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.send()
		}
	}
}

// send posts the current batch (up to MaxLogBatch lines).
func (s *logSink) send() {
	s.mu.Lock()
	if len(s.buf) == 0 {
		s.mu.Unlock()
		return
	}
	batch := s.buf
	if len(batch) > protocol.MaxLogBatch {
		batch = batch[:protocol.MaxLogBatch]
	}
	s.buf = s.buf[len(batch):]
	s.mu.Unlock()

	if err := s.cli.SendLogs(s.ctx, s.jobID, batch); err != nil && s.jrnl != nil {
		payload, _ := json.Marshal(protocol.LogsRequest{Lines: batch})
		_ = s.jrnl.Enqueue(s.ctx, s.jobID, "logs", string(payload))
	}
}

// flush stops the ticker, waits for it to exit (so only this goroutine touches
// the buffer), then sends any remaining lines.
func (s *logSink) flush() {
	s.once.Do(func() { close(s.done) })
	s.tickG.Wait()
	for {
		s.mu.Lock()
		n := len(s.buf)
		s.mu.Unlock()
		if n == 0 {
			return
		}
		s.send()
	}
}
