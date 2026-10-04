package auth

import (
	"bytes"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword("correct horse battery staple", h)
	if err != nil || !ok {
		t.Fatalf("correct password rejected: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong password", h)
	if err != nil || ok {
		t.Fatalf("wrong password accepted: ok=%v err=%v", ok, err)
	}
}

func TestHashesAreSalted(t *testing.T) {
	a, _ := HashPassword("same-password")
	b, _ := HashPassword("same-password")
	if a == b {
		t.Fatal("two hashes of the same password are identical")
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	for _, bad := range []string{
		"",
		"plain-text",
		"$bcrypt$v=19$m=1,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=65536$c2FsdA$a2V5",
		"$argon2id$v=19$m=1,t=1,p=1$$",
	} {
		if ok, err := VerifyPassword("x", bad); ok || err == nil {
			t.Errorf("hash %q: ok=%v err=%v, want rejection", bad, ok, err)
		}
	}
}

func TestTokens(t *testing.T) {
	token, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 32 || !bytes.Equal(hash, HashToken(token)) {
		t.Fatal("token hash mismatch")
	}
	other, _, _ := NewToken()
	if token == other {
		t.Fatal("tokens are not random")
	}
}
