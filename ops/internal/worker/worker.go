// Package worker is the panel's job coordinator. It runs one goroutine that
// wakes every second (or on demand) and runs a fixed set of passes, each in its
// own short write transaction. It decides which jobs are ready, enforces
// per-lock-key exclusivity, expires lost leases, and finishes deploys. It never
// connects to any server.
package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/mwita-lnx/RedCi/ops/internal/events"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// Waker is signalled when a job is promoted for a server, so a waiting
// long-poll returns immediately. The server implements this.
type Waker interface {
	Wake(serverID int64)
}

// Worker coordinates jobs.
type Worker struct {
	db      *store.DB
	bus     *events.Bus
	waker   Waker
	alerter *Alerter
	log     *slog.Logger
	wake    chan struct{}

	offlineAfter  time.Duration
	lastHousekeep time.Time
}

// New builds a Worker.
func New(db *store.DB, bus *events.Bus, waker Waker, log *slog.Logger) *Worker {
	return &Worker{
		db:           db,
		bus:          bus,
		waker:        waker,
		log:          log,
		wake:         make(chan struct{}, 1),
		offlineAfter: 60 * time.Second,
	}
}

// SetAlerter wires an outgoing alert webhook.
func (w *Worker) SetAlerter(a *Alerter) { w.alerter = a }

func (w *Worker) alert(ctx context.Context, text string) {
	if w.alerter != nil {
		w.alerter.Send(ctx, text)
	}
}

// Nudge asks the worker to run its passes now instead of waiting for the tick.
func (w *Worker) Nudge() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run drives the worker until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		case <-w.wake:
		}
		for _, pass := range []func(context.Context) error{
			w.expireLeases, w.applyCancels, w.promote, w.finishDeploys, w.markOffline, w.housekeeping,
		} {
			if err := pass(ctx); err != nil {
				w.log.Error("worker pass failed", "err", err)
			}
		}
	}
}

// expireLeases: claimed/running jobs past their lease become lost; their deploy
// is handled by finishDeploys on the next pass.
func (w *Worker) expireLeases(ctx context.Context) error {
	lost, err := w.db.WriteQ.ExpireLeases(ctx, nullInt(time.Now().Unix()))
	if err != nil {
		return err
	}
	for _, j := range lost {
		w.log.Warn("job lease expired", "job", j.ID, "server", j.ServerID)
		w.bus.Publish(events.Event{JobID: j.ID, Kind: events.KindStatus, Status: "lost"})
		w.bus.Publish(events.Event{JobID: j.ID, Kind: events.KindDone})
	}
	return nil
}

// applyCancels: queued/ready jobs with cancel_requested become cancelled.
func (w *Worker) applyCancels(ctx context.Context) error {
	return w.db.WriteQ.CancelJobsRequested(ctx)
}

// promote: a queued job becomes ready when every earlier job in its deploy has
// succeeded and no other job holds its lock. We signal the server's waiter.
func (w *Worker) promote(ctx context.Context) error {
	candidates, err := w.db.ReadQ.ReadyJobsToPromote(ctx)
	if err != nil {
		return err
	}
	for _, c := range candidates {
		// Earlier siblings in the same deploy must all be succeeded.
		if c.DeployID.Valid {
			notDone, err := w.db.ReadQ.EarlierSiblingsSucceeded(ctx, store.EarlierSiblingsSucceededParams{
				DeployID: c.DeployID, Seq: c.Seq,
			})
			if err != nil {
				return err
			}
			if notDone > 0 {
				continue
			}
		}
		// Lock must be free.
		if c.LockKey.Valid && c.LockKey.String != "" {
			held, err := w.db.ReadQ.LockHeld(ctx, c.LockKey)
			if err != nil {
				return err
			}
			if held > 0 {
				continue
			}
		}
		if err := w.db.WriteQ.PromoteJob(ctx, c.ID); err != nil {
			return err
		}
		w.log.Info("job ready", "job", c.ID, "server", c.ServerID)
		if w.waker != nil {
			w.waker.Wake(c.ServerID)
		}
	}
	return nil
}

// finishDeploys: a deploy succeeds when all its jobs succeeded; it fails (and
// its remaining jobs are cancelled) when any job failed, was lost or cancelled.
func (w *Worker) finishDeploys(ctx context.Context) error {
	deploys, err := w.db.ReadQ.ListRecentDeploys(ctx, 200)
	if err != nil {
		return err
	}
	for _, d := range deploys {
		if d.Status == "succeeded" || d.Status == "failed" || d.Status == "cancelled" {
			continue
		}
		counts, err := w.db.ReadQ.CountDeployJobsByStatus(ctx, nullInt(d.ID))
		if err != nil {
			return err
		}
		if counts.Total == 0 {
			continue
		}
		switch {
		case counts.Bad > 0:
			// Cancel any not-yet-run jobs, then fail the deploy.
			if err := w.cancelRemaining(ctx, d.ID); err != nil {
				return err
			}
			_ = w.db.WriteQ.FinishDeploy(ctx, store.FinishDeployParams{Status: "failed", ID: d.ID})
			w.log.Warn("deploy failed", "deploy", d.ID)
			w.alert(ctx, fmt.Sprintf("RedCi: deploy #%d (%s) failed", d.ID, d.Kind))
		case counts.Succeeded == counts.Total:
			_ = w.db.WriteQ.FinishDeploy(ctx, store.FinishDeployParams{Status: "succeeded", ID: d.ID})
			w.log.Info("deploy succeeded", "deploy", d.ID)
		default:
			_ = w.db.WriteQ.SetDeployRunning(ctx, d.ID)
		}
	}
	return nil
}

// cancelRemaining cancels queued/ready jobs of a failing deploy.
func (w *Worker) cancelRemaining(ctx context.Context, deployID int64) error {
	jobs, err := w.db.ReadQ.ListJobsForDeploy(ctx, nullInt(deployID))
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Status == "queued" || j.Status == "ready" {
			_ = w.db.WriteQ.FinishJob(ctx, store.FinishJobParams{Status: "cancelled", ID: j.ID})
			w.bus.Publish(events.Event{JobID: j.ID, Kind: events.KindStatus, Status: "cancelled"})
		}
	}
	return nil
}

// markOffline: a server whose last heartbeat is older than the threshold.
func (w *Worker) markOffline(ctx context.Context) error {
	cutoff := time.Now().Add(-w.offlineAfter).Unix()
	return w.db.WriteQ.MarkServersOffline(ctx, nullInt(cutoff))
}

// housekeeping runs retention deletes hourly.
func (w *Worker) housekeeping(ctx context.Context) error {
	if time.Since(w.lastHousekeep) < time.Hour {
		return nil
	}
	w.lastHousekeep = time.Now()
	cutoff := time.Now().Add(-15 * time.Minute).Unix()
	_ = w.db.WriteQ.PurgeOldLoginAttempts(ctx, cutoff)
	return nil
}

func nullInt(i int64) sql.NullInt64 { return sql.NullInt64{Int64: i, Valid: true} }
