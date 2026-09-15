#!/usr/bin/env bash
# 版本一致性校验脚本
# 用途：在 CI 中校验 VERSION 文件与 git tag 是否一致，防止版本漂移
# 用法：bash scripts/check_version.sh

set -euo pipefail

VERSION_FILE="VERSION"

if [ ! -f "$VERSION_FILE" ]; then
  echo "::error::VERSION file not found"
  exit 1
fi

VERSION=$(tr -d '[:space:]' < "$VERSION_FILE")
if [ -z "$VERSION" ]; then
  echo "::error::VERSION file is empty"
  exit 1
fi

# 校验语义化版本格式 (major.minor.patch)
if ! echo "$VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "::error::VERSION '$VERSION' does not match semver format (major.minor.patch)"
  exit 1
fi

echo "VERSION file: $VERSION"

# 在 CI 环境中校验 git tag
if [ "${GITHUB_REF_TYPE:-}" = "tag" ]; then
  GIT_TAG="${GITHUB_REF_NAME#v}"
  echo "Git tag (stripped v prefix): $GIT_TAG"
  if [ "$VERSION" != "$GIT_TAG" ]; then
    echo "::error::VERSION '$VERSION' does not match git tag '$GIT_TAG'"
    exit 1
  fi
  echo "✅ VERSION matches git tag"
fi

# 在本地环境校验 git tag (如果存在)
if command -v git >/dev/null 2>&1; then
  LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
  if [ -n "$LATEST_TAG" ]; then
    LATEST_TAG="${LATEST_TAG#v}"
    if [ "$VERSION" != "$LATEST_TAG" ]; then
      echo "⚠️  Warning: VERSION '$VERSION' does not match latest tag '$LATEST_TAG'"
      echo "    This is expected during development. CI will enforce this on release."
    else
      echo "✅ VERSION matches latest git tag"
    fi
  fi
fi

echo "✅ Version check passed"