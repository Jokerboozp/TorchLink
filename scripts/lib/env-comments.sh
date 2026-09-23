#!/usr/bin/env bash

# 执行当前脚本步骤。
env_comments_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# 执行当前脚本步骤。
deployment_env_comments_file="$env_comments_dir/env-comments.tsv"
# 执行当前脚本步骤。
unset env_comments_dir

# 执行当前脚本步骤。
deployment_env_comment() {
  # 执行当前脚本步骤。
  local key="$1" description
  # 执行当前脚本步骤。
  description="$(awk -F '\t' -v key="$key" '$1 == key { print substr($0, index($0, "\t") + 1); exit }' "$deployment_env_comments_file")"
  # 判断条件后执行对应操作。
  if [ -n "$description" ]; then
    # 执行当前脚本步骤。
    printf '%s' "$description"
  # 执行当前脚本步骤。
  else
    # 执行当前脚本步骤。
    printf '自定义配置项 %s' "$key"
  # 结束当前控制块。
  fi
# 结束当前控制块。
}

# 执行当前脚本步骤。
annotate_deployment_env_file() {
  # 执行当前脚本步骤。
  local env_path="$1" tmp line key
  # 执行当前脚本步骤。
  tmp="$(mktemp "${env_path}.comments.XXXXXX")"
  # 遍历数据并执行循环体。
  while IFS= read -r line || [ -n "$line" ]; do
    # 执行当前脚本步骤。
    line="${line%$'\r'}"
    # 执行当前脚本步骤。
    case "$line" in '# 配置说明：'*) continue;; esac
    # 判断条件后执行对应操作。
    if [[ "$line" =~ ^[[:space:]]*(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*= ]]; then
      # 执行当前脚本步骤。
      key="${BASH_REMATCH[2]}"
      # 执行当前脚本步骤。
      printf '# 配置说明：%s\n' "$(deployment_env_comment "$key")" >> "$tmp"
    # 结束当前控制块。
    fi
    # 执行当前脚本步骤。
    printf '%s\n' "$line" >> "$tmp"
  # 结束当前控制块。
  done < "$env_path"
  # 执行当前脚本步骤。
  mv -- "$tmp" "$env_path"
# 结束当前控制块。
}
