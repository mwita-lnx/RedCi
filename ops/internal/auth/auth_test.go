package auth

import "testing"

func TestPasswordHashVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword("correct horse battery staple", h)
	if err != nil || !ok {
		t.Fatalf("expected match, ok=%v err=%v", ok, err)
	}
	bad, err := VerifyPassword("wrong", h)
	if err != nil || bad {
		t.Fatalf("expected mismatch, ok=%v err=%v", bad, err)
	}
}

func TestVerifyPasswordBadHash(t *testing.T) {
	if _, err := VerifyPassword("x", "not-a-hash"); err != ErrBadHash {
		t.Fatalf("want ErrBadHash, got %v", err)
	}
}

func TestRoleAtLeast(t *testing.T) {
	if !RoleAdmin.AtLeast(RoleViewer) {
		t.Fatal("admin should satisfy viewer")
	}
	if RoleViewer.AtLeast(RoleDeployer) {
		t.Fatal("viewer should not satisfy deployer")
	}
	if !RoleDeployer.AtLeast(RoleDeployer) {
		t.Fatal("deployer should satisfy deployer")
	}
}

func TestTOTPRoundTrip(t *testing.T) {
	key, err := GenerateTOTP("admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if key.Secret() == "" {
		t.Fatal("empty secret")
	}
	if _, err := QRCodePNG(key, 200); err != nil {
		t.Fatalf("qr: %v", err)
	}
}
