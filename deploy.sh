#!/usr/bin/env -S bash -euET -o pipefail -O inherit_errexit
# https://unix.stackexchange.com/questions/23026/how-can-i-get-bash-to-exit-on-backtick-failure-in-a-similar-way-to-pipefail

set -x

printUsage() {
    echo "Usage: $0 \
    [--real-run] \
    [-h/--help] \
  " | tr -s " "
    exit 1
}

dryRun=YES
help=

while [[ $# -gt 0 ]]; do
    key="$1"

    case $key in
    --real-run)
        dryRun=NO
        shift # past argument
        ;;
    -h | --help)
        help=YES
        shift # past argument
        ;;
    *) # unknown option
        # POSITIONAL+=("$1") # save it in an array for later
        # shift # past argument
        help=YES
        shift # past argument
        ;;
    esac
done

[[ $help == YES ]] && printUsage

mode=real
[[ $dryRun == YES ]] && mode=dry
echo "In ${mode} run mode..."

source ../../_helper.sh

kubectlDryRun=
if [[ $dryRun == YES ]]; then
    kubectlDryRun=--dry-run=client
fi

tag=$(cat .tag)

declare -A acrMap=([dev]=xoxodev [int]=xoxoint [prod]=xoxoprod)
sed "s|XOXO_ACR|${acrMap[$env]}|; s|XOXO_RELEASE|app-gateway-ingress|; s|XOXO_NAMESPACE|application-gateway-kubernetes-ingress|; s|XOXO_POOL|${ctx}|; s|XOXO_TAG|${tag}|;" route-table-syncer.yml | kubectl apply ${kubectlDryRun} -f -
kubectl rollout status -n application-gateway-kubernetes-ingress deploy route-table-syncer
