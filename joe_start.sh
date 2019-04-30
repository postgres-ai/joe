#!/bin/bash

AWS_INSTANCE="i3.2xlarge" # 8cpu  61 GiB 1900GiB nvme
#AWS_INSTANCE="m4.4xlarge" # 16 cpu  64 GiB
AWS_INSTANCE_NAME="${AWS_INSTANCE/\./_}"

#  --debug \

$(pwd)/joe.sh \
  --aws-keypair-name xxx \
  --aws-ssh-key-path file:///xxx \
  --aws-ec2-type "$AWS_INSTANCE" \
  --aws-zone "a" \
  --pg-version 9.6 \
  --start-session \
  --db-ebs-volume-id "vol-xxx" 2>&1 | tee joe-start.log

