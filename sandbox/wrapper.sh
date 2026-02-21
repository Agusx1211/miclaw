#!/bin/bash
ALLOWED_COMMANDS="docker|git|systemctl|journalctl"
CMD=$(echo "$SSH_ORIGINAL_COMMAND" | awk '{print $1}')
if echo "$CMD" | grep -qE "^($ALLOWED_COMMANDS)$"; then
    eval "$SSH_ORIGINAL_COMMAND"
else
    echo "Command not allowed: $CMD" >&2
    exit 1
fi
