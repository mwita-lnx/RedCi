package main

import "testing"

// TestIssueRejectsHostileInput ensures validation fails before certbot runs.
// These all return an error from validation (not from exec), because the bad
// argument is caught first.
func TestIssueRejectsHostileInput(t *testing.T) {
	cases := [][]string{
		{},                                  // missing args
		{"a.com; rm -rf /", "ops@x.com"},    // injection in domain
		{"good.com", "notanemail"},          // bad email
		{"good.com", "ops@x.com", "--evil"}, // unexpected flag
	}
	for _, c := range cases {
		if err := issue(c); err == nil {
			t.Errorf("issue(%v) = nil, want error", c)
		}
	}
}

func TestRunUnknownCommand(t *testing.T) {
	if err := run([]string{"delete", "everything"}); err == nil {
		t.Error("unknown command should error")
	}
}
