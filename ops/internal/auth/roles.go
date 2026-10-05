package auth

// Role is one of the three panel roles from the spec.
type Role string

const (
	RoleViewer   Role = "viewer"
	RoleDeployer Role = "deployer"
	RoleAdmin    Role = "admin"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	switch r {
	case RoleViewer, RoleDeployer, RoleAdmin:
		return true
	}
	return false
}

// rank orders roles so a higher role satisfies a lower requirement.
func (r Role) rank() int {
	switch r {
	case RoleAdmin:
		return 3
	case RoleDeployer:
		return 2
	case RoleViewer:
		return 1
	}
	return 0
}

// AtLeast reports whether r meets or exceeds the required role.
func (r Role) AtLeast(required Role) bool {
	return r.rank() >= required.rank()
}
