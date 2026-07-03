#!/bin/bash
set -euo pipefail

# ============================================
#  构建 ncm-tool — 交叉编译多平台二进制
# ============================================

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SELF_DIR"

OUTPUT_DIR="$SELF_DIR/dist"
BINARY_NAME="ncm-tool"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || date -u '+%Y%m%d')}"
LDFLAGS="-s -w"

# 目标平台列表
PLATFORMS=(
  "darwin/arm64"
  "linux/amd64"
)

echo "============================================"
echo "  ncm-tool 交叉编译"
echo "  Version: $VERSION"
echo "  输出目录: $OUTPUT_DIR"
echo "============================================"
echo ""

rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

for PLATFORM in "${PLATFORMS[@]}"; do
  OS="${PLATFORM%/*}"
  ARCH="${PLATFORM#*/}"

  # 输出文件名：ncm-tool-darwin-arm64 / ncm-tool-linux-amd64
  OUTPUT_NAME="${BINARY_NAME}-${OS}-${ARCH}"
  OUTPUT_PATH="$OUTPUT_DIR/$OUTPUT_NAME"

  echo "🏗️   正在构建 $OS/$ARCH ..."

  GOOS="$OS" GOARCH="$ARCH" CGO_ENABLED=0 \
    go build -trimpath -ldflags="$LDFLAGS" -o "$OUTPUT_PATH" .

  FILE_SIZE=$(stat -f "%z" "$OUTPUT_PATH" 2>/dev/null || stat -c "%s" "$OUTPUT_PATH" 2>/dev/null)
  FILE_SIZE_MB=$(awk "BEGIN {printf \"%.2f\", $FILE_SIZE / 1024 / 1024}")
  echo "     ✓ $OUTPUT_NAME  (${FILE_SIZE_MB} MB)"
  echo ""
done

echo "============================================"
echo "  ✅ 构建完成"
echo "  输出目录: $OUTPUT_DIR"
echo "============================================"
echo ""
ls -lh "$OUTPUT_DIR"