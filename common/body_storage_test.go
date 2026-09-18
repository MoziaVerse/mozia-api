package common

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBodyStorageReplayReaders(t *testing.T) {
	for _, disk := range []bool{false, true} {
		name := "memory"
		if disk {
			name = "disk"
		}
		t.Run(name, func(t *testing.T) {
			originalConfig := GetDiskCacheConfig()
			SetDiskCacheConfig(DiskCacheConfig{Enabled: disk, ThresholdMB: 0, MaxSizeMB: 1, Path: t.TempDir()})
			t.Cleanup(func() { SetDiskCacheConfig(originalConfig) })
			payload := []byte("independent request body readers")
			storage, err := CreateBodyStorage(payload)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, storage.Close()) })
			require.Equal(t, disk, storage.IsDisk())

			body := ReaderOnly(storage)
			replayable, ok := body.(interface{ GetBody() (io.ReadCloser, error) })
			require.True(t, ok)
			prefix := make([]byte, 3)
			_, err = io.ReadFull(body, prefix)
			require.NoError(t, err)
			assert.Equal(t, payload[:3], prefix)

			first, err := replayable.GetBody()
			require.NoError(t, err)
			t.Cleanup(func() { first.Close() })
			_, err = io.ReadFull(first, prefix)
			require.NoError(t, err)
			assert.Equal(t, payload[:3], prefix)

			second, err := replayable.GetBody()
			require.NoError(t, err)
			t.Cleanup(func() { second.Close() })
			data, err := io.ReadAll(second)
			require.NoError(t, err)
			assert.Equal(t, payload, data)
			require.NoError(t, second.Close())

			data, err = io.ReadAll(first)
			require.NoError(t, err)
			assert.Equal(t, payload[3:], data)
			require.NoError(t, first.Close())
			require.NoError(t, io.NopCloser(body).Close())
			data, err = io.ReadAll(body)
			require.NoError(t, err)
			assert.Equal(t, payload[3:], data)

			// Only the owner releases storage; closing an attempt must not do so.
			require.NoError(t, storage.Close())
			_, err = replayable.GetBody()
			require.ErrorIs(t, err, ErrStorageClosed)
			if disk {
				assert.NoFileExists(t, storage.(*diskStorage).filePath)
			}
		})
	}
}
