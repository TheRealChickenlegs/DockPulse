package auth

import (
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	const password = "correct horse battery staple"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash does not start with $argon2id$: %q", hash)
	}

	ok, err := VerifyPassword(hash, password)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("expected correct password to verify")
	}

	ok, err = VerifyPassword(hash, "wrong")
	if err != nil {
		t.Fatalf("verify wrong: %v", err)
	}
	if ok {
		t.Fatal("expected wrong password to fail")
	}
}

func TestHashPasswordEmpty(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestVerifyPasswordEmpty(t *testing.T) {
	if ok, err := VerifyPassword("", ""); err != nil || ok {
		t.Fatalf("expected (false, nil) for empty inputs, got (%v, %v)", ok, err)
	}
	if ok, err := VerifyPassword("$argon2id$v=19$m=64,t=3,p=2$AAAA$BBBB", ""); err != nil || ok {
		t.Fatalf("expected (false, nil) for empty raw, got (%v, %v)", ok, err)
	}
}

func TestVerifyPasswordMalformed(t *testing.T) {
	cases := []string{
		"not-a-hash",
		"$argon2i$v=19$m=64,t=3,p=2$AAAA$BBBB",
		"$argon2id$v=1$m=64,t=3,p=2$AAAA$BBBB",
		"$argon2id$v=19$badparams$AAAA$BBBB",
		"$argon2id$v=19$m=64,t=3,p=2$!!!notbase64!!!$BBBB",
		"$argon2id$v=19$m=64,t=3,p=2$AAAA$!!!notbase64!!!",
	}
	for _, c := range cases {
		if _, err := VerifyPassword(c, "anything"); err == nil {
			t.Errorf("expected error for malformed hash %q", c)
		}
	}
}

func TestRandomToken(t *testing.T) {
	a := RandomToken(16)
	b := RandomToken(16)
	if a == b {
		t.Fatal("two consecutive RandomToken(16) calls produced the same value")
	}
	if len(a) != 32 {
		t.Fatalf("expected 32 hex chars (16 bytes), got %d", len(a))
	}
}

func TestRandomTokenDistinctLengths(t *testing.T) {
	if len(RandomToken(8)) != 16 {
		t.Fatal("8 bytes should encode to 16 hex chars")
	}
	if len(RandomToken(64)) != 128 {
		t.Fatal("64 bytes should encode to 128 hex chars")
	}
}
