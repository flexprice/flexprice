#!/usr/bin/env bash
#
# Builds the CLI and puts a `flexprice` alias on it, so you can try the real
# binary without installing it.
#
#   source cli/scripts/dev.sh          # sandboxed home (default)
#   source cli/scripts/dev.sh --real   # your actual ~/.flexprice and keychain
#
# The sandbox is the default deliberately. `login`, `whoami` and `init` reach
# the real OS keychain regardless of which API they point at, and an unattended
# run can raise a blocking macOS "Keychain Not Found" dialog whose default
# button is destructive. The sandbox sets FLEXPRICE_KEY_BACKEND=file and a
# throwaway HOME, so nothing you do here can touch your real credentials.

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    echo "This script must be sourced, not executed — an alias cannot outlive a subshell:"
    echo "    source cli/scripts/dev.sh"
    exit 1
fi

_fp_dev() {
    local script_dir cli_dir mode="sandbox"

    [ "$1" = "--real" ] && mode="real"

    script_dir="$(cd "$(dirname "${BASH_SOURCE[1]}")" && pwd)"
    cli_dir="$(dirname "$script_dir")"

    echo "Building…"
    if ! (cd "$cli_dir" && go build -o bin/flexprice .); then
        echo "✗ build failed"
        return 1
    fi

    if [ "$mode" = "sandbox" ]; then
        # Reuse one sandbox per shell so profiles survive between commands;
        # a fresh mktemp each call would silently discard every login.
        if [ -z "$FLEXPRICE_DEV_HOME" ] || [ ! -d "$FLEXPRICE_DEV_HOME" ]; then
            FLEXPRICE_DEV_HOME="$(mktemp -d)"
            export FLEXPRICE_DEV_HOME
        fi
        export FLEXPRICE_KEY_BACKEND=file
        alias flexprice="HOME='$FLEXPRICE_DEV_HOME' FLEXPRICE_KEY_BACKEND=file '$cli_dir/bin/flexprice'"
    else
        unset FLEXPRICE_DEV_HOME
        alias flexprice="'$cli_dir/bin/flexprice'"
    fi

    echo "✓ flexprice → $cli_dir/bin/flexprice"
    if [ "$mode" = "sandbox" ]; then
        echo "  mode: sandbox — HOME=$FLEXPRICE_DEV_HOME, encrypted-file keyring"
        echo "        your real ~/.flexprice and OS keychain are untouched"
        echo "        re-source with --real to use them instead"
    else
        echo "  mode: REAL — uses your ~/.flexprice and OS keychain"
    fi
    echo
    echo "  flexprice --help          grouped command reference"
    echo "  flexprice init            guided setup"
    echo "  unalias flexprice         undo"
}

_fp_dev "$@"
unset -f _fp_dev
