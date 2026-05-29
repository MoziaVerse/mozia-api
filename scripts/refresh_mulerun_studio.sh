#!/usr/bin/env bash
# 从 mulerun CLI 重新生成 relay/channel/task/mulerun/registry.go
#
# 用法：
#   bash scripts/refresh_mulerun_studio.sh         # 默认写回项目
#   DRY_RUN=1 bash scripts/refresh_mulerun_studio.sh   # 只打印新内容、不落盘
#
# 前提：本机已安装 `mulerun` CLI（npm install -g @mulerunai/cli）。
# 当 mulerun 上线新模型 / 改动现有路径时，跑一次这个脚本即可同步。

set -euo pipefail

if ! command -v mulerun >/dev/null 2>&1; then
  echo "error: mulerun CLI not found in PATH; install via 'npm install -g @mulerunai/cli'" >&2
  exit 1
fi

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="${PROJECT_ROOT}/relay/channel/task/mulerun/registry.go"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

# 1. 拉全量 endpoint 列表，过滤描述噪音。
mulerun studio list 2>/dev/null \
  | grep -oE '[a-z0-9-]+/[a-z0-9.-]+(/[a-z0-9-]+)?' \
  | grep -E '/(generation|edit|text-to-video|image-to-video|reference-to-video|video-to-video)$' \
  | grep -vE '^(text|images|fixed)/' \
  | sort -u > "${TMP_DIR}/ids.txt"

echo "found $(wc -l < "${TMP_DIR}/ids.txt") endpoints" >&2

# 2. 对每个 endpoint 跑 params，并提取 API Path / Output Type / Result Key。
mkdir -p "${TMP_DIR}/params"
while read -r id; do
  safe="$(echo "$id" | tr '/' '_')"
  mulerun studio params "$id" > "${TMP_DIR}/params/${safe}.txt" 2>&1
done < "${TMP_DIR}/ids.txt"

# 3. 把每个 params 文件浓缩成 4 列 TSV（cli_id / api_path / output_type / result_key）。
> "${TMP_DIR}/registry.tsv"
for f in "${TMP_DIR}/params"/*.txt; do
  cli="$(awk 'NR==1{print; exit}' "$f")"
  path="$(awk '/^API Path:/{sub(/^API Path: /,""); print; exit}' "$f")"
  out="$(awk '/^Output Type:/{sub(/^Output Type: /,""); print; exit}' "$f")"
  rk="$(awk '/^Result Key:/{sub(/^Result Key: /,""); print; exit}' "$f")"
  printf "%s\t%s\t%s\t%s\n" "$cli" "$path" "$out" "$rk" >> "${TMP_DIR}/registry.tsv"
done
sort -o "${TMP_DIR}/registry.tsv" "${TMP_DIR}/registry.tsv"

awk -F'\t' '
  BEGIN {
    print "// Code generated from `mulerun studio params`. DO NOT EDIT MANUALLY."
    print "// Refresh: bash scripts/refresh_mulerun_studio.sh."
    print ""
    print "package mulerun"
    print ""
    print "import ("
    print "\t\"crypto/sha1\""
    print "\t\"encoding/hex\""
    print ")"
    print ""
    print "type StudioEndpoint struct {"
    print "\tCLIID      string"
    print "\tAPIPath    string"
    print "\tOutputType string"
    print "\tResultKey  string"
    print "}"
    print ""
    print "var StudioEndpoints = []StudioEndpoint{"
  }
  {
    printf "\t{CLIID: \"%s\", APIPath: \"%s\", OutputType: \"%s\", ResultKey: \"%s\"},\n", $1, $2, $3, $4
  }
  END {
    print "}"
    print ""
    print "var studioByCLIID = func() map[string]*StudioEndpoint {"
    print "\tm := make(map[string]*StudioEndpoint, len(StudioEndpoints))"
    print "\tfor i := range StudioEndpoints {"
    print "\t\tm[StudioEndpoints[i].CLIID] = &StudioEndpoints[i]"
    print "\t}"
    print "\treturn m"
    print "}()"
    print ""
    print "var studioByShortKey = func() map[string]*StudioEndpoint {"
    print "\tm := make(map[string]*StudioEndpoint, len(StudioEndpoints))"
    print "\tfor i := range StudioEndpoints {"
    print "\t\tm[ShortKey(StudioEndpoints[i].CLIID)] = &StudioEndpoints[i]"
    print "\t}"
    print "\treturn m"
    print "}()"
    print ""
    print "func LookupStudioEndpoint(cliID string) *StudioEndpoint {"
    print "\tif cliID == \"\" { return nil }"
    print "\treturn studioByCLIID[cliID]"
    print "}"
    print ""
    print "func LookupStudioByShortKey(shortKey string) *StudioEndpoint {"
    print "\tif shortKey == \"\" { return nil }"
    print "\treturn studioByShortKey[shortKey]"
    print "}"
    print ""
    print "func ShortKey(cliID string) string {"
    print "\tsum := sha1Sum([]byte(cliID))"
    print "\treturn hexEncode(sum[:4])"
    print "}"
    print ""
    print "func sha1Sum(b []byte) [20]byte {"
    print "\treturn sha1.Sum(b)"
    print "}"
    print ""
    print "func hexEncode(b []byte) string {"
    print "\treturn hex.EncodeToString(b)"
    print "}"
    print ""
    print "func StudioModelIDs() []string {"
    print "\tout := make([]string, 0, len(StudioEndpoints))"
    print "\tfor i := range StudioEndpoints {"
    print "\t\tout = append(out, StudioEndpoints[i].CLIID)"
    print "\t}"
    print "\treturn out"
    print "}"
  }
' "${TMP_DIR}/registry.tsv" > "${TMP_DIR}/studio_registry.go.new"

if [[ "${DRY_RUN:-0}" == "1" ]]; then
  echo "=== generated (DRY_RUN; not written) ===" >&2
  cat "${TMP_DIR}/studio_registry.go.new"
  exit 0
fi

mv "${TMP_DIR}/studio_registry.go.new" "$TARGET"
echo "wrote $TARGET" >&2
echo "next: run 'go test ./relay/channel/mulerun ./relay/channel/task/mulerun' to confirm." >&2
