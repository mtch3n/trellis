#!/bin/bash
# Compatibility entrypoint for installations that called this script directly.
exec python3 "$(dirname "$0")/trellis_hook.py" stop
