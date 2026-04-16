#!/usr/bin/env bash
set -euo pipefail

# commit-guard installer
# usage: curl -sL https://raw.githubusercontent.com/codywilliamson/commit-guard/main/install.sh | bash
# or:    ./install.sh [--ci-only] [--config conventional|angular] [--pm pnpm|npm|yarn]

GUARD_REPO="codywilliamson/commit-guard"
GUARD_BRANCH="main"
TEMPLATE_URL="https://raw.githubusercontent.com/${GUARD_REPO}/${GUARD_BRANCH}/caller-template.yml"
WORKFLOW_DIR=".github/workflows"
WORKFLOW_FILE="${WORKFLOW_DIR}/commitlint.yml"

# defaults
CI_ONLY=false
CONFIG="conventional"
PM=""

# parse args
while [[ $# -gt 0 ]]; do
  case "$1" in
    --ci-only) CI_ONLY=true; shift ;;
    --config) CONFIG="$2"; shift 2 ;;
    --pm) PM="$2"; shift 2 ;;
    --help|-h)
      echo "commit-guard installer"
      echo ""
      echo "usage: install.sh [options]"
      echo ""
      echo "options:"
      echo "  --ci-only           Only install the CI workflow, skip local hooks"
      echo "  --config <preset>   Commitlint config: conventional (default), angular"
      echo "  --pm <manager>      Package manager: pnpm, npm, yarn (auto-detected if omitted)"
      echo "  --help              Show this help"
      exit 0
      ;;
    *) echo "unknown option: $1"; exit 1 ;;
  esac
done

# check we're in a git repo
if ! git rev-parse --is-inside-work-tree &>/dev/null; then
  echo "error: not a git repository. run this from your project root."
  exit 1
fi

# detect package manager
detect_pm() {
  if [[ -n "$PM" ]]; then return; fi
  if [[ -f "pnpm-lock.yaml" ]]; then PM="pnpm"
  elif [[ -f "yarn.lock" ]]; then PM="yarn"
  elif [[ -f "package-lock.json" ]] || [[ -f "package.json" ]]; then PM="npm"
  else PM=""; fi
}

detect_pm

# -- ci workflow --
echo "installing CI workflow..."
mkdir -p "$WORKFLOW_DIR"

curl -sL "$TEMPLATE_URL" -o "$WORKFLOW_FILE"

if [[ "$CONFIG" != "conventional" ]]; then
  sed -i "s|config: \"conventional\"|config: \"${CONFIG}\"|" "$WORKFLOW_FILE"
fi

echo "  installed: ${WORKFLOW_FILE}"

if [[ "$CI_ONLY" == true ]]; then
  echo ""
  echo "done (ci-only mode). commit and push to activate."
  exit 0
fi

# -- local hooks --
if [[ -z "$PM" ]]; then
  echo ""
  echo "no package.json found, skipping local hooks (ci-only)."
  echo "done. commit and push to activate."
  exit 0
fi

echo ""
echo "installing local hooks (${PM})..."

CONFIG_PKG="@commitlint/config-conventional"
if [[ "$CONFIG" == "angular" ]]; then
  CONFIG_PKG="@commitlint/config-angular"
fi

# install dependencies
case "$PM" in
  pnpm) pnpm add -Dw @commitlint/cli "$CONFIG_PKG" husky ;;
  yarn) yarn add -D @commitlint/cli "$CONFIG_PKG" husky ;;
  npm)  npm install -D @commitlint/cli "$CONFIG_PKG" husky ;;
esac

# commitlint config (skip if one already exists)
if [[ ! -f "commitlint.config.js" ]] && [[ ! -f "commitlint.config.mjs" ]] && [[ ! -f "commitlint.config.cjs" ]] && [[ ! -f ".commitlintrc.yml" ]] && [[ ! -f ".commitlintrc.json" ]]; then
  EXTENDS="@commitlint/config-conventional"
  if [[ "$CONFIG" == "angular" ]]; then
    EXTENDS="@commitlint/config-angular"
  fi

  cat > commitlint.config.js <<CONF
export default {
  extends: ["${EXTENDS}"],
};
CONF
  echo "  created: commitlint.config.js"
else
  echo "  commitlint config already exists, skipping"
fi

# husky setup
npx husky init 2>/dev/null || true

# commit-msg hook
mkdir -p .husky
cat > .husky/commit-msg <<'HOOK'
pnpm exec commitlint --edit "$1"
HOOK

# adjust hook for non-pnpm
if [[ "$PM" == "npm" ]]; then
  sed -i 's|pnpm exec|npx|' .husky/commit-msg
elif [[ "$PM" == "yarn" ]]; then
  sed -i 's|pnpm exec|yarn|' .husky/commit-msg
fi

chmod +x .husky/commit-msg
echo "  created: .husky/commit-msg"

# add prepare script if missing
if ! grep -q '"prepare"' package.json 2>/dev/null; then
  node -e "
    const pkg = JSON.parse(require('fs').readFileSync('package.json', 'utf-8'));
    pkg.scripts = pkg.scripts || {};
    pkg.scripts.prepare = 'husky';
    require('fs').writeFileSync('package.json', JSON.stringify(pkg, null, 2) + '\n');
  "
  echo "  added prepare script to package.json"
fi

echo ""
echo "done! installed:"
echo "  - CI workflow: ${WORKFLOW_FILE}"
echo "  - Local hook: .husky/commit-msg"
echo "  - Config: commitlint.config.js"
echo ""
echo "commit and push to activate CI checks."
