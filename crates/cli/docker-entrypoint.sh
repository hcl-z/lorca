#!/bin/sh
# The first start of a Runner container: a Device needs an identity before `lorca serve` syncs,
# and a container is not somewhere to run `lorca identity new` by hand. Every later start skips
# this and goes straight to the server.
set -eu

if [ ! -f "$LORCA_HOME/machine.json" ]; then
    name=${LORCA_DEVICE_NAME:-$(hostname)}

    if [ -n "${LORCA_PAIR_STRING:-}" ]; then
        echo "Joining the identity in LORCA_PAIR_STRING as $name."
        lorca pair "$LORCA_PAIR_STRING" --name "$name"
    else
        echo "No LORCA_PAIR_STRING: creating an identity on this Device as $name."
        out=$(lorca identity new --name "$name")
        # The phrase is the identity; a lost one cannot be recovered. Kept on the volume beside
        # the keys, and printed once so it also reaches the container's log.
        umask 077
        printf '%s\n' "$out" > "$LORCA_HOME/backup-phrase.txt"
        printf '%s\n' "$out"
        echo
        echo "The backup phrase is also in $LORCA_HOME/backup-phrase.txt. Write it down."
    fi
fi

exec lorca "$@"
