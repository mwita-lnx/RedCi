package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/mwita-lnx/RedCi/ops/internal/secrets"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// Lockout policy from the spec: 10 failures per key within 15 minutes blocks
// further attempts for 15 minutes.
const (
	lockoutWindow    = 15 * time.Minute
	lockoutThreshold = 10
)

var (
	// ErrInvalidCredentials is returned for a wrong email or password.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrLockedOut is returned when too many recent failures have accumulated.
	ErrLockedOut = errors.New("auth: too many attempts, try again later")
	// ErrUserDisabled is returned for a disabled account.
	ErrUserDisabled = errors.New("auth: account disabled")
)

// Service performs password and TOTP checks and records failures for lockout.
type Service struct {
	db     *store.DB
	sealer *secrets.Sealer
}

// NewService builds an auth Service.
func NewService(db *store.DB, sealer *secrets.Sealer) *Service {
	return &Service{db: db, sealer: sealer}
}

// locked reports whether key has hit the lockout threshold in the window.
func (s *Service) locked(ctx context.Context, key string) (bool, error) {
	since := time.Now().Add(-lockoutWindow).Unix()
	n, err := s.db.ReadQ.CountLoginAttempts(ctx, store.CountLoginAttemptsParams{
		Key: strings.ToLower(key),
		Ts:  since,
	})
	if err != nil {
		return false, err
	}
	return n >= lockoutThreshold, nil
}

// recordFailure inserts a failed-attempt row for both the email and the IP.
func (s *Service) recordFailure(ctx context.Context, email, ip string) {
	_ = s.db.WriteQ.InsertLoginAttempt(ctx, strings.ToLower(email))
	if ip != "" {
		_ = s.db.WriteQ.InsertLoginAttempt(ctx, ip)
	}
}

// Authenticate checks email+password and the lockout counters. It does NOT
// check TOTP; the handler performs the TOTP step after the password step.
// email and ip are both lockout keys.
func (s *Service) Authenticate(ctx context.Context, email, password, ip string) (store.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, key := range []string{email, ip} {
		if key == "" {
			continue
		}
		locked, err := s.locked(ctx, key)
		if err != nil {
			return store.User{}, err
		}
		if locked {
			return store.User{}, ErrLockedOut
		}
	}

	user, err := s.db.ReadQ.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		s.recordFailure(ctx, email, ip)
		return store.User{}, ErrInvalidCredentials
	}
	if err != nil {
		return store.User{}, err
	}
	if user.Disabled != 0 {
		return store.User{}, ErrUserDisabled
	}

	ok, err := VerifyPassword(password, user.PasswordHash)
	if err != nil || !ok {
		s.recordFailure(ctx, email, ip)
		return store.User{}, ErrInvalidCredentials
	}

	// Success: clear the counters for this email and IP.
	_ = s.db.WriteQ.ClearLoginAttempts(ctx, email)
	if ip != "" {
		_ = s.db.WriteQ.ClearLoginAttempts(ctx, ip)
	}
	return user, nil
}

// HasTOTP reports whether the user has completed 2FA enrollment.
func (s *Service) HasTOTP(user store.User) bool {
	return len(user.TotpSecretEnc) > 0
}

// DecryptTOTPSecret opens the stored TOTP secret for a user.
func (s *Service) DecryptTOTPSecret(user store.User) (string, error) {
	plain, err := s.sealer.Open(user.TotpSecretEnc)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// VerifyTOTPFor validates code against the user's stored secret.
func (s *Service) VerifyTOTPFor(ctx context.Context, user store.User, code string) (bool, error) {
	secret, err := s.DecryptTOTPSecret(user)
	if err != nil {
		return false, err
	}
	return ValidateTOTP(code, secret), nil
}

// EnrollTOTP seals and stores a new TOTP secret for the user, after confirming
// the user can produce a valid code from it.
func (s *Service) EnrollTOTP(ctx context.Context, userID int64, secret, code string) error {
	if !ValidateTOTP(code, secret) {
		return ErrInvalidCredentials
	}
	enc, err := s.sealer.Seal([]byte(secret))
	if err != nil {
		return err
	}
	return s.db.WriteQ.SetUserTOTP(ctx, store.SetUserTOTPParams{
		TotpSecretEnc: enc,
		ID:            userID,
	})
}

// TouchLogin records a successful login time.
func (s *Service) TouchLogin(ctx context.Context, userID int64) error {
	return s.db.WriteQ.TouchUserLogin(ctx, userID)
}

// CreateUser hashes the password and inserts a new user. Used by the CLI.
func (s *Service) CreateUser(ctx context.Context, email, name, password string, role Role) (store.User, error) {
	if !role.Valid() {
		return store.User{}, errors.New("auth: invalid role")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return store.User{}, err
	}
	return s.db.WriteQ.CreateUser(ctx, store.CreateUserParams{
		Email:        strings.ToLower(strings.TrimSpace(email)),
		Name:         name,
		PasswordHash: hash,
		Role:         string(role),
	})
}
