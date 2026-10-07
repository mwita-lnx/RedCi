package jobs

import (
	"encoding/json"
	"fmt"
	"time"
)

// Type is a job type name. These strings are stored in jobs.type and sent over
// the wire, so they must never change once released.
type Type string

const (
	TypeServerStatus     Type = "server_status"
	TypeListNginxConfigs Type = "list_nginx_configs"
	TypeNewSite          Type = "new_site"
	TypeInstallApp       Type = "install_app"
	TypeGetFrappeApp     Type = "get_frappe_app"
	TypeDeployFrappeApp  Type = "deploy_frappe_app"
	TypeBackupSite       Type = "backup_site"
	TypeSetupNginx       Type = "setup_nginx"
	TypeDeleteSite       Type = "delete_site"
	TypeIssueCertificate Type = "issue_certificate"
	TypeWriteProxyRoute  Type = "write_proxy_route"
	TypeDeleteProxyRoute Type = "delete_proxy_route"
	TypeDeployWebApp       Type = "deploy_web_app"
	TypeRollbackWebApp     Type = "rollback_web_app"
	TypeRollbackFrappeApp  Type = "rollback_frappe_app"
)

// Spec describes a job type's static properties: its default timeout, whether
// retries are allowed, and how its lock key is formed.
type Spec struct {
	Type        Type
	Timeout     time.Duration
	Retryable   bool // only read-only jobs + issue_certificate
	MaxAttempts int
}

// registry holds the Spec for every known job type.
var registry = map[Type]Spec{
	TypeServerStatus:     {TypeServerStatus, 1 * time.Minute, true, 3},
	TypeListNginxConfigs: {TypeListNginxConfigs, 1 * time.Minute, true, 3},
	TypeNewSite:          {TypeNewSite, 30 * time.Minute, false, 1},
	TypeInstallApp:       {TypeInstallApp, 20 * time.Minute, false, 1},
	TypeGetFrappeApp:     {TypeGetFrappeApp, 20 * time.Minute, false, 1},
	TypeDeployFrappeApp:  {TypeDeployFrappeApp, 45 * time.Minute, false, 1},
	TypeBackupSite:       {TypeBackupSite, 30 * time.Minute, false, 1},
	TypeSetupNginx:       {TypeSetupNginx, 5 * time.Minute, false, 1},
	TypeDeleteSite:       {TypeDeleteSite, 10 * time.Minute, false, 1},
	TypeIssueCertificate: {TypeIssueCertificate, 5 * time.Minute, true, 2},
	TypeWriteProxyRoute:  {TypeWriteProxyRoute, 2 * time.Minute, false, 1},
	TypeDeleteProxyRoute: {TypeDeleteProxyRoute, 2 * time.Minute, false, 1},
	TypeDeployWebApp:      {TypeDeployWebApp, 20 * time.Minute, false, 1},
	TypeRollbackWebApp:    {TypeRollbackWebApp, 5 * time.Minute, false, 1},
	TypeRollbackFrappeApp: {TypeRollbackFrappeApp, 45 * time.Minute, false, 1},
}

// Lookup returns the Spec for a job type.
func Lookup(t Type) (Spec, bool) {
	s, ok := registry[t]
	return s, ok
}

// Params is implemented by every job type's parameter struct. Validate is
// called by the panel before insert and by the agent before running.
type Params interface {
	Validate(roots []string) error
}

// newParams returns a zero-valued Params for a type, so the agent can unmarshal
// into the right concrete struct.
func newParams(t Type) (Params, error) {
	switch t {
	case TypeServerStatus:
		return &ServerStatusParams{}, nil
	case TypeListNginxConfigs:
		return &ListNginxConfigsParams{}, nil
	case TypeNewSite:
		return &NewSiteParams{}, nil
	case TypeInstallApp:
		return &InstallAppParams{}, nil
	case TypeSetupNginx:
		return &SetupNginxParams{}, nil
	case TypeDeleteSite:
		return &DeleteSiteParams{}, nil
	case TypeIssueCertificate:
		return &IssueCertificateParams{}, nil
	case TypeGetFrappeApp:
		return &GetFrappeAppParams{}, nil
	case TypeDeployFrappeApp:
		return &DeployFrappeAppParams{}, nil
	case TypeBackupSite:
		return &BackupSiteParams{}, nil
	case TypeWriteProxyRoute:
		return &WriteProxyRouteParams{}, nil
	case TypeDeleteProxyRoute:
		return &DeleteProxyRouteParams{}, nil
	case TypeDeployWebApp:
		return &DeployWebAppParams{}, nil
	case TypeRollbackWebApp:
		return &RollbackWebAppParams{}, nil
	case TypeRollbackFrappeApp:
		return &RollbackFrappeAppParams{}, nil
	default:
		return nil, fmt.Errorf("unknown or not-yet-implemented job type %q", t)
	}
}

// ParseParams unmarshals raw JSON into the concrete params for t and validates
// it against the allowlisted bench roots.
func ParseParams(t Type, raw json.RawMessage, roots []string) (Params, error) {
	p, err := newParams(t)
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, p); err != nil {
			return nil, fmt.Errorf("decode %s params: %w", t, err)
		}
	}
	if err := p.Validate(roots); err != nil {
		return nil, err
	}
	return p, nil
}
