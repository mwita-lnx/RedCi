package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// webhookSecretName is the panel secret holding the GitHub webhook secret.
const webhookSecretName = "github_webhook_secret"

// maxWebhookBody caps the webhook payload we read.
const maxWebhookBody = 5 << 20 // 5 MB

// pushPayload is the subset of GitHub's push event we use.
type pushPayload struct {
	Ref        string `json:"ref"` // refs/heads/<branch>
	After      string `json:"after"`
	Repository struct {
		FullName string `json:"full_name"` // owner/name
	} `json:"repository"`
}

// handleGithubWebhook verifies the HMAC signature, dedupes the delivery, and
// for a push to a configured branch creates one deploy per target. It responds
// within milliseconds; the worker does the slow work.
func (s *Server) handleGithubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	// Verify signature with a constant-time compare.
	secret, err := s.openSecret(r.Context(), webhookSecretName)
	if err != nil {
		s.log.Error("webhook secret not configured", "err", err)
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}
	if !validSignature([]byte(secret), body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	if event == "ping" {
		writeJSON(w, http.StatusOK, map[string]string{"msg": "pong"})
		return
	}

	// Dedupe redeliveries by X-GitHub-Delivery: if we have seen this id before,
	// ack without reprocessing.
	delivery := r.Header.Get("X-GitHub-Delivery")
	if delivery != "" {
		if _, err := s.db.ReadQ.GetWebhookDelivery(r.Context(), delivery); err == nil {
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = s.db.WriteQ.RecordWebhookDelivery(r.Context(), store.RecordWebhookDeliveryParams{
			DeliveryID: delivery, Event: event,
		})
	}

	if event != "push" {
		w.WriteHeader(http.StatusOK) // ignore other events
		return
	}

	var p pushPayload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	branch := strings.TrimPrefix(p.Ref, "refs/heads/")

	// Respond 202 immediately; create deploys synchronously but cheaply (just
	// row inserts), then the worker picks them up.
	created := s.dispatchPush(r.Context(), p.Repository.FullName, branch, p.After)
	s.auditSystem(r, "webhook.push", "repo", 0, p.Repository.FullName+"@"+branch)
	s.log.Info("webhook push", "repo", p.Repository.FullName, "branch", branch, "deploys", created)
	writeJSON(w, http.StatusAccepted, map[string]int{"deploys": created})
}

// dispatchPush creates deploys for every app source matching repo+branch.
func (s *Server) dispatchPush(ctx context.Context, repo, branch, commit string) int {
	sources, err := s.db.ReadQ.FindAppSourcesByRepoBranch(ctx, store.FindAppSourcesByRepoBranchParams{
		Repo: repo, Branch: branch,
	})
	if err != nil {
		s.log.Error("find app sources failed", "err", err)
		return 0
	}
	total := 0
	for _, src := range sources {
		switch src.Kind {
		case "frappe":
			n, err := s.deployFrappeAppForPush(ctx, src, commit)
			if err != nil {
				s.log.Error("frappe deploy build failed", "source", src.Name, "err", err)
			}
			total += n
		case "web":
			n, err := s.deployWebAppForPush(ctx, src, commit)
			if err != nil {
				s.log.Error("web deploy build failed", "source", src.Name, "err", err)
			}
			total += n
		}
	}
	s.nudge()
	return total
}

// validSignature checks the X-Hub-Signature-256 header (sha256=<hex>).
func validSignature(secret []byte, body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}
