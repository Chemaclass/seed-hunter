#!/usr/bin/env bash
#
# release.sh — cut a new seed-hunter release end-to-end.
#
# Usage:
#   ./release.sh <version> [--dry-run] [--no-tests] [--yes]
#
# Examples:
#   ./release.sh 0.2.0              # interactive, runs tests
#   ./release.sh 0.2.0 --dry-run    # show what would happen, change nothing
#   ./release.sh 0.2.0 --no-tests   # skip the local test run (riskier, faster)
#   ./release.sh 0.2.0 --yes        # skip the confirmation prompt
#
# What it does, in order:
#
#   1. Validates the working tree (clean, on main, gh authenticated, tag
#      doesn't exist, CHANGELOG.md has unreleased content).
#   2. Validates the version argument is plain semver (X.Y.Z).
#   3. Runs `make test` as a smoke check (unless --no-tests).
#   4. Updates CHANGELOG.md: moves the [Unreleased] heading to
#      [X.Y.Z] - YYYY-MM-DD, recreates an empty [Unreleased], and rewrites
#      the reference-link section at the bottom.
#   5. Commits the CHANGELOG bump.
#   6. Builds cross-platform binaries via `make release VERSION=X.Y.Z`.
#   7. Generates SHA256SUMS.
#   8. Tags vX.Y.Z (annotated).
#   9. Pushes the commit and the tag to origin.
#  10. Extracts the just-released CHANGELOG section into release notes.
#  11. Creates the GitHub release with binaries + checksums attached.
#  12. Cleans up dist/.
#
# If anything fails after step 5 (the local commit), the trap rolls back
# the commit and any local tag so the working tree is restored.

set -euo pipefail

# ── ANSI helpers ──────────────────────────────────────────────────────────

if [[ -t 1 ]]; then
    BOLD=$'\033[1m'; DIM=$'\033[2m'; RED=$'\033[31m'
    GREEN=$'\033[32m'; YELLOW=$'\033[33m'; BLUE=$'\033[34m'; RESET=$'\033[0m'
else
    BOLD=""; DIM=""; RED=""; GREEN=""; YELLOW=""; BLUE=""; RESET=""
fi

step()  { echo "${BLUE}${BOLD}==>${RESET}${BOLD} $*${RESET}"; }
info()  { echo "    $*"; }
warn()  { echo "${YELLOW}!! $*${RESET}" >&2; }
err()   { echo "${RED}${BOLD}error:${RESET} $*" >&2; }
ok()    { echo "${GREEN}✓${RESET} $*"; }

# ── Argument parsing ──────────────────────────────────────────────────────

VERSION=""
DRY_RUN=false
NO_TESTS=false
ASSUME_YES=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run)  DRY_RUN=true; shift ;;
        --no-tests) NO_TESTS=true; shift ;;
        --yes|-y)   ASSUME_YES=true; shift ;;
        -h|--help)
            sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        -*)
            err "unknown flag: $1"
            exit 2
            ;;
        *)
            if [[ -z "$VERSION" ]]; then
                VERSION="$1"
            else
                err "too many positional arguments (got '$1' after version '$VERSION')"
                exit 2
            fi
            shift
            ;;
    esac
done

if [[ -z "$VERSION" ]]; then
    err "version is required. Usage: $0 <version> [--dry-run] [--no-tests] [--yes]"
    exit 2
fi

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    err "version must be semver X.Y.Z (no leading 'v', no suffix). Got: $VERSION"
    exit 2
fi

TAG="v${VERSION}"
TODAY=$(date -u +%Y-%m-%d)

# ── DRY RUN helper ────────────────────────────────────────────────────────

run() {
    if $DRY_RUN; then
        echo "${DIM}DRY RUN:${RESET} $*"
    else
        "$@"
    fi
}

# ── Rollback trap ─────────────────────────────────────────────────────────

COMMIT_MADE=false
TAG_MADE=false

rollback() {
    local exit_code=$?
    if [[ $exit_code -eq 0 ]]; then
        return
    fi
    # Pre-commit failures (dirty tree, missing tag, etc.) are already
    # reported with a clear `err` message — no rollback needed and no
    # extra noise.
    if ! $COMMIT_MADE && ! $TAG_MADE; then
        exit "$exit_code"
    fi
    echo
    err "release failed at line $BASH_LINENO with exit code $exit_code"
    if $TAG_MADE && [[ "${PUSHED_TAG:-false}" != "true" ]]; then
        warn "rolling back local tag $TAG"
        git tag -d "$TAG" >/dev/null 2>&1 || true
    fi
    if $COMMIT_MADE && [[ "${PUSHED_COMMIT:-false}" != "true" ]]; then
        warn "rolling back local commit (git reset --hard HEAD~1)"
        git reset --hard HEAD~1 >/dev/null 2>&1 || true
    fi
    if [[ "${PUSHED_COMMIT:-false}" == "true" || "${PUSHED_TAG:-false}" == "true" ]]; then
        warn "the commit and/or tag were already pushed to origin —"
        warn "manual rollback required:"
        warn "  git push --delete origin $TAG    # delete remote tag"
        warn "  git revert HEAD                  # revert the changelog commit, then push"
    fi
    exit "$exit_code"
}
trap rollback EXIT

# ── Step 1: validate environment ──────────────────────────────────────────

step "Validating environment"

if ! command -v git >/dev/null 2>&1; then
    err "git is required"
    exit 1
fi
if ! command -v gh >/dev/null 2>&1; then
    err "gh CLI is required (https://cli.github.com)"
    exit 1
fi
if ! command -v make >/dev/null 2>&1; then
    err "make is required"
    exit 1
fi
if ! gh auth status >/dev/null 2>&1; then
    err "gh CLI is not authenticated. Run 'gh auth login' first."
    exit 1
fi

REPO_URL=$(git config --get remote.origin.url \
    | sed -e 's|^git@github\.com:|https://github.com/|' -e 's|\.git$||')
if [[ -z "$REPO_URL" ]]; then
    err "could not determine remote URL"
    exit 1
fi
info "repo: $REPO_URL"

CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [[ "$CURRENT_BRANCH" != "main" ]]; then
    err "must be on the 'main' branch (currently on '$CURRENT_BRANCH')"
    exit 1
fi
info "branch: $CURRENT_BRANCH"

if [[ -n "$(git status --porcelain)" ]]; then
    err "working tree is dirty. Commit or stash your changes first."
    git status --short
    exit 1
fi
info "working tree: clean"

if git rev-parse "$TAG" >/dev/null 2>&1; then
    err "tag $TAG already exists locally"
    exit 1
fi
if git ls-remote --tags origin "refs/tags/$TAG" 2>/dev/null | grep -q "$TAG"; then
    err "tag $TAG already exists on origin"
    exit 1
fi
info "tag: $TAG (available)"

if [[ ! -f CHANGELOG.md ]]; then
    err "CHANGELOG.md not found at project root"
    exit 1
fi

# Verify there is at least ONE non-blank line under [Unreleased]. If the
# section is empty, the user forgot to write release notes.
UNRELEASED_BODY=$(awk '
    /^## \[Unreleased\]/ { capture = 1; next }
    /^## \[/ && capture  { exit }
    capture && /[^[:space:]]/ { print }
' CHANGELOG.md)

if [[ -z "$UNRELEASED_BODY" ]]; then
    err "CHANGELOG.md [Unreleased] section is empty. Add release notes there first."
    exit 1
fi
info "changelog: [Unreleased] has content"

PREV_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
if [[ -n "$PREV_TAG" ]]; then
    PREV_VERSION="${PREV_TAG#v}"
    info "previous: $PREV_TAG"
else
    PREV_VERSION="0.0.0"
    info "previous: (none — this is the first release)"
fi

ok "environment looks good"

# ── Step 2: print plan and confirm ────────────────────────────────────────

echo
step "Release plan"
info "version:      ${BOLD}${VERSION}${RESET}"
info "tag:          $TAG"
info "date:         $TODAY"
info "previous:     ${PREV_VERSION}"
info "tests:        $(if $NO_TESTS; then echo 'SKIPPED'; else echo 'will run'; fi)"
info "dry run:      $(if $DRY_RUN; then echo 'YES'; else echo 'no'; fi)"

if ! $DRY_RUN && ! $ASSUME_YES; then
    echo
    read -r -p "Proceed with release? [y/N] " answer
    case "$answer" in
        y|Y|yes|YES) ;;
        *)
            warn "aborted by user"
            trap - EXIT
            exit 0
            ;;
    esac
fi

# ── Step 3: tests ─────────────────────────────────────────────────────────

if ! $NO_TESTS; then
    step "Running tests"
    run make test
    ok "tests pass"
fi

# ── Step 4: update CHANGELOG.md ───────────────────────────────────────────

step "Updating CHANGELOG.md"

if $DRY_RUN; then
    info "DRY RUN: would rewrite [Unreleased] -> [$VERSION] - $TODAY and update reference links"
else
    awk \
        -v version="$VERSION" \
        -v prev="$PREV_VERSION" \
        -v date="$TODAY" \
        -v repo="$REPO_URL" \
        '
        BEGIN { inserted_heading = 0; inserted_link = 0; saw_prev_link = 0 }

        # Replace the [Unreleased] heading with [Unreleased] + new version heading
        /^## \[Unreleased\]/ && !inserted_heading {
            print
            print ""
            print "## [" version "] - " date
            inserted_heading = 1
            next
        }

        # Replace the [Unreleased] reference link and add the new version link
        /^\[Unreleased\]:/ && !inserted_link {
            print "[Unreleased]: " repo "/compare/v" version "...HEAD"
            if (prev == "0.0.0") {
                print "[" version "]: " repo "/releases/tag/v" version
            } else {
                print "[" version "]: " repo "/compare/v" prev "...v" version
            }
            inserted_link = 1
            next
        }

        { print }

        END {
            if (!inserted_heading) {
                print "ERROR: did not find [Unreleased] heading" > "/dev/stderr"
                exit 1
            }
            if (!inserted_link) {
                print "ERROR: did not find [Unreleased]: reference link" > "/dev/stderr"
                exit 1
            }
        }
        ' CHANGELOG.md > CHANGELOG.md.new

    mv CHANGELOG.md.new CHANGELOG.md
fi

ok "CHANGELOG.md updated"

# ── Step 5: commit the bump ───────────────────────────────────────────────

step "Committing CHANGELOG bump"
run git add CHANGELOG.md
run git commit -m "chore: release v${VERSION}"
COMMIT_MADE=true
ok "commit created"

# ── Step 6: build release binaries ────────────────────────────────────────

step "Building release binaries"
run make release VERSION="$VERSION"

if $DRY_RUN; then
    info "DRY RUN: would generate SHA256SUMS"
else
    (cd dist && shasum -a 256 seed-hunter-"$VERSION"-* > SHA256SUMS)
fi
ok "binaries built"

# ── Step 7: tag ───────────────────────────────────────────────────────────

step "Tagging $TAG"
run git tag -a "$TAG" -m "$TAG"
TAG_MADE=true
ok "tag created"

# ── Step 8: push commit and tag ───────────────────────────────────────────

step "Pushing commit and tag to origin"
run git push origin main
PUSHED_COMMIT=true
run git push origin "$TAG"
PUSHED_TAG=true
ok "pushed"

# ── Step 9: extract release notes from CHANGELOG.md ───────────────────────

step "Extracting release notes for $TAG"

if $DRY_RUN; then
    info "DRY RUN: would extract [$VERSION] section from CHANGELOG.md"
else
    mkdir -p dist
    awk \
        -v version="$VERSION" '
        $0 ~ "^## \\[" version "\\] - " { capture = 1; next }
        /^## \[/ && capture { exit }
        capture { print }
        ' CHANGELOG.md > dist/release-notes.md

    if [[ ! -s dist/release-notes.md ]]; then
        err "extracted release notes are empty — check CHANGELOG.md"
        exit 1
    fi
fi
ok "notes extracted"

# ── Step 10: create the GitHub release ────────────────────────────────────

step "Creating GitHub release $TAG"
if $DRY_RUN; then
    info "DRY RUN: would create GH release with these assets:"
    info "  - dist/seed-hunter-${VERSION}-darwin-arm64"
    info "  - dist/seed-hunter-${VERSION}-darwin-amd64"
    info "  - dist/seed-hunter-${VERSION}-linux-amd64"
    info "  - dist/seed-hunter-${VERSION}-linux-arm64"
    info "  - dist/SHA256SUMS"
else
    gh release create "$TAG" \
        --title "$TAG" \
        --notes-file dist/release-notes.md \
        dist/seed-hunter-"$VERSION"-darwin-arm64 \
        dist/seed-hunter-"$VERSION"-darwin-amd64 \
        dist/seed-hunter-"$VERSION"-linux-amd64 \
        dist/seed-hunter-"$VERSION"-linux-arm64 \
        dist/SHA256SUMS
fi
ok "release published"

# ── Step 11: cleanup ──────────────────────────────────────────────────────

step "Cleaning up"
run make clean
ok "cleaned"

# ── Done ──────────────────────────────────────────────────────────────────

echo
ok "${BOLD}seed-hunter $TAG released successfully${RESET}"
echo
if ! $DRY_RUN; then
    info "view it at: ${REPO_URL}/releases/tag/${TAG}"
fi

# Disarm the trap so a successful exit doesn't trip the rollback.
trap - EXIT
