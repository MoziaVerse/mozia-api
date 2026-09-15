# MoziaH3-VDN

`MoziaH3-VDN` 是独立渠道类型（208），使用 H3 VDN 的 multipart 协议。
原 `MoziaH3`（207）继续使用 JSON 协议。选择协议只看渠道类型，不判断模型名。

## 渠道配置

1. 新建渠道，类型选择 `MoziaH3-VDN`。
2. Base URL 填写一个 VDN 实例地址，支持带或不带 `/v1`；密钥填写该实例的 Bearer API Key。
3. 添加对外模型名，例如 `minimax/minimax-h3-vdn`，模型映射设置为：

   ```json
   {"minimax/minimax-h3-vdn":"VDN-MiniMax-H3"}
   ```

   两个名称都来自渠道配置，代码不维护模型名称白名单。
4. 每个实例建立独立渠道。查询和下载沿任务保存的渠道 ID 使用原实例；有未完成任务时不要更换该渠道的 Base URL。
5. 为新模型配置价格。按次计费使用 `task_billing` 的 `per_request`；按秒计费使用 `per_second`，读取 `duration` 或 `seconds`。固定时长为 **14.375 秒**，是否向上取整由计费配置决定。未配置 `task_billing` 时，沿用 H3 的时长倍率，即基础价格乘以 14.375。

不建议将 VDN 与输出规格不同的 BF16 模型配置成同一个对外模型。

## 客户端请求

支持 `POST /v1/videos` 和 `POST /v1/video/generations`，平台对外接受 JSON 或 multipart，向上游统一发送 multipart。

```json
{
  "model": "minimax/minimax-h3-vdn",
  "prompt": "一位行人沿着阳光下的街道缓慢前行",
  "seed": 0,
  "content": [
    {
      "type": "image_url",
      "role": "first_frame",
      "image_url": {"url": "https://example.com/first.png"}
    }
  ]
}
```

- 不提供图片：文生视频；只提供首帧：首帧生视频；首尾都有：首尾帧；只提供尾帧：尾帧约束。
- `content` 中图片角色为 `first_frame` / `last_frame`，文字可用 `type=text` 提供；图片支持 HTTP(S) URL 或 base64 image data URL。
- JSON 也支持顶层 `first_frame` / `last_frame`；`input_reference` 或 `image` 表示首帧，`images` 按首、尾顺序最多两张。同一帧重复指定会被拒绝。
- multipart 可上传文件字段 `first_frame` / `last_frame`，文字字段为 `model`、`prompt`、可选 `seed`。
- `seed` 缺省时不发送，由上游使用默认值 42；显式 `0` 会保留。
- `prompt` 为 1–16,384 个字符，每张图片最多 10 MiB，multipart 请求最多 21 MiB。
- 输出固定为 1344×768、345 帧、24 FPS、8 NFE，约 14.375 秒。建议省略规格参数；如指定 `duration`、`seconds`、`size` 或 `resolution`，必须与固定值一致（`resolution=768p`）。不接受 `fps`、`num_frames`、`num_inference_steps` 或 `nfe` 参数。
- 其他字段会返回 400，包括 `task`、`task_type`、`target`、`conditions`、`metadata`；不支持参考音频、参考视频或参考图片模式。

创建响应沿用平台公开 `task_...` ID；查询、下载继续使用这个 ID，不向客户端暴露实例密钥。
上游 HTTP 202 会转换为平台现有的 HTTP 200 创建响应。

## 重试与接口边界

上游明确返回 429 时允许按网关重试配置切换渠道。网络错误、提交超时、5xx 或损坏的创建响应均不自动重提，因为 VDN 没有幂等键，任务可能已经被接受。

本接入包含创建、状态查询和视频下载。上游的取消与 lifecycle 接口未新增为平台公开接口。
