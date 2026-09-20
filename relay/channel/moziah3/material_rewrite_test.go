package moziah3

import (
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 平台素材存储的公网 URL 在提交给自托管 H3 时必须换成集群内门；其它 URL 原样透传。
func TestBuildRequestRewritesPlatformMaterialURLsToInternalEndpoint(t *testing.T) {
	service.SetMaterialObjectStorage(&service.MaterialObjectStorage{
		Endpoint:         "http://117.161.30.178:30333",
		InternalEndpoint: "http://172.16.10.151:30333",
		Bucket:           "h3-inputs",
		AccessKeyID:      "ak",
		AccessKeySecret:  "sk",
	})
	t.Cleanup(func() { service.SetMaterialObjectStorage(nil) })

	body := `{
		"model":"minimax/minimax-h3-ref2va",
		"prompt":"walk <Picture 1>",
		"duration":5,
		"size":"1344x768",
		"content":[
			{"type":"image_url","role":"reference_image","image_url":{"url":"http://117.161.30.178:30333/h3-inputs/materials/2026/09/20/abc.png?X-Amz-Expires=604800&X-Amz-Signature=old"}},
			{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/other.png"}}
		]
	}`
	request, _, _ := buildUpstreamRequest(t, body, "minimax/minimax-h3-ref2va")
	require.Len(t, request.Conditions, 2)

	internal, err := url.Parse(request.Conditions[0].URI)
	require.NoError(t, err)
	assert.Equal(t, "172.16.10.151:30333", internal.Host)
	assert.Equal(t, "/h3-inputs/materials/2026/09/20/abc.png", internal.Path)
	assert.NotEqual(t, "old", internal.Query().Get("X-Amz-Signature"))
	assert.Equal(t, "86400", internal.Query().Get("X-Amz-Expires"))

	assert.Equal(t, "https://example.com/other.png", request.Conditions[1].URI)
}

func TestBuildRequestLeavesURLsUntouchedWithoutMaterialStorage(t *testing.T) {
	service.SetMaterialObjectStorage(nil)
	body := `{
		"model":"minimax/minimax-h3-ref2va",
		"prompt":"walk <Picture 1>",
		"duration":5,
		"size":"1344x768",
		"content":[{"type":"image_url","role":"reference_image","image_url":{"url":"http://117.161.30.178:30333/h3-inputs/materials/x.png?sig=1"}}]
	}`
	request, _, _ := buildUpstreamRequest(t, body, "minimax/minimax-h3-ref2va")
	require.Len(t, request.Conditions, 1)
	assert.Equal(t, "http://117.161.30.178:30333/h3-inputs/materials/x.png?sig=1", request.Conditions[0].URI)
}
