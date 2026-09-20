package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// MaterialObjectStorage 是素材上传落盘用的 S3 兼容对象存储（生产是 H3 机房内的 SeaweedFS）。
//
// 它有两个门：
//   - Endpoint（公网）：网关、客户、外部供应商从这里进；上传和给客户的 file_url 都用它
//   - InternalEndpoint（集群内）：只有 H3 worker 能到；派任务给自托管 H3 时把 URI 换成这边签的
//
// S3 预签名绑定 host，所以同一个对象要签两把钥匙。签名用仓库里已有的 aws signer v4，不加依赖。
type MaterialObjectStorage struct {
	Endpoint         string // http://117.161.30.178:30333
	InternalEndpoint string // http://172.16.10.151:30333，为空则不做内网重写
	Bucket           string // h3-inputs
	Region           string // us-east-1（S3 兼容接口的占位区域）
	AccessKeyID      string
	AccessKeySecret  string
	Prefix           string        // materials/
	PublicTTL        time.Duration // 给客户的预签名有效期
	InternalTTL      time.Duration // 给 worker 的预签名有效期（要盖过排队时间）
	HTTPClient       *http.Client
	Now              func() time.Time // 可注入，测试用固定时钟
	signer           *v4.Signer
}

const (
	defaultMaterialPublicTTL   = 7 * 24 * time.Hour // S3 预签名上限
	defaultMaterialInternalTTL = 24 * time.Hour
	unsignedPayload            = "UNSIGNED-PAYLOAD"
)

var (
	materialStorageOnce sync.Once
	materialStorage     *MaterialObjectStorage
)

// SetMaterialObjectStorage 覆盖全局实例（测试注入；传 nil 退回 Cool 代理路径）。
func SetMaterialObjectStorage(s *MaterialObjectStorage) {
	materialStorageOnce.Do(func() {})
	if s != nil {
		s.normalize()
	}
	materialStorage = s
}

// GetMaterialObjectStorage 读取 MATERIAL_S3_* 环境变量。必填项配齐才启用，
// 否则返回 nil，素材上传回落到原来的 Cool 代理路径——回滚只需删 env 重启。
func GetMaterialObjectStorage() *MaterialObjectStorage {
	materialStorageOnce.Do(func() {
		materialStorage = newMaterialObjectStorageFromEnv()
	})
	return materialStorage
}

func newMaterialObjectStorageFromEnv() *MaterialObjectStorage {
	s := &MaterialObjectStorage{
		Endpoint:         strings.TrimSpace(common.GetEnvOrDefaultString("MATERIAL_S3_ENDPOINT", "")),
		InternalEndpoint: strings.TrimSpace(common.GetEnvOrDefaultString("MATERIAL_S3_INTERNAL_ENDPOINT", "")),
		Bucket:           strings.TrimSpace(common.GetEnvOrDefaultString("MATERIAL_S3_BUCKET", "")),
		Region:           strings.TrimSpace(common.GetEnvOrDefaultString("MATERIAL_S3_REGION", "us-east-1")),
		AccessKeyID:      strings.TrimSpace(common.GetEnvOrDefaultString("MATERIAL_S3_ACCESS_KEY_ID", "")),
		AccessKeySecret:  strings.TrimSpace(common.GetEnvOrDefaultString("MATERIAL_S3_ACCESS_KEY_SECRET", "")),
		Prefix:           strings.TrimSpace(common.GetEnvOrDefaultString("MATERIAL_S3_PREFIX", "materials/")),
		PublicTTL:        time.Duration(common.GetEnvOrDefault("MATERIAL_S3_PUBLIC_TTL_SECONDS", 0)) * time.Second,
		InternalTTL:      time.Duration(common.GetEnvOrDefault("MATERIAL_S3_INTERNAL_TTL_SECONDS", 0)) * time.Second,
	}
	if s.Endpoint == "" || s.Bucket == "" || s.AccessKeyID == "" || s.AccessKeySecret == "" {
		return nil
	}
	return s.normalize()
}

func (s *MaterialObjectStorage) normalize() *MaterialObjectStorage {
	s.Endpoint = strings.TrimRight(s.Endpoint, "/")
	s.InternalEndpoint = strings.TrimRight(s.InternalEndpoint, "/")
	s.Prefix = strings.TrimLeft(s.Prefix, "/")
	if s.Prefix != "" && !strings.HasSuffix(s.Prefix, "/") {
		s.Prefix += "/"
	}
	if s.Region == "" {
		s.Region = "us-east-1"
	}
	s.PublicTTL = s.publicTTL()
	s.InternalTTL = s.internalTTL()
	if s.HTTPClient == nil {
		s.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	s.signerV4()
	return s
}

// signerV4 懒初始化，直接构造的实例（测试）也能用。
func (s *MaterialObjectStorage) signerV4() *v4.Signer {
	if s.signer == nil {
		// SeaweedFS 走 path-style（/bucket/key），key 里只有 hash/日期/扩展名，无需二次转义
		s.signer = v4.NewSigner(func(o *v4.SignerOptions) { o.DisableURIPathEscaping = true })
	}
	return s.signer
}

func (s *MaterialObjectStorage) clock() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

func (s *MaterialObjectStorage) publicTTL() time.Duration {
	if s.PublicTTL <= 0 {
		return defaultMaterialPublicTTL
	}
	return s.PublicTTL
}

func (s *MaterialObjectStorage) internalTTL() time.Duration {
	if s.InternalTTL <= 0 {
		return defaultMaterialInternalTTL
	}
	return s.InternalTTL
}

func (s *MaterialObjectStorage) credentials() aws.Credentials {
	return aws.Credentials{AccessKeyID: s.AccessKeyID, SecretAccessKey: s.AccessKeySecret}
}

// objectURL 拼 path-style 地址：{endpoint}/{bucket}/{key}
func (s *MaterialObjectStorage) objectURL(endpoint string, key string) (*url.URL, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid material storage endpoint: %q", endpoint)
	}
	parsed.Path = "/" + s.Bucket + "/" + key
	parsed.RawQuery = ""
	return parsed, nil
}

// ObjectKey 生成对象名：{prefix}{yyyy/mm/dd}/{sha256 前 32 位}.{ext}。
// 同内容同 key，重复 PUT 无害；日期段方便清理和排查。
func (s *MaterialObjectStorage) ObjectKey(sha256Hex string, ext string) string {
	day := s.clock().Format("2006/01/02")
	return fmt.Sprintf("%s%s/%s.%s", s.Prefix, day, sha256Hex[:32], strings.TrimLeft(ext, "."))
}

// PutObject 上传一个对象。payloadSHA256 是内容的 sha256 hex，签名会把它绑进去；size 进 Content-Length。
func (s *MaterialObjectStorage) PutObject(ctx context.Context, key string, contentType string, body io.Reader, size int64, payloadSHA256 string) error {
	target, err := s.objectURL(s.Endpoint, key)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, target.String(), common.ReaderOnly(body))
	if err != nil {
		return err
	}
	request.ContentLength = size
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("X-Amz-Content-Sha256", payloadSHA256)
	if err := s.signerV4().SignHTTP(ctx, s.credentials(), request, payloadSHA256, "s3", s.Region, s.clock()); err != nil {
		return fmt.Errorf("sign put: %w", err)
	}

	response, err := s.HTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("s3 put %s returned HTTP %d: %s", key, response.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}

// PublicURL 返回给客户的预签名 GET（公网门）。
func (s *MaterialObjectStorage) PublicURL(key string) (string, error) {
	return s.presignGet(s.Endpoint, key, s.publicTTL())
}

// InternalURL 返回给 H3 worker 的预签名 GET（集群内门）。未配内网地址时返回公网的。
func (s *MaterialObjectStorage) InternalURL(key string) (string, error) {
	if s.InternalEndpoint == "" {
		return s.PublicURL(key)
	}
	return s.presignGet(s.InternalEndpoint, key, s.internalTTL())
}

func (s *MaterialObjectStorage) presignGet(endpoint string, key string, ttl time.Duration) (string, error) {
	target, err := s.objectURL(endpoint, key)
	if err != nil {
		return "", err
	}
	query := target.Query()
	query.Set("X-Amz-Expires", strconv.FormatInt(int64(ttl/time.Second), 10))
	target.RawQuery = query.Encode()
	request, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return "", err
	}
	signed, _, err := s.signerV4().PresignHTTP(context.Background(), s.credentials(), request, unsignedPayload, "s3", s.Region, s.clock())
	if err != nil {
		return "", fmt.Errorf("presign get: %w", err)
	}
	return signed, nil
}

// ObjectKeyFromURL 判断一个 URL 是不是本存储里的对象（公网或内网门都算），是则返回 key。
// 只看 host + path，忽略原有签名——网关持有密钥，可以对任何自家对象重新签。
func (s *MaterialObjectStorage) ObjectKeyFromURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return "", false
	}
	for _, endpoint := range []string{s.Endpoint, s.InternalEndpoint} {
		if endpoint == "" {
			continue
		}
		base, err := url.Parse(endpoint)
		if err != nil || !strings.EqualFold(base.Host, parsed.Host) {
			continue
		}
		prefix := "/" + s.Bucket + "/"
		if !strings.HasPrefix(parsed.Path, prefix) {
			return "", false
		}
		key := strings.TrimPrefix(parsed.Path, prefix)
		if key == "" || strings.Contains(key, "..") {
			return "", false
		}
		return key, true
	}
	return "", false
}

// RewriteForCluster 把指向本存储的 URL 换成集群内门的预签名 URL；不是本存储的原样返回。
// 自托管 H3 适配器在提交前对每个素材 URI 调一次——签名在派发那一刻生成，不受上传后排队多久影响。
func (s *MaterialObjectStorage) RewriteForCluster(raw string) (string, error) {
	key, ok := s.ObjectKeyFromURL(raw)
	if !ok {
		return raw, nil
	}
	return s.InternalURL(key)
}

// RewriteMaterialURLForCluster 是 RewriteForCluster 的包级入口，未启用存储时原样返回。
func RewriteMaterialURLForCluster(raw string) string {
	s := GetMaterialObjectStorage()
	if s == nil {
		return raw
	}
	rewritten, err := s.RewriteForCluster(raw)
	if err != nil {
		common.SysError(fmt.Sprintf("material url rewrite failed for %s: %v", raw, err))
		return raw
	}
	return rewritten
}
