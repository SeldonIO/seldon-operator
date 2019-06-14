#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

STARTUP_DIR="$( cd "$( dirname "$0" )" && pwd )"

if [ -z "${GOPATH:-}" ]; then
    export GOPATH=$(go env GOPATH)
fi

CODEGEN_PKG=${STARTUP_DIR}/../vendor/k8s.io/code-generator

echo ${CODEGEN_PKG}

${CODEGEN_PKG}/generate-groups.sh all "github.com/seldonio/seldon-operator/pkg/client" "github.com/seldonio/seldon-operator/pkg/apis" machinelearning:v1alpha2
