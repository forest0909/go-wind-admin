#!/usr/bin/env bash
# 防止依赖回流到上游模块。
# 扫描 go.mod、buf.yaml、Makefile / app.mk，以及全部 .go。
# 命中 github.com/tx7do、buf.build/tx7do、buf.build/go-wind 就失败。
#
# 两份生成代码里的文档 URL 除外。它们不是 import。改掉会破坏 protobuf
# 描述符长度，或者让生成链不再和已提交的产物一致：
#   backend/api/gen/go/admin/service/v1/admin_doc.pb.go
#   backend/third_party/go-crud/api/gen/go/pagination/v1/pagination.pb.go
# 这两份文件仍然检查带引号的 import。proto 和 openapi.yaml 不在本次扫描范围。

set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

pattern='github.com/tx7do|buf\.build/tx7do|buf\.build/go-wind'
import_pattern='"github.com/tx7do/|buf\.build/tx7do|buf\.build/go-wind'

is_url_exception() {
  case "$1" in
    backend/api/gen/go/admin/service/v1/admin_doc.pb.go|\
    backend/third_party/go-crud/api/gen/go/pagination/v1/pagination.pb.go)
      return 0
      ;;
  esac
  return 1
}

fail=0

note_hits() {
  local file="$1"
  local hits="$2"
  if [[ -n "$hits" ]]; then
    echo "$file"
    echo "$hits"
    fail=1
  fi
}

scan_file() {
  local file="$1"
  local hits
  hits="$(grep -nE "$pattern" "$file" || true)"
  note_hits "$file" "$hits"
}

while IFS= read -r file; do
  scan_file "${file#./}"
done < <(find . -name go.mod -not -path './.git/*' | sort)

while IFS= read -r file; do
  scan_file "${file#./}"
done < <(find . -name buf.yaml -not -path './.git/*' | sort)

while IFS= read -r file; do
  scan_file "${file#./}"
done < <(find . \( -name Makefile -o -name app.mk \) -not -path './.git/*' | sort)

while IFS= read -r file; do
  rel="${file#./}"
  if is_url_exception "$rel"; then
    import_hits="$(grep -nE "$import_pattern" "$file" || true)"
    note_hits "$rel" "$import_hits"
    continue
  fi
  hits="$(grep -nE "$pattern" "$file" || true)"
  note_hits "$rel" "$hits"
done < <(find . -name '*.go' -not -path './.git/*' | sort)

if [[ "$fail" -ne 0 ]]; then
  echo "upstream dependency markers found" >&2
  exit 1
fi

echo "no upstream dependency markers in go.mod, buf.yaml, Makefile, or Go sources"
