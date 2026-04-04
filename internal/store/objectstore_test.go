package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func newObjectStoreForPathTests(t *testing.T) *ObjectTokenStore {
	t.Helper()
	root := t.TempDir()
	authDir := filepath.Join(root, "auths")
	return &ObjectTokenStore{spoolRoot: root, authDir: authDir}
}

func TestObjectTokenStoreResolveAuthPathRejectsOutsideManagedDir(t *testing.T) {
	store := newObjectStoreForPathTests(t)
	outside := filepath.Join(filepath.Dir(store.spoolRoot), "outside.json")

	_, err := store.resolveAuthPath(&cliproxyauth.Auth{
		ID:         "bad",
		Attributes: map[string]string{"path": outside},
	})
	if err == nil || !strings.Contains(err.Error(), "outside managed directory") {
		t.Fatalf("expected outside managed directory error, got %v", err)
	}
}

func TestObjectTokenStoreResolveAuthPathNormalizesRelativePath(t *testing.T) {
	store := newObjectStoreForPathTests(t)

	path, err := store.resolveAuthPath(&cliproxyauth.Auth{
		ID:         "ok",
		Attributes: map[string]string{"path": filepath.Join("team", "token.json")},
	})
	if err != nil {
		t.Fatalf("resolveAuthPath returned error: %v", err)
	}
	want := filepath.Join(store.authDir, "team", "token.json")
	if path != want {
		t.Fatalf("expected %s, got %s", want, path)
	}
}

func TestObjectTokenStoreSaveRejectsOutsideManagedDir(t *testing.T) {
	store := newObjectStoreForPathTests(t)
	outside := filepath.Join(filepath.Dir(store.spoolRoot), "secret.json")

	_, err := store.Save(context.Background(), &cliproxyauth.Auth{
		ID:         "bad",
		Metadata:   map[string]any{"type": "test"},
		Attributes: map[string]string{"path": outside},
	})
	if err == nil || !strings.Contains(err.Error(), "outside managed directory") {
		t.Fatalf("expected outside managed directory error, got %v", err)
	}
}

func TestObjectTokenStorePersistAuthFilesRejectsOutsideManagedDir(t *testing.T) {
	store := newObjectStoreForPathTests(t)
	outside := filepath.Join(filepath.Dir(store.spoolRoot), "secret.json")

	err := store.PersistAuthFiles(context.Background(), "", outside)
	if err == nil || !strings.Contains(err.Error(), "outside managed directory") {
		t.Fatalf("expected outside managed directory error, got %v", err)
	}
}

func TestObjectTokenStoreDeleteRejectsOutsideManagedDir(t *testing.T) {
	store := newObjectStoreForPathTests(t)
	outside := filepath.Join(filepath.Dir(store.spoolRoot), "secret.json")

	err := store.Delete(context.Background(), outside)
	if err == nil || !strings.Contains(err.Error(), "outside managed directory") {
		t.Fatalf("expected outside managed directory error, got %v", err)
	}
}
