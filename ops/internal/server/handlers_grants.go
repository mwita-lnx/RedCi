package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/mwita-lnx/RedCi/ops/internal/auth"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// GrantResource adds or updates a per-resource role grant for a user. Shared by
// the admin API and the CLI. resourceType is "site" or "bench"; role is one of
// the panel roles. Re-granting the same (user, resource) replaces the role.
func (s *Server) GrantResource(ctx context.Context, userID int64, resourceType string, resourceID int64, role string) error {
	if resourceType != "site" && resourceType != "bench" {
		return fmt.Errorf("resource_type must be site or bench")
	}
	if !auth.Role(role).Valid() {
		return fmt.Errorf("role must be viewer, deployer or admin")
	}
	// Validate the target exists so we don't store dangling grants.
	switch resourceType {
	case "site":
		if _, err := s.db.ReadQ.GetSite(ctx, resourceID); err != nil {
			return fmt.Errorf("site %d not found", resourceID)
		}
	case "bench":
		if _, err := s.db.ReadQ.GetBench(ctx, resourceID); err != nil {
			return fmt.Errorf("bench %d not found", resourceID)
		}
	}
	if _, err := s.db.ReadQ.GetUserByID(ctx, userID); err != nil {
		return fmt.Errorf("user %d not found", userID)
	}
	_, err := s.db.WriteQ.UpsertGrant(ctx, store.UpsertGrantParams{
		UserID: userID, ResourceType: resourceType, ResourceID: resourceID, Role: role,
	})
	return err
}

// apiListGrants returns a user's grants (admin only).
func (s *Server) apiListGrants(w http.ResponseWriter, r *http.Request) {
	uid := pathID(r)
	grants, err := s.db.ReadQ.ListGrantsForUser(r.Context(), uid)
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]any, 0, len(grants))
	for _, g := range grants {
		out = append(out, map[string]any{
			"id": g.ID, "resource_type": g.ResourceType,
			"resource_id": g.ResourceID, "role": g.Role,
		})
	}
	writeJSONAPI(w, http.StatusOK, out)
}

// apiAddGrant adds/updates a grant on a user (admin only).
func (s *Server) apiAddGrant(w http.ResponseWriter, r *http.Request) {
	uid := pathID(r)
	var in struct {
		ResourceType string `json:"resource_type"`
		ResourceID   int64  `json:"resource_id"`
		Role         string `json:"role"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.GrantResource(r.Context(), uid, in.ResourceType, in.ResourceID, in.Role); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "user.grant", "user", uid, fmt.Sprintf("%s:%d=%s", in.ResourceType, in.ResourceID, in.Role))
	writeJSONAPI(w, http.StatusOK, map[string]any{"ok": true})
}

// apiDeleteGrant removes a grant (admin only). Path: /users/{id}/grants/{grantId}.
func (s *Server) apiDeleteGrant(w http.ResponseWriter, r *http.Request) {
	uid := pathID(r)
	gid, _ := strconv.ParseInt(r.PathValue("grantId"), 10, 64)
	if err := s.db.WriteQ.DeleteGrant(r.Context(), store.DeleteGrantParams{ID: gid, UserID: uid}); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "user.revoke", "user", uid, fmt.Sprintf("grant:%d", gid))
	writeJSONAPI(w, http.StatusOK, map[string]any{"ok": true})
}
