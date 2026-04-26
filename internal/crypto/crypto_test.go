package crypto

import (
	"bytes"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	key := bytes.Repeat([]byte{0x42}, 32)
	c, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	c := newTestCipher(t)
	plaintext := []byte("super secret SMTP password")
	ct, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if ct == "" {
		t.Fatal("empty ciphertext")
	}
	got, err := c.Decrypt(ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("got %q want %q", got, plaintext)
	}
}

func TestSignVerifyRoundtrip(t *testing.T) {
	c := newTestCipher(t)
	signed := c.Sign([]byte("session=abc123"))
	got, err := c.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "session=abc123" {
		t.Fatalf("got %q", got)
	}
}

func TestVerifyTampered(t *testing.T) {
	c := newTestCipher(t)
	signed := c.Sign([]byte("payload"))
	tampered := signed[:len(signed)-2] + "AA"
	if _, err := c.Verify(tampered); err == nil {
		t.Fatal("expected verify to fail on tampered signature")
	}
}
