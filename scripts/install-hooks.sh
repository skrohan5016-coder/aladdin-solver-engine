#!/usr/bin/env bash
#
# Installs a pre-push hook that runs scripts/ci.sh before anything leaves your
# machine. This catches a broken boundary before it is published rather than
# after, which matters more here than in a normal project: once a signing path
# is pushed to a public remote, deleting the commit does not unpublish it.
#
#   bash scripts/install-hooks.sh
#
# Bypass for a genuine emergency:  git push --no-verify
#
set -euo pipefail

cd "$(dirname "$0")/.."

if [ ! -d .git ]; then
  echo "not a git repository — run this from a clone, not a tarball" >&2
  exit 1
fi

mkdir -p .git/hooks
cat > .git/hooks/pre-push <<'HOOK'
#!/usr/bin/env bash
echo "running project gates before push (skip with --no-verify)"
if ! bash scripts/ci.sh; then
  echo "push aborted: fix the failures above, or use --no-verify if you are sure"
  exit 1
fi
HOOK

chmod +x .git/hooks/pre-push
echo "pre-push hook installed at .git/hooks/pre-push"
echo "every push now runs scripts/ci.sh first"
