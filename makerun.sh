#!/bin/bash

# 2019 © Anatoly Stansler anatoly@postgres.ai
# 2019 © Dmitry Udalov dmius@postgres.ai
# 2019 © Postgres.ai

go get github.com/aws/aws-sdk-go/aws
go get github.com/aws/aws-sdk-go/aws/awserr
go get github.com/aws/aws-sdk-go/aws/session
go get github.com/aws/aws-sdk-go/service/ec2
go get github.com/docker/machine/libmachine/mcnutils
go get github.com/docker/machine/libmachine/ssh
go get github.com/jessevdk/go-flags
go get gopkg.in/yaml.v2
go get github.com/nlopes/slack
go get github.com/lib/pq
go get github.com/tkanos/gonfig

make all

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

./bin/joe
