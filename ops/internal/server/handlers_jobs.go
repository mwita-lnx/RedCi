package server

import (
	"fmt"
	"html"
	"net/http"
	"strconv"
	"time"

	"github.com/mwita-lnx/RedCi/ops/internal/events"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/ops/web/components"
)

func (s *Server) handleDeploys(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	rows, err := s.db.ReadQ.ListRecentDeploys(r.Context(), 100)
	if err != nil {
		http.Error(w, "could not load deploys", http.StatusInternalServerError)
		return
	}
	render(w, r, components.Deploys(user, rows))
}

func (s *Server) handleJobDetail(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	job, err := s.db.ReadQ.GetJob(r.Context(), pathID(r))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// -1 so the first line (seq 0) is included; the query filters seq > ?.
	logs, _ := s.db.ReadQ.ListJobLogs(r.Context(), store.ListJobLogsParams{JobID: job.ID, Seq: -1})
	render(w, r, components.JobDetail(user, job, logs))
}

// handleJobStream is the SSE endpoint. It replays stored log lines (honouring
// Last-Event-ID), then subscribes to the event bus. A comment line every 15s
// keeps proxies from closing the connection.
func (s *Server) handleJobStream(w http.ResponseWriter, r *http.Request) {
	jobID := pathID(r)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // tell nginx not to buffer

	// Subscribe BEFORE the replay so no event is missed between the two.
	ch, unsubscribe := s.bus.Subscribe(jobID)
	defer unsubscribe()

	// Replay stored lines after Last-Event-ID.
	// -1 so a fresh stream (no Last-Event-ID) replays from seq 0; the query
	// filters seq > ?, so resumption passes the last seen seq.
	lastSeq := int64(-1)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		lastSeq, _ = strconv.ParseInt(v, 10, 64)
	}
	logs, _ := s.db.ReadQ.ListJobLogs(r.Context(), store.ListJobLogsParams{JobID: jobID, Seq: lastSeq})
	for _, ln := range logs {
		writeSSELog(w, ln.Seq, ln.Stream, ln.Line)
	}
	// If the job already finished, emit its status and close.
	if job, err := s.db.ReadQ.GetJob(r.Context(), jobID); err == nil {
		if isTerminal(job.Status) {
			writeSSEStatus(w, job.Status)
			writeSSEDone(w)
			flusher.Flush()
			return
		}
	}
	flusher.Flush()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case e, open := <-ch:
			if !open {
				return
			}
			switch e.Kind {
			case events.KindLog:
				writeSSELog(w, e.Seq, e.Stream, e.Line)
			case events.KindStatus:
				writeSSEStatus(w, e.Status)
			case events.KindDone:
				writeSSEDone(w)
				flusher.Flush()
				return
			}
			flusher.Flush()
		}
	}
}

func isTerminal(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled", "lost":
		return true
	}
	return false
}

// writeSSELog emits a `log` event whose data is an HTML fragment (htmx swaps it
// into the log <div> with beforeend).
func writeSSELog(w http.ResponseWriter, seq int64, stream, line string) {
	frag := fmt.Sprintf(`<div class="logline %s">%s</div>`, html.EscapeString(stream), html.EscapeString(line))
	fmt.Fprintf(w, "id: %d\nevent: log\ndata: %s\n\n", seq, frag)
}

func writeSSEStatus(w http.ResponseWriter, status string) {
	frag := fmt.Sprintf(`<span class="badge %s">%s</span>`, html.EscapeString(status), html.EscapeString(status))
	fmt.Fprintf(w, "event: status\ndata: %s\n\n", frag)
}

func writeSSEDone(w http.ResponseWriter) {
	fmt.Fprint(w, "event: done\ndata: done\n\n")
}
