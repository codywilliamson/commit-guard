#!/usr/bin/env bash
set -euo pipefail

# commit-guard installer
# usage: curl -sL https://raw.githubusercontent.com/codywilliamson/commit-guard/main/install.sh | bash
# or:    ./install.sh [options]

GUARD_REPO="${COMMIT_GUARD_REPO:-codywilliamson/commit-guard}"
GUARD_REF="${COMMIT_GUARD_REF:-v0.2.2}"
TEMPLATE_URL="${COMMIT_GUARD_TEMPLATE_URL:-https://raw.githubusercontent.com/${GUARD_REPO}/${GUARD_REF}/caller-template.yml}"
VALIDATOR_URL="${COMMIT_GUARD_VALIDATOR_URL:-https://raw.githubusercontent.com/${GUARD_REPO}/${GUARD_REF}/scripts/validate-commit-message.sh}"
WORKFLOW_DIR=".github/workflows"
WORKFLOW_FILE="${WORKFLOW_DIR}/commitlint.yml"

CI_ONLY=false
CONFIG="conventional"
HOOK_MODE="auto"
PM=""
PR_MODE="smart"

replace_line() {
  local search="$1"
  local replacement="$2"
  local file="$3"
  local temp_file

  temp_file="$(mktemp)"
  sed "s|${search}|${replacement}|" "$file" > "$temp_file"
  mv "$temp_file" "$file"
}

detect_pm() {
  if [[ -n "$PM" ]]; then
    return
  fi

  if [[ -f "pnpm-lock.yaml" ]]; then
    PM="pnpm"
  elif [[ -f "yarn.lock" ]]; then
    PM="yarn"
  elif [[ -f "package-lock.json" ]] || [[ -f "package.json" ]]; then
    PM="npm"
  fi
}

resolve_hook_mode() {
  case "$HOOK_MODE" in
    auto)
      if [[ -n "$PM" ]]; then
        HOOK_MODE="husky"
      else
        HOOK_MODE="native"
      fi
      ;;
    husky|native|none) ;;
    *)
      echo "error: invalid hook mode '${HOOK_MODE}'. expected auto, husky, native, or none."
      exit 1
      ;;
  esac
}

ensure_valid_pr_mode() {
  case "$PR_MODE" in
    smart|commits|title) ;;
    *)
      echo "error: invalid pr mode '${PR_MODE}'. expected smart, commits, or title."
      exit 1
      ;;
  esac
}

install_native_hook() {
  local current_hooks_path
  local hooks_dir
  local hook_file

  current_hooks_path="$(git config --get core.hooksPath || true)"
  hooks_dir="${current_hooks_path:-.githooks}"

  mkdir -p "$hooks_dir"
  hook_file="${hooks_dir}/commit-msg"
  curl -sL "$VALIDATOR_URL" -o "$hook_file"
  chmod +x "$hook_file"

  if [[ -z "$current_hooks_path" ]]; then
    git config core.hooksPath "$hooks_dir"
    echo "  configured core.hooksPath: ${hooks_dir}"
  fi

  echo "  created: ${hook_file}"
}

install_husky_hooks() {
  local config_pkg
  local hook_runner

  if [[ -z "$PM" ]]; then
    echo "error: husky mode requires a Node.js repo with a detected package manager."
    exit 1
  fi

  echo ""
  echo "installing local hooks (${PM}, husky)..."

  config_pkg="@commitlint/config-conventional"
  if [[ "$CONFIG" == "angular" ]]; then
    config_pkg="@commitlint/config-angular"
  fi

  case "$PM" in
    pnpm) pnpm add -Dw @commitlint/cli "$config_pkg" husky ;;
    yarn) yarn add -D @commitlint/cli "$config_pkg" husky ;;
    npm) npm install -D @commitlint/cli "$config_pkg" husky ;;
  esac

  if [[ ! -f "commitlint.config.js" ]] && [[ ! -f "commitlint.config.mjs" ]] && [[ ! -f "commitlint.config.cjs" ]] && [[ ! -f ".commitlintrc.yml" ]] && [[ ! -f ".commitlintrc.json" ]]; then
    local extends_pkg
    extends_pkg="@commitlint/config-conventional"
    if [[ "$CONFIG" == "angular" ]]; then
      extends_pkg="@commitlint/config-angular"
    fi

    cat > commitlint.config.js <<CONF
export default {
  extends: ["${extends_pkg}"],
};
CONF
    echo "  created: commitlint.config.js"
  else
    echo "  commitlint config already exists, skipping"
  fi

  npx husky init 2>/dev/null || true
  mkdir -p .husky

  case "$PM" in
    pnpm) hook_runner='pnpm exec commitlint' ;;
    yarn) hook_runner='yarn commitlint' ;;
    npm) hook_runner='npx --no -- commitlint' ;;
  esac

  cat > .husky/commit-msg <<HOOK
#!/usr/bin/env sh
${hook_runner} --edit "\$1"
HOOK

  chmod +x .husky/commit-msg
  echo "  created: .husky/commit-msg"

  if [[ -f "package.json" ]] && ! grep -q '"prepare"' package.json 2>/dev/null; then
    node -e "
      const fs = require('fs');
      const pkg = JSON.parse(fs.readFileSync('package.json', 'utf-8'));
      pkg.scripts = pkg.scripts || {};
      pkg.scripts.prepare = 'husky';
      fs.writeFileSync('package.json', JSON.stringify(pkg, null, 2) + '\n');
    "
    echo "  added prepare script to package.json"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ci-only) CI_ONLY=true; shift ;;
    --config) CONFIG="$2"; shift 2 ;;
    --hook-mode) HOOK_MODE="$2"; shift 2 ;;
    --pm) PM="$2"; shift 2 ;;
    --pr-mode) PR_MODE="$2"; shift 2 ;;
    --help|-h)
      echo "commit-guard installer"
      echo ""
      echo "usage: install.sh [options]"
      echo ""
      echo "options:"
      echo "  --ci-only              Only install the CI workflow, skip local hooks"
      echo "  --config <preset>      Commitlint config: conventional (default), angular"
      echo "  --hook-mode <mode>     Hook mode: auto (default), husky, native, none"
      echo "  --pm <manager>         Package manager: pnpm, npm, yarn (auto-detected if omitted)"
      echo "  --pr-mode <mode>       PR lint mode: smart (default), commits, title"
      echo "  --help                 Show this help"
      exit 0
      ;;
    *)
      echo "unknown option: $1"
      exit 1
      ;;
  esac
done

if ! git rev-parse --is-inside-work-tree &>/dev/null; then
  echo "error: not a git repository. run this from your project root."
  exit 1
fi

ensure_valid_pr_mode
detect_pm
resolve_hook_mode

echo "installing CI workflow..."
mkdir -p "$WORKFLOW_DIR"
curl -sL "$TEMPLATE_URL" -o "$WORKFLOW_FILE"

if [[ "$CONFIG" != "conventional" ]]; then
  replace_line 'config: "conventional"' "config: \"${CONFIG}\"" "$WORKFLOW_FILE"
fi

if [[ "$PR_MODE" != "smart" ]]; then
  replace_line 'pr-mode: "smart"' "pr-mode: \"${PR_MODE}\"" "$WORKFLOW_FILE"
fi

echo "  installed: ${WORKFLOW_FILE}"

if [[ "$CI_ONLY" == true ]] || [[ "$HOOK_MODE" == "none" ]]; then
  echo ""
  echo "done. commit and push to activate."
  exit 0
fi

case "$HOOK_MODE" in
  native)
    echo ""
    echo "installing local hooks (native git)..."
    install_native_hook
    ;;
  husky)
    install_husky_hooks
    ;;
esac

echo ""
echo "done! installed:"
echo "  - CI workflow: ${WORKFLOW_FILE}"

if [[ "$HOOK_MODE" == "native" ]]; then
  echo "  - Local hook: $(git config --get core.hooksPath)/commit-msg"
elif [[ "$HOOK_MODE" == "husky" ]]; then
  echo "  - Local hook: .husky/commit-msg"
  echo "  - Config: commitlint.config.js"
fi

echo ""
echo "commit and push to activate CI checks."
