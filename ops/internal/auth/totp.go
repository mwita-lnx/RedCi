package auth

import (
	"bytes"
	"image/png"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// TOTPIssuer is the label shown in authenticator apps.
const TOTPIssuer = "RedCi Ops Panel"

// GenerateTOTP creates a new TOTP secret for the given account (the email).
// It returns the otpauth key; the raw secret is key.Secret().
func GenerateTOTP(account string) (*otp.Key, error) {
	return totp.Generate(totp.GenerateOpts{
		Issuer:      TOTPIssuer,
		AccountName: account,
	})
}

// TOTPFromSecret rebuilds an otp.Key from a stored base32 secret, so a QR
// code can be re-rendered during enrollment.
func TOTPFromSecret(account, secret string) (*otp.Key, error) {
	return otp.NewKeyFromURL(
		"otpauth://totp/" + TOTPIssuer + ":" + account +
			"?issuer=" + TOTPIssuer + "&secret=" + secret + "&algorithm=SHA1&digits=6&period=30",
	)
}

// ValidateTOTP reports whether code is valid for secret right now.
func ValidateTOTP(code, secret string) bool {
	return totp.Validate(code, secret)
}

// QRCodePNG renders the key's otpauth URL as a PNG QR code.
func QRCodePNG(key *otp.Key, size int) ([]byte, error) {
	img, err := key.Image(size, size)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
