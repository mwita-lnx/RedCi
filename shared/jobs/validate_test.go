package jobs

import (
	"strings"
	"testing"
)

var benchRoots = []string{"/home/frappe/frappe-bench"}

func TestValidateDomain(t *testing.T) {
	good := []string{"example.com", "client1.example.com", "a.co", "sub.sub.example.org"}
	for _, d := range good {
		if err := ValidateDomain(d); err != nil {
			t.Errorf("ValidateDomain(%q) = %v, want nil", d, err)
		}
	}
	bad := []string{
		"", "nodot", "a.com; rm -rf /", "--help", "../../etc",
		"Example.com", "a..b.com", "-lead.com", "trail-.com",
		"a b.com", "a.com\n", strings.Repeat("a", 300) + ".com",
	}
	for _, d := range bad {
		if err := ValidateDomain(d); err == nil {
			t.Errorf("ValidateDomain(%q) = nil, want error", d)
		}
	}
}

func TestValidateAppName(t *testing.T) {
	for _, a := range []string{"erpnext", "hrms", "my_app", "a"} {
		if err := ValidateAppName(a); err != nil {
			t.Errorf("app %q should be valid: %v", a, err)
		}
	}
	for _, a := range []string{"", "1app", "App", "my-app", "a; ls", "../x", "a.b"} {
		if err := ValidateAppName(a); err == nil {
			t.Errorf("app %q should be invalid", a)
		}
	}
}

func TestValidateCommit(t *testing.T) {
	if err := ValidateCommit("0123456789abcdef0123456789abcdef01234567"); err != nil {
		t.Errorf("valid commit rejected: %v", err)
	}
	for _, c := range []string{"", "abc", "0123456789ABCDEF0123456789abcdef01234567", "g123456789abcdef0123456789abcdef01234567"} {
		if err := ValidateCommit(c); err == nil {
			t.Errorf("commit %q should be invalid", c)
		}
	}
}

func TestValidatePathUnder(t *testing.T) {
	ok := []string{"/home/frappe/frappe-bench", "/home/frappe/frappe-bench/sites"}
	for _, p := range ok {
		if err := ValidatePathUnder(p, benchRoots); err != nil {
			t.Errorf("path %q should be under root: %v", p, err)
		}
	}
	bad := []string{
		"", "relative/path", "/etc/passwd", "/home/frappe/frappe-bench/../../../etc",
		"/home/frappe/frappe-bench-evil", // prefix but not a child
	}
	for _, p := range bad {
		if err := ValidatePathUnder(p, benchRoots); err == nil {
			t.Errorf("path %q should be rejected", p)
		}
	}
}

func TestValidateUpstream(t *testing.T) {
	if err := ValidateUpstream("http://127.0.0.1:3001", false); err != nil {
		t.Errorf("valid upstream rejected: %v", err)
	}
	for _, u := range []string{"http://evil.com", "http://127.0.0.1", "https://127.0.0.1:3001", "http://10.0.0.1:3001"} {
		if err := ValidateUpstream(u, false); err == nil {
			t.Errorf("upstream %q should be rejected without override", u)
		}
	}
	// Admin override allows anything non-empty.
	if err := ValidateUpstream("http://10.0.0.5:8080", true); err != nil {
		t.Errorf("admin override should allow custom upstream: %v", err)
	}
}

func TestNewSiteParamsValidate(t *testing.T) {
	good := NewSiteParams{
		BenchPath: "/home/frappe/frappe-bench", Domain: "client1.example.com",
		Apps: []string{"erpnext", "hrms"}, AdminPassword: "secret:admin:1", DBRootPassword: "secret:db_root:1",
	}
	if err := good.Validate(benchRoots); err != nil {
		t.Fatalf("good params rejected: %v", err)
	}
	bad := good
	bad.Apps = nil
	if err := bad.Validate(benchRoots); err == nil {
		t.Fatal("empty apps should be rejected")
	}
	bad = good
	bad.Domain = "a.com; rm -rf /"
	if err := bad.Validate(benchRoots); err == nil {
		t.Fatal("injection domain should be rejected")
	}
}

// FuzzValidateDomain ensures no input panics and anything accepted re-validates.
func FuzzValidateDomain(f *testing.F) {
	for _, s := range []string{"example.com", "a.com; rm -rf /", "", "../../etc", "日本.com"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if err := ValidateDomain(s); err == nil {
			// Accepted values must satisfy the regex deterministically.
			if !domainRE.MatchString(s) {
				t.Fatalf("accepted %q that does not match domainRE", s)
			}
		}
	})
}

// FuzzValidatePathUnder must never accept a path containing "..".
func FuzzValidatePathUnder(f *testing.F) {
	for _, s := range []string{"/home/frappe/frappe-bench", "/etc", "../x", "/home/frappe/frappe-bench/../.."} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if err := ValidatePathUnder(s, benchRoots); err == nil {
			if strings.Contains(s, "..") {
				t.Fatalf("accepted traversal path %q", s)
			}
		}
	})
}
