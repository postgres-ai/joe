#!/bin/bash

# 2019 © Postgres.ai

run() {
  set -x

  go get github.com/aws/aws-sdk-go/aws
  go get github.com/aws/aws-sdk-go/aws/awserr
  go get github.com/aws/aws-sdk-go/aws/session
  go get github.com/aws/aws-sdk-go/service/ec2
  go get github.com/docker/machine/libmachine/mcnutils
  go get github.com/docker/machine/libmachine/ssh
  go get github.com/dustin/go-humanize
  go get github.com/jessevdk/go-flags
  go get github.com/lib/pq
  go get github.com/mitchellh/go-wordwrap
  go get github.com/nlopes/slack
  go get github.com/sergi/go-diff/diffmatchpatch
  go get github.com/tkanos/gonfig
  go get golang.org/x/crypto/ssh/terminal
  go get golang.org/x/net/http2
  go get golang.org/x/sync/errgroup
  go get golang.org/x/tools/go/buildutil
  go get gopkg.in/yaml.v2

  make all

  set +x

  # Instead of editing default values here create a separate config file.
  # e.g. for staging environment create a file /deploy/configs/staging.sh
  # use `export` to define variables and run with `ENV=staging makerun.sh`.
  if [ -z ${ENV+x} ]; then
    echo "Using default variables"

    # Slack API.
    export CHAT_TOKEN="xoxb-TOKEN"
    export CHAT_VERIFICATION_TOKEN="TOKEN"

    # HTTP server for Slack Events.
    export SERVER_PORT=3000

    # DB connection info.
    export DB_HOST="localhost"
    export DB_PORT=10799
    export DB_USER="postgres"
    export DB_PASSWORD="postgres"
    export DB_NAME="postgres"

    # Edit config/provisioning.yaml.
  else
    source ./deploy/configs/${ENV}.sh
  fi

  # Read and set git status info if it wasn't done before.
  if [ -z "$GIT_COMMIT_HASH" ]; then
    echo "Fetching git status..."
    export_git_status
  fi

  set -x

  ./bin/joe
}

export_git_status() {
  DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
  GIT_STATUS_RES=$(cd $DIR && git status --porcelain=v2 -b)
  EXIT_CODE=$?

  if [ $EXIT_CODE != 0 ]; then
    echo "git exit code: ${EXIT_CODE}"
    exit
  fi

  GIT_MODIFIED="false"
  GIT_BRANCH=""
  GIT_COMMIT_HASH=""
  while read -r line; do
    COLUMNS=($line)
    if [ ${#COLUMNS[@]} > 3 ]; then
      if [ ${COLUMNS[1]} == "branch.oid" ]; then
        GIT_COMMIT_HASH=${COLUMNS[2]}
        continue
      elif [ ${COLUMNS[1]} == "branch.head" ]; then
        GIT_BRANCH=${COLUMNS[2]}
        continue
      fi
    fi

    GIT_MODIFIED="true"
  done <<< "$GIT_STATUS_RES"

  export GIT_MODIFIED
  export GIT_BRANCH
  export GIT_COMMIT_HASH
}

is_command_defined() {
    type $1 2>/dev/null | grep -q 'is a function'
}

# Parse command and arguments.
COMMAND=$1
shift
ARGUMENTS=${@}

# Run command.
is_command_defined $COMMAND
if [ $? -eq 0 ]; then
  $COMMAND $ARGUMENTS
else
  echo "Command not found"
fi
