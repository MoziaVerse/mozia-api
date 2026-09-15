package moziah3

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/taskbilling"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVDNMultipartGenerationModes(t *testing.T) {
	service.InitHttpClient()
	var imageBuffer bytes.Buffer
	require.NoError(t, png.Encode(&imageBuffer, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	imageBytes := imageBuffer.Bytes()
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)

	for _, frames := range [][]string{nil, {"first_frame"}, {"first_frame", "last_frame"}, {"last_frame"}} {
		for _, format := range []string{"json", "multipart"} {
			t.Run(format+"/"+strings.Join(frames, "+"), func(t *testing.T) {
				var input bytes.Buffer
				contentType := "application/json"
				if format == "json" {
					content := []map[string]any{{"type": "text", "text": "animate"}}
					for _, frame := range frames {
						content = append(content, map[string]any{"type": "image_url", "role": frame, "image_url": map[string]string{"url": dataURL}})
					}
					body, err := common.Marshal(map[string]any{"model": "my-video", "content": content, "seed": 0})
					require.NoError(t, err)
					input.Write(body)
				} else {
					writer := multipart.NewWriter(&input)
					require.NoError(t, writer.WriteField("model", "my-video"))
					require.NoError(t, writer.WriteField("prompt", "animate"))
					require.NoError(t, writer.WriteField("seed", "0"))
					for _, frame := range frames {
						part, err := writer.CreateFormFile(frame, "frame.png")
						require.NoError(t, err)
						_, err = part.Write(imageBytes)
						require.NoError(t, err)
					}
					require.NoError(t, writer.Close())
					contentType = writer.FormDataContentType()
				}
				ctx, cleanup := requestContext(t, input.String())
				t.Cleanup(cleanup)
				ctx.Request.Header.Set("Content-Type", contentType)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
					if r.Method == http.MethodGet {
						assert.Equal(t, "/v1/videos/upstream-id", r.URL.Path)
						_, _ = io.WriteString(w, `{"id":"upstream-id","status":"completed"}`)
						return
					}
					assert.Equal(t, "/v1/videos", r.URL.Path)
					assert.Positive(t, r.ContentLength)
					if !assert.NoError(t, r.ParseMultipartForm(1<<20)) {
						http.Error(w, "bad multipart", http.StatusBadRequest)
						return
					}
					defer r.MultipartForm.RemoveAll()
					assert.Equal(t, map[string][]string{
						"model": {"arbitrary-upstream-name"}, "prompt": {"animate"}, "seed": {"0"},
					}, r.MultipartForm.Value)
					assert.Len(t, r.MultipartForm.File, len(frames))
					for _, frame := range frames {
						file, header, err := r.FormFile(frame)
						if !assert.NoError(t, err) {
							continue
						}
						data, err := io.ReadAll(file)
						_ = file.Close()
						assert.NoError(t, err)
						assert.Equal(t, imageBytes, data)
						assert.Equal(t, "image/png", header.Header.Get("Content-Type"))
					}
					w.WriteHeader(http.StatusAccepted)
					_, _ = io.WriteString(w, `{"id":"upstream-id","status":"queued"}`)
				}))
				defer server.Close()

				info := relayInfo("my-video")
				info.ChannelBaseUrl = server.URL + "/v1/"
				info.PublicTaskID = "task_public"
				adaptor := &VDNTaskAdaptor{}
				adaptor.Init(info)
				require.Nil(t, adaptor.ValidateRequestAndSetAction(ctx, info))
				info.UpstreamModelName = "arbitrary-upstream-name"
				body, err := adaptor.BuildRequestBody(ctx, info)
				require.NoError(t, err)
				resp, err := adaptor.DoRequest(ctx, info, body)
				require.NoError(t, err)
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
				id, _, taskErr := adaptor.DoResponse(ctx, resp, info)
				require.Nil(t, taskErr)
				assert.Equal(t, "upstream-id", id)
				assert.Equal(t, map[string]float64{"duration": 14.375}, adaptor.EstimateBilling(ctx, info))

				billingBody, err := adaptor.BillingRequestBody()
				require.NoError(t, err)
				evaluation, err := taskbilling.EvaluatePricing(taskbilling.Config{
					Version: 1, Mode: taskbilling.ModePerSecond,
					Duration:  &taskbilling.Dimension{Paths: []string{"duration"}, Default: 5},
					Surcharge: &taskbilling.Surcharge{Name: "images", Kind: taskbilling.SurchargeItemCount, Paths: []string{"images"}, UnitPrice: 0.2},
				}, billingBody)
				require.NoError(t, err)
				assert.Equal(t, map[string]float64{"duration": 14.375}, evaluation.Ratios)
				require.NotNil(t, evaluation.Surcharge)
				assert.Equal(t, len(frames), evaluation.Surcharge.Count)

				poll, err := adaptor.FetchTask(server.URL, "secret", map[string]any{"task_id": id}, "")
				require.NoError(t, err)
				defer poll.Body.Close()
				pollBody, err := io.ReadAll(poll.Body)
				require.NoError(t, err)
				result, err := adaptor.ParseTaskResult(pollBody)
				require.NoError(t, err)
				assert.Equal(t, model.TaskStatusSuccess, result.Status)
			})
		}
	}
}

func TestVDNRejectsUnsupportedRequests(t *testing.T) {
	for _, tc := range []struct{ name, fields, message string }{
		{"duration", `"duration":5`, "fixes duration"},
		{"null duration", `"duration":null`, "fixes duration"},
		{"size", `"size":"768x1344"`, "fixes size"},
		{"fps", `"fps":24`, "does not support"},
		{"frames", `"num_frames":345`, "does not support"},
		{"inference steps", `"num_inference_steps":8`, "does not support"},
		{"nfe", `"nfe":8`, "does not support"},
		{"task", `"task":"t2va"`, "does not support"},
		{"task type", `"task_type":"l2va"`, "does not support"},
		{"bf16 target", `"target":{"duration_seconds":5}`, "does not support"},
		{"fractional seed", `"seed":0.5`, "seed must be an integer"},
		{"bad image", `"first_frame":"data:image/png;base64,bm90IGFuIGltYWdl"`, "must be an image"},
		{"bad source", `"first_frame":"file:///etc/passwd"`, "HTTP(S) URL"},
		{"duplicate frame", `"first_frame":"a","input_reference":"b"`, "duplicate first_frame"},
		{"references", `"content":[{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/image.png"}}]`, "not reference materials"},
		{"audio", `"content":[{"type":"audio_url","role":"reference_audio","audio_url":{"url":"https://example.com/audio.wav"}}]`, "not reference materials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cleanup := requestContext(t, `{"model":"my-video","prompt":"animate",`+tc.fields+`}`)
			t.Cleanup(cleanup)
			adaptor := &VDNTaskAdaptor{}
			err := adaptor.ValidateRequestAndSetAction(ctx, relayInfo("my-video"))
			require.NotNil(t, err)
			assert.Equal(t, http.StatusBadRequest, err.StatusCode)
			assert.Contains(t, err.Message, tc.message)
		})
	}
}

func TestVDNPromptLengthAndOptionalSeed(t *testing.T) {
	for _, length := range []int{0, 16384, 16385} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			ctx, cleanup := requestContext(t, `{"model":"custom","prompt":"`+strings.Repeat("字", length)+`","seconds":"14.375","size":"1344x768"}`)
			t.Cleanup(cleanup)
			adaptor := &VDNTaskAdaptor{}
			info := relayInfo("custom")
			taskErr := adaptor.ValidateRequestAndSetAction(ctx, info)
			if length == 0 || length > 16384 {
				require.NotNil(t, taskErr)
				return
			}
			require.Nil(t, taskErr)
			body, err := adaptor.BuildRequestBody(ctx, info)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "/", body)
			require.NoError(t, adaptor.BuildRequestHeader(ctx, request, info))
			require.NoError(t, request.ParseMultipartForm(1<<20))
			defer request.MultipartForm.RemoveAll()
			assert.NotContains(t, request.MultipartForm.Value, "seed")
			assert.Equal(t, strings.Repeat("字", length), request.FormValue("prompt"))
		})
	}
}

func TestVDNImageSizeLimit(t *testing.T) {
	for _, format := range []string{"json", "multipart"} {
		t.Run(format, func(t *testing.T) {
			var input bytes.Buffer
			contentType := "application/json"
			data := bytes.Repeat([]byte{'x'}, (10<<20)+1)
			if format == "json" {
				input.WriteString(`{"model":"custom","prompt":"animate","last_frame":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(data) + `"}`)
			} else {
				writer := multipart.NewWriter(&input)
				require.NoError(t, writer.WriteField("model", "custom"))
				require.NoError(t, writer.WriteField("prompt", "animate"))
				part, err := writer.CreateFormFile("last_frame", "frame.png")
				require.NoError(t, err)
				_, err = part.Write(data)
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				contentType = writer.FormDataContentType()
			}
			ctx, cleanup := requestContext(t, input.String())
			t.Cleanup(cleanup)
			ctx.Request.Header.Set("Content-Type", contentType)
			taskErr := (&VDNTaskAdaptor{}).ValidateRequestAndSetAction(ctx, relayInfo("custom"))
			require.NotNil(t, taskErr)
			assert.Contains(t, taskErr.Message, "last_frame exceeds 10 MiB")
		})
	}
}

func TestVDNRunningAndTerminalStates(t *testing.T) {
	for _, tc := range []struct{ status, want string }{
		{"queued", model.TaskStatusQueued}, {"in_progress", model.TaskStatusInProgress},
		{"conditioning", model.TaskStatusInProgress}, {"generating", model.TaskStatusInProgress},
		{"waiting_decode", model.TaskStatusInProgress}, {"decoding", model.TaskStatusInProgress},
		{"completed", model.TaskStatusSuccess}, {"failed", model.TaskStatusFailure}, {"cancelled", model.TaskStatusFailure},
	} {
		result, err := (&VDNTaskAdaptor{}).ParseTaskResult([]byte(`{"id":"video-id","status":"` + tc.status + `"}`))
		require.NoError(t, err)
		assert.Equal(t, tc.want, result.Status)
	}
}

func TestVDNDownloadsFrameUsingSharedFetchPolicy(t *testing.T) {
	service.InitHttpClient()
	fetch := system_setting.GetFetchSetting()
	original := *fetch
	t.Cleanup(func() { *fetch = original })
	var frame bytes.Buffer
	require.NoError(t, png.Encode(&frame, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write(frame.Bytes())
	}))
	defer server.Close()
	address, err := url.Parse(server.URL)
	require.NoError(t, err)
	*fetch = system_setting.FetchSetting{
		EnableSSRFProtection: true, AllowPrivateIp: false, AllowedPorts: []string{address.Port()},
	}
	_, err = loadVDNFrame(server.URL + "/frame.png")
	require.Error(t, err)
	fetch.AllowPrivateIp = true
	data, err := loadVDNFrame(server.URL + "/frame.png")
	require.NoError(t, err)
	assert.Equal(t, frame.Bytes(), data)
}

func TestVDNRejectsOversizeMultipartBeforeParsing(t *testing.T) {
	ctx, cleanup := requestContext(t, strings.Repeat("x", (21<<20)+1))
	t.Cleanup(cleanup)
	ctx.Request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	taskErr := (&VDNTaskAdaptor{}).ValidateRequestAndSetAction(ctx, relayInfo("custom"))
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "request exceeds 21 MiB")
}
