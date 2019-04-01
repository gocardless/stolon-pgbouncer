#!/usr/bin/env bash

set -eu -o pipefail

if hostname | grep keeper0; then
	if (stolonctl clusterdata 2>&1 || /bin/true) | grep 'nil cluster data: <nil>'; then
		yes yes | stolonctl init -f /stolon-pgbouncer/docker/stolon-node/stolon/specification.json
	fi
fi
