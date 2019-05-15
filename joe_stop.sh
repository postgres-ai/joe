#!/bin/bash

AWS_INSTANCE="i3.2xlarge" # 8cpu  61 GiB 1900GiB nvme
#AWS_INSTANCE="m4.4xlarge" # 16 cpu  64 GiB
AWS_INSTANCE_NAME="${AWS_INSTANCE/\./_}"

#  --debug \

$(pwd)/joe.sh \
  --aws-keypair-name anatoly \
  --aws-ssh-key-path ~/.ssh/anatoly.pem \
  --aws-ec2-type "$AWS_INSTANCE" \
  --aws-block-duration 360 \
  --aws-zone "a" \
  --pg-version 9.6 \
  --stop-session \
  --db-ebs-volume-id "vol-08ae661f3be3e4b46" 2>&1 | tee joe-start.log
