package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

func TestVerifyRejectsExpiredMissingExpirationAndWrongAlgorithm(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := Issue(private, Identity{Subject: "Alice", Role: "user"})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := Verify(public, valid)
	if err != nil || identity.Subject != "Alice" {
		t.Fatalf("valid token: %v, %+v", err, identity)
	}

	for name, mutate := range map[string]func() (string, error){
		"expired": func() (string, error) {
			now := time.Now().UTC()
			tok, err := jwt.NewBuilder().Issuer(Issuer).Audience([]string{Audience}).
				Subject("Alice").IssuedAt(now.Add(-2*time.Hour)).
				Expiration(now.Add(-time.Hour)).Claim("role", "user").Build()
			if err != nil {
				return "", err
			}
			signed, err := jwt.Sign(tok, jwt.WithKey(jwa.EdDSAEd25519(), private))
			return string(signed), err
		},
		"missing-exp": func() (string, error) {
			tok, err := jwt.NewBuilder().Issuer(Issuer).Audience([]string{Audience}).
				Subject("Alice").Claim("role", "user").Build()
			if err != nil {
				return "", err
			}
			signed, err := jwt.Sign(tok, jwt.WithKey(jwa.EdDSAEd25519(), private))
			return string(signed), err
		},
		"wrong-algorithm": func() (string, error) {
			tok, err := jwt.NewBuilder().Issuer(Issuer).Audience([]string{Audience}).
				Subject("Alice").Expiration(time.Now().Add(time.Hour)).
				Claim("role", "user").Build()
			if err != nil {
				return "", err
			}
			signed, err := jwt.Sign(tok, jwt.WithKey(jwa.HS256(), []byte("0123456789abcdef0123456789abcdef")))
			return string(signed), err
		},
		"tampered": func() (string, error) {
			parts := strings.Split(valid, ".")
			if parts[1][0] == 'A' {
				parts[1] = "B" + parts[1][1:]
			} else {
				parts[1] = "A" + parts[1][1:]
			}
			return strings.Join(parts, "."), nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := mutate()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Verify(public, raw); err == nil {
				t.Fatal("token was accepted")
			}
		})
	}
}
