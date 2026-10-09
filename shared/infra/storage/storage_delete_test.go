package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newDeleteTestStorage(t *testing.T) (*LocalStorage, string) {
	t.Helper()
	tempDir := t.TempDir()
	base := filepath.Join(tempDir, "storage")
	encKey := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	st, err := NewLocalStorage(base, encKey, "test")
	if err != nil {
		t.Fatalf("failed to create LocalStorage: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, base
}

// invalidKeys are refused by Size and Delete before any filesystem call.
var invalidKeys = []struct {
	name string
	in   string
}{
	{name: "dot-dot traversal", in: "../../etc/passwd"},
	{name: "sibling-prefix escape with UUID suffix", in: "../storage-evil/a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"},
	{name: "intermediate dot-dot", in: "a0eebc99/../evil"},
	{name: "absolute path", in: "/etc/passwd"},
	{name: "empty key", in: ""},
	{name: "dot", in: "."},
	{name: "filename", in: "document.pdf"},
	{name: "uppercase UUID", in: "A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11"},
	{name: "UUID with trailing slash", in: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11/"},
	{name: "UUID with null byte", in: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11\x00"},
	{name: "temp file name", in: ".upload-0123456789abcdef0123456789abcdef.tmp"},
}

func TestLocalStorage_Size(t *testing.T) {
	st, _ := newDeleteTestStorage(t)
	ctx := context.Background()

	for _, n := range []int{0, 1, 5, 4096, 1<<20 + 3} {
		objID := fmt.Sprintf("c1eebc99-9c0b-4ef8-bb6d-%012x", n)
		content := bytes.Repeat([]byte{'x'}, n)
		if err := st.Upload(ctx, objID, bytes.NewReader(content), "application/pdf"); err != nil {
			t.Fatalf("Upload(%d bytes) failed: %v", n, err)
		}
		size, err := st.Size(objID)
		if err != nil {
			t.Fatalf("Size(%d bytes) failed: %v", n, err)
		}
		if size != int64(n) {
			t.Fatalf("Size = %d, want plaintext size %d", size, n)
		}
		rc, err := st.OpenFile(objID)
		if err != nil {
			t.Fatalf("OpenFile failed: %v", err)
		}
		got, _ := io.ReadAll(rc)
		_ = rc.Close()
		if !bytes.Equal(got, content) {
			t.Fatalf("round trip of %d bytes differs", n)
		}
	}

	t.Run("missing key is ErrNotFound", func(t *testing.T) {
		_, err := st.Size("d1eebc99-9c0b-4ef8-bb6d-6bb9bd380a22")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Size(missing) error = %v, want ErrNotFound", err)
		}
		_, err = st.OpenFile("d1eebc99-9c0b-4ef8-bb6d-6bb9bd380a22")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("OpenFile(missing) error = %v, want ErrNotFound", err)
		}
	})

	for _, tc := range invalidKeys {
		t.Run("invalid key "+tc.name, func(t *testing.T) {
			if _, err := st.Size(tc.in); err == nil {
				t.Fatalf("Size(%q) = nil error, want refusal", tc.in)
			}
		})
	}
}

func TestLocalStorage_SizeTruncatedFile(t *testing.T) {
	st, base := newDeleteTestStorage(t)
	objID := "e1eebc99-9c0b-4ef8-bb6d-6bb9bd380a33"
	if err := os.WriteFile(filepath.Join(base, objID), []byte{1, 2, 3}, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := st.Size(objID); err == nil {
		t.Fatal("Size of a truncated file = nil error, want refusal")
	}
}

func TestLocalStorage_SizeRefusesDirectoryAndOutsideSymlink(t *testing.T) {
	st, base := newDeleteTestStorage(t)

	dirID := "f1eebc99-9c0b-4ef8-bb6d-6bb9bd380a44"
	if err := os.Mkdir(filepath.Join(base, dirID), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := st.Size(dirID); err == nil {
		t.Fatal("Size(directory) = nil error, want refusal")
	}

	outside := filepath.Join(filepath.Dir(base), "outside.bin")
	if err := os.WriteFile(outside, bytes.Repeat([]byte{'s'}, 500), 0600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	linkID := "f2eebc99-9c0b-4ef8-bb6d-6bb9bd380a55"
	if err := os.Symlink(outside, filepath.Join(base, linkID)); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := st.Size(linkID)
	if err == nil || !strings.Contains(err.Error(), "directory traversal detected") {
		t.Fatalf("Size(symlink outside root) error = %v, want traversal refusal", err)
	}
}

func TestLocalStorage_Delete(t *testing.T) {
	st, base := newDeleteTestStorage(t)
	ctx := context.Background()
	objID := "a2eebc99-9c0b-4ef8-bb6d-6bb9bd380a66"

	if err := st.Upload(ctx, objID, strings.NewReader("%PDF-1.7 body"), "application/pdf"); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	if err := st.Delete(ctx, objID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(base, objID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("object still on disk after Delete: %v", err)
	}
	if _, err := st.OpenFile(objID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("OpenFile after Delete error = %v, want ErrNotFound", err)
	}

	t.Run("idempotent on missing key", func(t *testing.T) {
		if err := st.Delete(ctx, objID); err != nil {
			t.Fatalf("second Delete = %v, want nil", err)
		}
		if err := st.Delete(ctx, "b2eebc99-9c0b-4ef8-bb6d-6bb9bd380a77"); err != nil {
			t.Fatalf("Delete(never stored) = %v, want nil", err)
		}
	})

	t.Run("key can be uploaded again after delete", func(t *testing.T) {
		if err := st.Upload(ctx, objID, strings.NewReader("again"), "application/pdf"); err != nil {
			t.Fatalf("re-Upload after Delete failed: %v", err)
		}
	})

	t.Run("cancelled context deletes nothing", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if err := st.Delete(cctx, objID); err == nil {
			t.Fatal("Delete with cancelled context = nil, want error")
		}
		if _, err := os.Lstat(filepath.Join(base, objID)); err != nil {
			t.Fatalf("object removed despite cancelled context: %v", err)
		}
	})

	for _, tc := range invalidKeys {
		t.Run("invalid key "+tc.name, func(t *testing.T) {
			if err := st.Delete(ctx, tc.in); err == nil {
				t.Fatalf("Delete(%q) = nil error, want refusal", tc.in)
			}
		})
	}
}

func TestLocalStorage_DeleteContainment(t *testing.T) {
	st, base := newDeleteTestStorage(t)
	ctx := context.Background()
	parent := filepath.Dir(base)

	// A file next to the root that a traversal would reach.
	victim := filepath.Join(parent, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep me"), 0600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	for _, objID := range []string{"../victim.txt", "../storage/../victim.txt", victim} {
		if err := st.Delete(ctx, objID); err == nil {
			t.Fatalf("Delete(%q) = nil error, want refusal", objID)
		}
	}
	if b, err := os.ReadFile(victim); err != nil || string(b) != "keep me" {
		t.Fatalf("victim outside root changed: %q, %v", b, err)
	}

	t.Run("symlink to outside removes the link only", func(t *testing.T) {
		linkID := "c2eebc99-9c0b-4ef8-bb6d-6bb9bd380a88"
		if err := os.Symlink(victim, filepath.Join(base, linkID)); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		if err := st.Delete(ctx, linkID); err != nil {
			t.Fatalf("Delete(symlink) = %v, want nil", err)
		}
		if _, err := os.Lstat(filepath.Join(base, linkID)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("symlink still present: %v", err)
		}
		if b, err := os.ReadFile(victim); err != nil || string(b) != "keep me" {
			t.Fatalf("symlink target outside root changed: %q, %v", b, err)
		}
	})

	t.Run("directory is refused", func(t *testing.T) {
		dirID := "d2eebc99-9c0b-4ef8-bb6d-6bb9bd380a99"
		dir := filepath.Join(base, dirID)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := st.Delete(ctx, dirID); err == nil {
			t.Fatal("Delete(directory) = nil, want refusal")
		}
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("directory removed: %v", err)
		}
	})

	t.Run("symlinked root parent component is refused", func(t *testing.T) {
		// A UUID key never has a path separator, so the only way out is the
		// final component; Upload/OpenFile already refuse it, Delete removes
		// only the entry itself (above). Nothing else to escape through.
		if err := st.Delete(ctx, "e2eebc99-9c0b-4ef8-bb6d-6bb9bd380aaa/x"); err == nil {
			t.Fatal("Delete with a path separator = nil, want refusal")
		}
	})
}
