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

	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><path fill="#123" d="M0 0h10v10H0z"/></svg>`)
	svgLogo := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(svg)
	normalized, err = NormalizeResellerLogo(svgLogo)
	require.NoError(t, err)
	assert.Equal(t, svgLogo, normalized)

	_, err = NormalizeResellerLogo("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<html/>`)))
	assert.ErrorIs(t, err, ErrInvalidResellerLogo)
	_, err = NormalizeResellerLogo("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg>`)))
	assert.ErrorIs(t, err, ErrInvalidResellerLogo)
}

func TestNormalizeResellerFavicon(t *testing.T) {
	ico := []byte{0, 0, 1, 0, 1, 0, 16, 16, 0, 0, 1, 0, 32, 0}
	favicon := "data:image/x-icon;base64," + base64.StdEncoding.EncodeToString(ico)
	normalized, err := NormalizeResellerFavicon(favicon)
	require.NoError(t, err)
	assert.Equal(t, favicon, normalized)

	_, err = NormalizeResellerFavicon("data:image/x-icon;base64," + base64.StdEncoding.EncodeToString([]byte("not an icon")))
	assert.ErrorIs(t, err, ErrInvalidResellerFavicon)

	_, err = NormalizeResellerFavicon("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>")))
	assert.ErrorIs(t, err, ErrInvalidResellerFavicon)
}
