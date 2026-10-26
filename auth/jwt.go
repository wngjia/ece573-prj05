package auth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

const (
	Issuer        = "ece573-login"
	Audience      = "ece573-marketplace"
	TokenLifetime = 30 * time.Minute
)

type Identity struct {
	Subject string
	Role    string
}

func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	private, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not Ed25519")
	}
	return private, nil
}

func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid public key PEM")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	public, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key is not Ed25519")
	}
	return public, nil
}

func Issue(private ed25519.PrivateKey, identity Identity) (string, error) {
	now := time.Now().UTC()
	token, err := jwt.NewBuilder().
		Issuer(Issuer).
		Audience([]string{Audience}).
		Subject(identity.Subject).
		IssuedAt(now).
		Expiration(now.Add(TokenLifetime)).
		Claim("role", identity.Role).
		Build()
	if err != nil {
		return "", err
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.EdDSAEd25519(), private))
	if err != nil {
		return "", err
	}
	return string(signed), nil
}

func Verify(public ed25519.PublicKey, raw string) (Identity, error) {
	token, err := jwt.Parse([]byte(raw), jwt.WithKey(jwa.EdDSAEd25519(), public))
	if err != nil {
		return Identity{}, err
	}
	role, err := jwt.Get[string](token, "role")
	if err != nil {
		return Identity{}, err
	}
	subject, subjectOK := token.Subject()
	issuer, issuerOK := token.Issuer()
	audience, audienceOK := token.Audience()
	expiration, expirationOK := token.Expiration()
	if !subjectOK || subject == "" || !issuerOK || issuer != Issuer ||
		!audienceOK || !slices.Contains(audience, Audience) ||
		!expirationOK || !time.Now().Before(expiration) ||
		(role != "user" && role != "admin") {
		return Identity{}, errors.New("invalid token claims")
	}
	return Identity{Subject: subject, Role: role}, nil
}

func Bearer(header string) (string, error) {
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(value) == "" || strings.Contains(value, " ") {
		return "", errors.New("missing bearer token")
	}
	return value, nil
}
