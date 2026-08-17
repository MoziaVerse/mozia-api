package model

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeResellerLogo(t *testing.T) {
	logo := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	normalized, err := NormalizeResellerLogo(logo)
	require.NoError(t, err)
	assert.Equal(t, logo, normalized)

	_, err = NormalizeResellerLogo("data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not an image")))
	assert.ErrorIs(t, err, ErrInvalidResellerLogo)

	_, err = NormalizeResellerLogo("data:image/png;base64," + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x89}, resellerLogoMaxBytes+1)))
	assert.ErrorIs(t, err, ErrInvalidResellerLogo)
}
