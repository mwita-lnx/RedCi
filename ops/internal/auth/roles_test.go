package auth

import "testing"

func TestRoleMax(t *testing.T) {
	cases := []struct{ a, b, want Role }{
		{RoleViewer, RoleDeployer, RoleDeployer},
		{RoleDeployer, RoleViewer, RoleDeployer},
		{RoleAdmin, RoleDeployer, RoleAdmin},
		{RoleViewer, RoleViewer, RoleViewer},
	}
	for _, c := range cases {
		if got := c.a.Max(c.b); got != c.want {
			t.Errorf("%s.Max(%s) = %s, want %s", c.a, c.b, got, c.want)
		}
	}
}
