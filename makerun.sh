#!/bin/bash

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
echo ""
./bin/joe
