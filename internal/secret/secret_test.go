package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptedFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	in := map[string]string{"GROQ": "gsk_abc123", "OR": "sk-or-xyz789"}
	if err := SealEncryptedFile(path, "correct horse battery", in); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %o, want 600", info.Mode().Perm())
	}

	got, err := OpenEncryptedFile(path, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if got["GROQ"] != "gsk_abc123" || got["OR"] != "sk-or-xyz789" {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	if _, err := OpenEncryptedFile(path, "wrong passphrase"); err == nil {
		t.Error("decrypt with wrong passphrase should fail")
	}
}

func TestStoreResolveSchemes(t *testing.T) {
	t.Setenv("FREEBIE_TEST_KEY", "from-env")
	s := New()

	if v, err := s.Resolve("env:FREEBIE_TEST_KEY"); err != nil || v != "from-env" {
		t.Errorf("env: got %q, %v", v, err)
	}
	if v, err := s.Resolve("FREEBIE_TEST_KEY"); err != nil || v != "from-env" {
		t.Errorf("bare env: got %q, %v", v, err)
	}
	if v, err := s.Resolve("literal:hunter2"); err != nil || v != "hunter2" {
		t.Errorf("literal: got %q, %v", v, err)
	}
	if v, err := s.Resolve(""); err != nil || v != "" {
		t.Errorf("empty: got %q, %v", v, err)
	}
	if _, err := s.Resolve("env:FREEBIE_NOT_SET_XYZ"); err == nil {
		t.Error("missing env var should error")
	}

	fpath := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(fpath, []byte("  file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Resolve("file:" + fpath); err != nil || v != "file-secret" {
		t.Errorf("file: got %q, %v", v, err)
	}
}

func TestStoreResolveEnc(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := SealEncryptedFile(path, "pp", map[string]string{"K": "enc-val"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREEBIE_SECRETS_FILE", path)
	t.Setenv("FREEBIE_SECRETS_PASSPHRASE", "pp")
	s := New()

	if v, err := s.Resolve("enc:K"); err != nil || v != "enc-val" {
		t.Errorf("enc: got %q, %v", v, err)
	}
	if _, err := s.Resolve("enc:missing"); err == nil {
		t.Error("missing enc key should error")
	}
}
