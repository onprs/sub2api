package repository

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestTicketAttachmentLocalStoreRoundTripAndPermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "attachments")
	store, err := newTicketAttachmentLocalStore(&config.Config{Ticketing: config.TicketingConfig{LocalStorageRoot: root}})
	require.NoError(t, err)

	key := "2026/07/object-id"
	require.NoError(t, store.Put(context.Background(), key, []byte("evidence"), "text/plain"))
	body, err := store.Open(context.Background(), key)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.Equal(t, []byte("evidence"), data)

	if runtime.GOOS != "windows" {
		rootInfo, err := os.Stat(root)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), rootInfo.Mode().Perm())
		fileInfo, err := os.Stat(filepath.Join(root, filepath.FromSlash(key)))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
	}
	require.NoError(t, store.Delete(context.Background(), key))
	require.NoError(t, store.Delete(context.Background(), key), "missing objects are idempotent")
}

func TestTicketAttachmentLocalStoreRejectsPathTraversalAndExistingObject(t *testing.T) {
	store, err := newTicketAttachmentLocalStore(&config.Config{Ticketing: config.TicketingConfig{LocalStorageRoot: t.TempDir()}})
	require.NoError(t, err)

	for _, key := range []string{"../outside", "/absolute", `..\\outside`, "a/../outside", "."} {
		t.Run(key, func(t *testing.T) {
			require.Error(t, store.Put(context.Background(), key, []byte("x"), "text/plain"))
		})
	}
	require.NoError(t, store.Put(context.Background(), "safe/object", []byte("first"), "text/plain"))
	require.Error(t, store.Put(context.Background(), "safe/object", []byte("second"), "text/plain"))
}

func TestTicketAttachmentLocalStoreRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on many Windows environments")
	}
	root := t.TempDir()
	outside := t.TempDir()
	store, err := newTicketAttachmentLocalStore(&config.Config{Ticketing: config.TicketingConfig{LocalStorageRoot: root}})
	require.NoError(t, err)
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape")))
	require.Error(t, store.Put(context.Background(), "escape/object", []byte("x"), "text/plain"))
}

func TestTicketAttachmentRegistryDoesNotBlockStartupWhenLocalRootIsUnavailable(t *testing.T) {
	rootFile := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(rootFile, []byte("occupied"), 0o600))
	registry, err := NewTicketAttachmentStoreRegistry(
		&config.Config{Ticketing: config.TicketingConfig{LocalStorageRoot: rootFile}},
		NewTicketAttachmentS3StoreFactory(),
	)
	require.NoError(t, err)
	require.Nil(t, registry.LocalStore())
}

func TestTicketAttachmentLocalStoreProbeLeavesNoObject(t *testing.T) {
	root := t.TempDir()
	store, err := newTicketAttachmentLocalStore(&config.Config{Ticketing: config.TicketingConfig{LocalStorageRoot: root}})
	require.NoError(t, err)
	require.NoError(t, store.Probe(context.Background()))
	entries, err := os.ReadDir(filepath.Join(root, ".probe"))
	require.NoError(t, err)
	require.Empty(t, entries)
}
