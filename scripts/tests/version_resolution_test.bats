#!/usr/bin/env bats

# Tests for resolve_version: --version override, API success, fallback.

setup() {
    # Override error() so it doesn't exit during tests
    error() { echo "ERROR: $1"; return 1; }
    export -f error

    source scripts/install.sh

    # Silence info/warn output so assertions on $output are cleaner
    info() { :; }
    warn() { :; }
    export -f info warn
}

@test "resolve_version: --version=X.Y.Z override wins over API" {
    VERSION_OVERRIDE="1.2.3"
    # curl would succeed but must not be consulted
    curl() { echo '{"tag_name":"v9.9.9"}'; return 0; }
    export -f curl

    resolve_version

    [ "$VERSION" = "1.2.3" ]
    [ "$RELEASE_URL" = "https://github.com/dbinky/ralph-o-matic/releases/download/v1.2.3" ]
}

@test "resolve_version: --version strips leading v" {
    VERSION_OVERRIDE="v2.0.0"

    resolve_version

    [ "$VERSION" = "2.0.0" ]
}

@test "resolve_version: uses latest tag from GitHub API" {
    VERSION_OVERRIDE=""
    curl() { echo '{"tag_name":"v0.8.1","name":"Release 0.8.1"}'; return 0; }
    export -f curl

    resolve_version

    [ "$VERSION" = "0.8.1" ]
    [ "$RELEASE_URL" = "https://github.com/dbinky/ralph-o-matic/releases/download/v0.8.1" ]
}

@test "resolve_version: falls back when curl fails" {
    VERSION_OVERRIDE=""
    VERSION_FALLBACK="0.7.0"
    curl() { return 1; }
    export -f curl

    resolve_version

    [ "$VERSION" = "0.7.0" ]
    [ "$RELEASE_URL" = "https://github.com/dbinky/ralph-o-matic/releases/download/v0.7.0" ]
}

@test "resolve_version: falls back when API returns malformed JSON" {
    VERSION_OVERRIDE=""
    VERSION_FALLBACK="0.7.0"
    curl() { echo 'not json at all'; return 0; }
    export -f curl

    resolve_version

    [ "$VERSION" = "0.7.0" ]
}

@test "resolve_version: falls back when tag_name is not semver" {
    VERSION_OVERRIDE=""
    VERSION_FALLBACK="0.7.0"
    curl() { echo '{"tag_name":"nightly-build"}'; return 0; }
    export -f curl

    resolve_version

    [ "$VERSION" = "0.7.0" ]
}

@test "parse_args: --version=X.Y.Z sets VERSION_OVERRIDE" {
    VERSION_OVERRIDE=""

    parse_args --version=1.2.3

    [ "$VERSION_OVERRIDE" = "1.2.3" ]
}
