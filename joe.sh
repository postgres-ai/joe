#!/bin/bash
#
VERBOSE_OUTPUT_REDIRECT=" > /dev/null 2>&1"
STDERR_DST="/dev/null"
INSTACE_KEEP_ALIVE=3600000 # 1000 hours
JOE_CUR_DIR=$(pwd)/joe-run
DEBUG=true
NO_OUTPUT=false
CURRENT_TS=$(date +%Y%m%d_%H%M%S%N_%Z)
DOCKER_MACHINE=""
CONTAINER_ID=""
START_SESSION=0
STOP_SESSION=0
LOCAL_SSH_TUNNEL_PORT=10799

mkdir -p $JOE_CUR_DIR

#######################################
# Print an message to STDOUT
# Globals:
#   None
# Arguments:
#   (text) Message
# Returns:
#   None
#######################################
function msg() {
  if ! $NO_OUTPUT; then
    echo "[$(date +'%Y-%m-%dT%H:%M:%S%z')] $@"
  fi
}

#######################################
# Print an message to STDOUT without timestamp
# Globals:
#   None
# Arguments:
#   (text) Message
# Returns:
#   None
#######################################
function msg_wo_dt() {
  if ! $NO_OUTPUT; then
    echo "$@"
  fi
}

#######################################
# Print an error/warning/notice message to STDERR
# Globals:
#   None
# Arguments:
#   (text) Error message
# Returns:
#   None
#######################################
function err() {
  if ! $NO_OUTPUT; then
    echo "[$(date +'%Y-%m-%dT%H:%M:%S%z')] $@" >&2
  fi
}

#######################################
# Print a debug-level message to STDOUT
# Globals:
#   DEBUG
# Arguments:
#   (text) Message
# Returns:
#   None
#######################################
function dbg() {
  if $DEBUG ; then
    msg "DEBUG: $@"
  fi
}

# get params
while [ $# -gt 0 ]; do
  case "$1" in
    -d | --debug )
      DEBUG=true
      VERBOSE_OUTPUT_REDIRECT=''
      STDERR_DST='/dev/stderr'
      shift ;;
    --pg-version )
      PG_VERSION="$2"; shift 2 ;;
    --aws-ec2-type )
      AWS_EC2_TYPE="$2"; shift 2 ;;
    --aws-keypair-name )
      AWS_KEYPAIR_NAME="$2"; shift 2 ;;
    --aws-ssh-key-path )
      AWS_SSH_KEY_PATH="$2"; shift 2 ;;
    --aws-region )
      AWS_REGION="$2"; shift 2 ;;
    --aws-zone )
      AWS_ZONE="$2"; shift 2 ;;
    --aws-block-duration )
      AWS_BLOCK_DURATION=$2; shift 2 ;;
    --db-ebs-volume-id )  # instace live time
      DB_EBS_VOLUME_ID=$2; shift 2;;
      
    --start-session )
      START_SESSION=1; shift 1 ;;
    --stop-session )
      STOP_SESSION=1; shift 1 ;;

    --db-name )
      DB_NAME="$2"; shift 2 ;;
    --less-output )
      DEBUG=false
      NO_OUTPUT=false
      VERBOSE_OUTPUT_REDIRECT=" > /dev/null 2>&1"
      shift ;;

    --help )
      echo "Joe supports: "
      echo "  -d | --debug"
      echo "  --pg-version"
      echo "  --aws-ec2-type"
      echo "  --aws-keypair-name"
      echo "  --aws-ssh-key-path"
      echo "  --pg-version"
      echo "  --aws-region"
      echo "  --aws-zone"
      echo "  --aws-block-duration"
      echo "  --db-ebs-volume-id"
      echo "  --start-session"
      echo "  --stop-session"
      shift;;

    * )
      option=$1
      option="${option##*( )}"
      option="${option%%*( )}"
      if [[ "${option:0:2}" == "--" ]]; then
        err "ERROR: Invalid option '$1'. Please double-check options."
        exit 1
      elif [[ "$option" != "" ]]; then
        err "ERROR: \"joe\" does not support payload (except \"--help\"). Use options, see \"joe --help\")"
        exit 1
      fi
    break ;;
  esac
done

#######################################
# Check path to file/directory.
# Globals:
#   None
# Arguments:
#   (text) name of the variable holding the
#          file path (starts with 'file://' or 's3://') or any string
# Returns:
#   (integer) for input starting with 's3://' always returns 0
#             for 'file://': 0 if file exists locally, error if it doesn't
#             1 if the input is empty,
#             -1 otherwise.
#######################################
function check_path() {
  if [[ -z $1 ]]; then
    return 1
  fi
  eval path=\$$1
  if [[ $path =~ "s3://" ]]; then
    dbg "$1 looks like a S3 file path. Warning: Its presence will not be checked!"
    return 0 # we do not actually check S3 paths at the moment
  elif [[ $path =~ "file://" ]]; then
    dbg "$1 looks like a local file path."
    path=${path/file:\/\//}
    if [[ -f $path ]]; then
      dbg "$path found."
      eval "$1=\"$path\"" # update original variable
      return 0 # file found
    else
      err "ERROR: File '$path' has not been found locally."
      exit 1
    fi
  else
    dbg "Value of $1 is not a file path. Use its value as a content."
    return 255
  fi
}

#######################################
# Validate CLI parameters
# Globals:
#   Variables related to all CLI parameters
# Arguments:
#   None
# Returns:
#   None
#######################################
function check_cli_parameters() {
  ### Check path|value variables for empty value ###
  ([[ ! -z ${AWS_ZONE+x} ]] && [[ -z $AWS_ZONE ]]) && unset -v AWS_ZONE
  ### CLI parameters checks ###
  if [[ -z ${AWS_KEYPAIR_NAME+x} ]] || [[ -z ${AWS_SSH_KEY_PATH+x} ]]; then
    err "ERROR: AWS keypair name and SSH key file must be specified to run on AWS EC2."
    exit 1
  else
    check_path AWS_SSH_KEY_PATH
  fi
  if [[ -z ${AWS_EC2_TYPE+x} ]]; then
    err "ERROR: AWS EC2 Instance type is not specified."
    exit 1
  fi
  if [[ -z ${AWS_REGION+x} ]]; then
    msg "NOTICE: AWS EC2 region is not specified. 'us-east-1' will be used."
    AWS_REGION='us-east-1'
  fi
  if [[ -z ${AWS_ZONE+x} ]]; then
    err "NOTICE: AWS EC2 zone is not specified. Will be determined during the price optimization process."
  fi
  if [[ -z ${AWS_BLOCK_DURATION+x} ]]; then
    # See https://aws.amazon.com/en/blogs/aws/new-ec2-spot-blocks-for-defined-duration-workloads/
    msg "NOTICE: EC2 spot block duration is not specified. Will use 60 minutes."
    AWS_BLOCK_DURATION=60
  else
    case $AWS_BLOCK_DURATION in
      0|60|120|240|300|360)
        dbg "Container life time duration is $AWS_BLOCK_DURATION."
      ;;
      *)
        msg "ERROR: The value of '--aws-block-duration' is invalid: $AWS_BLOCK_DURATION. Choose one of the following: 60, 120, 180, 240, 300, or 360."
        exit 1
      ;;
    esac
  fi
  if [[ -z ${PG_VERSION+x} ]]; then
    msg "NOTICE: The Postgres version is not specified. The default will be used: ${POSTGRES_VERSION_DEFAULT}."
    PG_VERSION="$POSTGRES_VERSION_DEFAULT"
  fi
  if [[ "$PG_VERSION" = "9.6" ]]; then
    CURRENT_LSN_FUNCTION="pg_current_xlog_location()"
  else
    CURRENT_LSN_FUNCTION="pg_current_wal_lsn()"
  fi
  if [[ -z ${DB_EBS_VOLUME_ID+x} ]]; then
    err "ERROR: The object (database) is not defined."
    exit 1
  fi
  if [[ -z ${DB_NAME+x} ]]; then
    dbg "NOTICE: Database name is not given. Will use 'test'"
    DB_NAME='test'
  fi
}

#######################################
# Create Docker machine using an AWS EC2 spot instance
# See also: https://docs.docker.com/machine/reference/create/
# Globals:
#   None
# Arguments:
#   (text) [1] Machine name
#   (text) [2] EC2 Instance type
#   (text) [3] Spot instance bid price (in dollars)
#   (int)  [4] AWS spot instance duration in minutes (60, 120, 180, 240, 300,
#              or 360)
#   (text) [5] AWS keypair to use
#   (text) [6] Path to Private Key file to use for instance
#              Matching public key with .pub extension should exist
#   (text) [7] The AWS region to launch the instance
#              (for example us-east-1, eu-central-1)
#   (text) [8] The AWS zone to launch the instance in (one of a,b,c,d,e)
# Returns:
#   None
#######################################
function create_ec2_docker_machine() {
  msg "Attempting to provision a Docker machine in region $7 with price $3..."
  docker-machine create --driver=amazonec2 \
    --amazonec2-request-spot-instance \
    --amazonec2-instance-type=$2 \
    --amazonec2-spot-price=$3 \
    --amazonec2-block-duration-minutes=$4 \
    --amazonec2-keypair-name="$5" \
    --amazonec2-ssh-keypath="$6" \
    --amazonec2-region="$7" \
    --amazonec2-zone="$8" \
    $1 2> >(grep -v "failed waiting for successful resource state" >&2) &
}

#######################################
# Order to destroy Docker machine (any platform)
# See also: https://docs.docker.com/machine/reference/rm/
# Globals:
#   None
# Arguments:
#   (text) Machine name
# Returns:
#   None
#######################################
function destroy_docker_machine() {
  # If spot request wasn't fulfilled, there is no associated instance,
  # so "docker-machine rm" will show an error, which is safe to ignore.
  # We better filter it out to avoid any confusions.
  # What is used here is called "process substitution",
  # see https://www.gnu.org/software/bash/manual/bash.html#Process-Substitution
  # The same trick is used in create_ec2_docker_machine() to filter out errors
  # when we have "price-too-low" attempts, such errors come in few minutes
  # after an attempt and are generally unexpected by user.
  cmdout=$(docker-machine rm --force $1 2> >(grep -v "unknown instance" >&2) )
  msg "Termination requested for machine, current status: $cmdout"
}

#######################################
# Wait until EC2 instance with Docker maching is up and running
# Globals:
#   None
# Arguments:
#   (text) Machine name
# Returns:
#   None
#######################################
function wait_ec2_docker_machine_ready() {
  local machine=$1
  local check_price=$2
  while true; do
    sleep 5
    local stop_now=1
    ps ax | grep "docker-machine create" | grep "$machine" >/dev/null && stop_now=0
    ((stop_now==1)) && return 0
    if $check_price ; then
      status=$( \
        aws --region=$AWS_REGION ec2 describe-spot-instance-requests \
          --filters="Name=launch.instance-type,Values=$AWS_EC2_TYPE" \
        | jq  '.SpotInstanceRequests | sort_by(.CreateTime) | .[] | .Status.Code' \
        | tail -n 1
      )
      if [[ "$status" == "\"price-too-low\"" ]]; then
        echo "price-too-low"; # this value is result of function (not message for user), to be checked later
        return 0
      fi
    fi
  done
}

#######################################
# Determine EC2 spot price from history with multiplier
# Globals:
#   AWS_REGION, AWS_EC2_TYPE, EC2_PRICE
# Arguments:
#   None
# Returns:
#   None
# Result:
#   Fill AWS_ZONE and EC2_PRICE variables, update AWS_REGION.
#######################################
function determine_history_ec2_spot_price() {
  ## Get max price from history and apply multiplier
  # TODO detect region and/or allow to choose via options
  prices=$(
    aws --region=$AWS_REGION ec2 \
      describe-spot-price-history --instance-types $AWS_EC2_TYPE --no-paginate \
      --start-time=$(date +%s) --product-descriptions="Linux/UNIX (Amazon VPC)" \
      --query 'SpotPriceHistory[*].{az:AvailabilityZone, price:SpotPrice}'
  )
  if [[ ! -z ${AWS_ZONE+x} ]]; then
    # zone given by option
    price_data=$(echo $prices | jq ".[] | select(.az == \"$AWS_REGION$AWS_ZONE\")")
  else
    # zone NOT given by options, will detected from min price
    price_data=$(echo $prices | jq 'min_by(.price)')
  fi
  region=$(echo $price_data | jq '.az')
  price=$(echo $price_data | jq '.price')
  #region=$(echo $price_data | jq 'min_by(.price) | .az') #TODO(NikolayS) double-check zones&regions
  region="${region/\"/}"
  region="${region/\"/}"
  price="${price/\"/}"
  price="${price/\"/}"
  AWS_ZONE=${region:$((${#region}-1)):1}
  AWS_REGION=${region:0:$((${#region}-1))}
  msg "Min price from history: ${price}/h in $AWS_REGION (zone: $AWS_ZONE)."
  multiplier="1.01"
  price=$(echo "$price * $multiplier" | bc -l)
  msg "Increased price: ${price}/h"
  EC2_PRICE=$price
}

#######################################
# Determine actual EC2 spot price from aws error message
# Globals:
#   AWS_REGION, AWS_EC2_TYPE, EC2_PRICE
# Arguments:
#   None
# Returns:
#   None
# Result:
#   Update EC2_PRICE variable or stop script if price do not determined
#######################################
function determine_actual_ec2_spot_price() {
  aws --region=$AWS_REGION ec2 describe-spot-instance-requests \
    --filters 'Name=status-code,Values=price-too-low' \
  | grep SpotInstanceRequestId | awk '{gsub(/[,"]/, "", $2); print $2}' \
  | xargs aws --region=$AWS_REGION ec2 cancel-spot-instance-requests \
    --spot-instance-request-ids || true
  corrrectPriceForLastFailedRequest=$( \
    aws --region=$AWS_REGION ec2 describe-spot-instance-requests \
      --filters="Name=launch.instance-type,Values=$AWS_EC2_TYPE" \
    | jq  '.SpotInstanceRequests[] | select(.Status.Code == "price-too-low") | .Status.Message' \
    | grep -Eo '[0-9]+[.][0-9]+' | tail -n 1 &
  )
  if [[ ("$corrrectPriceForLastFailedRequest" != "")  &&  ("$corrrectPriceForLastFailedRequest" != "null") ]]; then
    EC2_PRICE=$corrrectPriceForLastFailedRequest
  else
    err "ERROR: Cannot determine actual price for the instance $AWS_EC2_TYPE."
    exit 1
  fi
}

#######################################
# Attach an EBS volume containing the database backup (made with pg_basebackup)
# Globals:
#   DOCKER_MACHINE, AWS_REGION, DB_EBS_VOLUME_ID
# Arguments:
#   None
# Returns:
#   None
#######################################
function attach_db_ebs_drive() {
  docker-machine ssh $DOCKER_MACHINE "sudo apt-get install -y zfsutils-linux"
  docker-machine ssh $DOCKER_MACHINE "sudo sh -c \"mkdir /home/storage\""
  docker-machine ssh $DOCKER_MACHINE "wget http://s3.amazonaws.com/ec2metadata/ec2-metadata"
  docker-machine ssh $DOCKER_MACHINE "chmod u+x ec2-metadata"
  local instance_id=$(docker-machine ssh $DOCKER_MACHINE ./ec2-metadata -i)
  instance_id=${instance_id:13}
  local attach_result=$(aws --region=$AWS_REGION ec2 attach-volume \
    --device /dev/xvdc --volume-id $DB_EBS_VOLUME_ID --instance-id $instance_id)
  sleep 10
  docker-machine ssh $DOCKER_MACHINE sudo zpool import -R / zpool
  dbg $(docker-machine ssh $DOCKER_MACHINE "sudo df -h /home/storage")
  
  # Set ARC size as 30% of RAM
  # get MemTotal (kB)
  local memtotal_kb=$(docker-machine ssh $DOCKER_MACHINE "grep MemTotal /proc/meminfo | awk '{print \$2}'")
  # Calculate recommended ARC size in bytes.
  local arc_size_b=$(( memtotal_kb / 100 * 30 * 1024))
  # If the calculated ARC is less than 1 GiB, then set it to 1 GiB.
  if [[ "${arc_size_b}" -lt "1073741824" ]]; then
    arc_size_b="1073741824" # 1 GiB
  fi
  # finally, change ARC MAX
  docker-machine ssh $DOCKER_MACHINE "echo ${arc_size_b} | sudo tee /sys/module/zfs/parameters/zfs_arc_max"
  docker-machine ssh $DOCKER_MACHINE "sudo cat /sys/module/zfs/parameters/zfs_arc_max"
  msg "ARC MAX has been set to ${arc_size_b} bytes."
}

#######################################
# Print "How to connect" instructions
# Globals:
#   DOCKER_MACHINE, CURRENT_TS, RUN_ON
# Arguments:
#   None
# Returns:
#   None
#######################################
function print_connection {
  msg_wo_dt ""
  msg_wo_dt "  =========================================================="
  msg_wo_dt "  How to connect to the Docker machine:"
  msg_wo_dt "    docker-machine ssh ${DOCKER_MACHINE}"
  msg_wo_dt "  How to connect directly to the container:"
  msg_wo_dt "    docker \`docker-machine config ${DOCKER_MACHINE}\` exec -it ${CONTAINER_ID} bash"
  msg_wo_dt "  =========================================================="
  msg_wo_dt ""
}

check_cli_parameters

START_TIME=$(date +%s); #save start time

if $DEBUG ; then
  set -xueo pipefail
else
  set -ueo pipefail
fi
shopt -s expand_aliases

#trap cleanup_and_exit 1 2 13 15 EXIT

if ([[ -f "$JOE_CUR_DIR/session.id" ]] && [[ "1" -eq "$START_SESSION" ]]); then
  echo "Joe > Session already started." >&2
  exit 1
fi

if [[ "1" -eq "$STOP_SESSION" ]]; then
  if [[ -f "$JOE_CUR_DIR/session.id" ]]; then
    rm -rf "$JOE_CUR_DIR/session.id"  # remove session id file
    echo "Joe > Session stopped." 
  else 
    echo "Joe > No session." 
  fi
  exit 0
fi

if [[ -f "$JOE_CUR_DIR/instance.id" ]]; then
  DOCKER_MACHINE=$(cat $JOE_CUR_DIR/instance.id)
  CONTAINER_ID="docker-$DOCKER_MACHINE"
  dbg "Instace id: $DOCKER_MACHINE"
fi

INSTANCE_EXISTS=$(docker `docker-machine config $DOCKER_MACHINE` exec -it "$CONTAINER_ID" echo -n 1 || echo 0)

if [[ "1" -eq "$INSTANCE_EXISTS" ]]; then
  dbg "Instance with docker found"
  CONTAINER_HASH=$(docker-machine ssh $DOCKER_MACHINE "sudo docker ps -aqf \"name=${CONTAINER_ID}\"")
  DOCKER_CONFIG=$(docker-machine config $DOCKER_MACHINE)
  if $DEBUG; then
    print_connection
  fi
else 
  rm -rf "$JOE_CUR_DIR/instance.id"
  msg "Started Instace NOT FOUND"
  DOCKER_MACHINE="joe-$CURRENT_TS"
  DOCKER_MACHINE="${DOCKER_MACHINE//_/-}"
  CONTAINER_ID="docker-$DOCKER_MACHINE"
  
  determine_history_ec2_spot_price
  create_ec2_docker_machine $DOCKER_MACHINE $AWS_EC2_TYPE $EC2_PRICE \
    $AWS_BLOCK_DURATION $AWS_KEYPAIR_NAME $AWS_SSH_KEY_PATH $AWS_REGION $AWS_ZONE
  status=$(wait_ec2_docker_machine_ready "$DOCKER_MACHINE" true)
  if [[ "$status" == "price-too-low" ]]; then
    msg "Price $price is too low for $AWS_EC2_TYPE instance. Getting the up-to-date value from the error message..."
    #destroy_docker_machine $DOCKER_MACHINE
    # "docker-machine rm" doesn't work for "price-too-low" spot requests,
    # so we need to clean up them via aws cli interface directly
    determine_actual_ec2_spot_price
    #update docker machine name
    CURRENT_TS=$(date +%Y%m%d_%H%M%S%N_%Z)
    DOCKER_MACHINE="joe-$CURRENT_TS"
    DOCKER_MACHINE="${DOCKER_MACHINE//_/-}"
    CONTAINER_ID="docker-$DOCKER_MACHINE"
    #try start docker machine name with new price
    create_ec2_docker_machine $DOCKER_MACHINE $AWS_EC2_TYPE $EC2_PRICE \
      $AWS_BLOCK_DURATION $AWS_KEYPAIR_NAME $AWS_SSH_KEY_PATH $AWS_REGION $AWS_ZONE
    wait_ec2_docker_machine_ready "$DOCKER_MACHINE" false
  fi

  dbg "Checking the status of the Docker machine..."
  res=$(docker-machine status $DOCKER_MACHINE 2>&1 &)
  if [[ "$res" != "Running" ]]; then
    err "ERROR: Docker machine $DOCKER_MACHINE is NOT running."
    exit 1
  fi

  if [[ ! -z ${DB_EBS_VOLUME_ID+x} ]]; then
    attach_db_ebs_drive
  fi
  
  DOCKER_CONFIG=$(docker-machine config $DOCKER_MACHINE)

  docker $DOCKER_CONFIG pull "postgresmen/postgres-nancy:${PG_VERSION}" 2>&1 \
    | grep -e 'Pulling from' -e Digest -e Status -e Error

  CONTAINER_HASH=$( \
    docker $DOCKER_CONFIG run \
      --name="$CONTAINER_ID" \
      --privileged \
      -p 5432:5432 \
      -v /home/ubuntu:/machine_home \
      -v /home/storage:/storage \
      -dit "postgresmen/postgres-nancy:${PG_VERSION}"
  )  

  print_connection
  echo "$DOCKER_MACHINE" > "$JOE_CUR_DIR/instance.id"
fi
alias docker_exec='docker $DOCKER_CONFIG exec -i ${CONTAINER_HASH} '
CPU_CNT=$(docker_exec bash -c "cat /proc/cpuinfo | grep processor | wc -l")

#######################################
# Stop postgres and wait for complete stop
# Globals:
#   None
# Arguments:
#   None
# Returns:
#   None
#######################################
function stop_postgres {
  dbg "Stopping Postgres..."
  local cnt=0
  while true; do
    res=$(docker_exec bash -c "ps auxww | grep postgres | grep -v "grep" 2>/dev/null || echo ''")
    if [[ -z "$res" ]]; then
      # postgres process not found
      dbg "Postgres stopped."
      return;
    fi
    cnt=$((cnt+1))
    if [[ "${cnt}" -ge "900" ]]; then
      msg "WARNING: could not stop Postgres in 15 minutes. Killing."
      docker_exec bash -c "sudo killall -s 9 postgres || true"
    fi
    # Try normal "fast stop"
    docker_exec bash -c "sudo pg_ctlcluster ${PG_VERSION} main stop -m f ${VERBOSE_OUTPUT_REDIRECT} || true"
    sleep 1
  done
}

#######################################
# Start postgres and wait for ready
# Globals:
#   None
# Arguments:
#   None
# Returns:
#   None
#######################################
function start_postgres {
  dbg "Starting Postgres..."
  local cnt=0
  while true; do
    res=$(docker_exec bash -c "psql -Upostgres -d postgres -t -c \"select 1\" 2>/dev/null || echo '' ")
    if [[ ! -z "$res" ]]; then
      dbg "Postgres started."
      return;
    fi
    cnt=$((cnt+1))
    if [[ "${cnt}" -ge "900" ]]; then
      dbg "WARNING: Can't start Postgres in 15 minutes." >&2
      return 12
    fi
    docker_exec bash -c "sudo pg_ctlcluster ${PG_VERSION} main start ${VERBOSE_OUTPUT_REDIRECT} || true"
    sleep 1
  done
  dbg "Postgres started"
}

#######################################
# Get actual instance ip
# Globals:
#   None
# Arguments:
#   None
# Returns:
#   None
#######################################
function get_actual_instance_ip {
  docker-machine ssh $DOCKER_MACHINE "wget http://s3.amazonaws.com/ec2metadata/ec2-metadata ${VERBOSE_OUTPUT_REDIRECT}"
  docker-machine ssh $DOCKER_MACHINE "chmod u+x ec2-metadata ${VERBOSE_OUTPUT_REDIRECT}"
  local instance_id=$(docker-machine ssh $DOCKER_MACHINE ./ec2-metadata -i)
  instance_id=${instance_id:13}
  instace_ip=$(aws ec2 describe-instances --instance-ids "${instance_id}" --query 'Reservations[*].Instances[*].PublicIpAddress' --output text)
  echo "${instace_ip}" > "$JOE_CUR_DIR/instance.ip"
}

#######################################
# Get instance ip from file or actual if file not found
# Globals:
#   None
# Arguments:
#   None
# Returns:
#   None
#######################################
function get_instance_ip {
  if [[ ! -f "$JOE_CUR_DIR/instance.ip" ]]; then
    get_actual_instance_ip
  else
    instace_ip=$(cat "$JOE_CUR_DIR/instance.ip")
  fi
}

#######################################
# Open SSH tunnel
# Globals:
#   None
# Arguments:
#   None
# Returns:
#   None
#######################################
function start_ssh_tunnel {
  get_instance_ip
  instace_ip=$(cat "$JOE_CUR_DIR/instance.ip")
  ssh -o 'StrictHostKeyChecking no' -i ${AWS_SSH_KEY_PATH} -f -N -M -S ${JOE_CUR_DIR}/sshsock -L ${LOCAL_SSH_TUNNEL_PORT}:localhost:5432 ubuntu@${instace_ip} &
}

#######################################
# Close SSH tunnel
# Globals:
#   None
# Arguments:
#   None
# Returns:
#   None
#######################################
function stop_ssh_tunnel {
  get_instance_ip
  instace_ip=$(cat "$JOE_CUR_DIR/instance.ip")
  ssh -S ${JOE_CUR_DIR}/sshsock -O exit ubuntu@${instace_ip} &
}

#######################################
# Check existance of SSH tunnel and reopen if need
# Globals:
#   None
# Arguments:
#   None
# Returns:
#   None
#######################################
function check_ssh_tunnel {
  res=$(bash -c "PGPASSWORD=testuser psql -t -q -h localhost -p ${LOCAL_SSH_TUNNEL_PORT} --user=testuser postgres -c \"select '1';\"" || echo '0')
  res="$(echo -e "${res}" | tr -d '[:space:]')"
  if [[ "1" -ne "$res" ]]; then
    dbg "Joe > Refresh SSH tunnel"
    stop_ssh_tunnel
    start_ssh_tunnel
  fi
}

#######################################
# Create test user on database
# Globals:
#   None
# Arguments:
#   None
# Returns:
#   None
#######################################
function create_testuser {
    (docker_exec psql -U postgres $DB_NAME -f - <<EOF
    do
    \$do\$
    begin
       if not exists (select 1 from pg_catalog.pg_roles where rolname = 'testuser') then
          CREATE ROLE testuser LOGIN password 'testuser' superuser;
       end if;
    end
    \$do\$;
EOF
) > /dev/null
}

if [[ "1" -ne "$INSTANCE_EXISTS" ]]; then
  stop_postgres
  docker_exec bash -c "sudo mv /var/lib/postgresql /var/lib/postgresql_original"
  docker_exec bash -c "ln -s /storage/postgresql /var/lib/postgresql"
  start_postgres
  create_testuser
  get_actual_instance_ip
  start_ssh_tunnel
  echo "Joe > Postgres instance launched, SSH tunnel ready (port: $LOCAL_SSH_TUNNEL_PORT)"
fi

#######################################
# Do rollback to earlier created ZFS snapshot with stop and start postgres
# Globals:
#   None
# Arguments:
#   None
# Returns:
#   None
#######################################
function zfs_rollback_snapshot {
  OP_START_TIME=$(date +%s)
  dbg "Rollback database"
  stop_postgres
  docker_exec bash -c "zfs rollback -f -r zpool@init_db $VERBOSE_OUTPUT_REDIRECT"
  start_postgres
  END_TIME=$(date +%s)
  DURATION=$(echo $((END_TIME-OP_START_TIME)) | awk '{printf "%d:%02d:%02d", $1/3600, ($1/60)%60, $1%60}')
  msg "Time taken to rollback database: $DURATION."
}

#######################################
# Create ZFS snapshot with stop and start postgres
# Globals:
#   None
# Arguments:
#   None
# Returns:
#   None
#######################################
function zfs_create_snapshot {
  OP_START_TIME=$(date +%s)
  dbg "Create database snapshot"
  stop_postgres
  docker_exec bash -c "zfs snapshot -r zpool@init_db  $VERBOSE_OUTPUT_REDIRECT"
  start_postgres
  END_TIME=$(date +%s)
  DURATION=$(echo $((END_TIME-OP_START_TIME)) | awk '{printf "%d:%02d:%02d", $1/3600, ($1/60)%60, $1%60}')
  msg "Time taken to create database snapshot: $DURATION."
}

#######################################
# Output instace params
# Globals:
#   None
# Arguments:
#   None
# Returns:
#   None
#######################################
function get_instance_params {
  get_instance_ip
  instace_ip=$(cat "$JOE_CUR_DIR/instance.ip")
  echo "Joe > EC2 instance ip: ${instace_ip}"
  echo "Joe > To connect: ssh -o 'StrictHostKeyChecking no' -i ${AWS_SSH_KEY_PATH} ubuntu@${instace_ip}"
  echo "Joe > To start SSH tunnel *(started): ssh -o 'StrictHostKeyChecking no' -i ${AWS_SSH_KEY_PATH} -f -N -M -S ${JOE_CUR_DIR}/sshsock -L 10799:localhost:5432 ubuntu@${instace_ip}"
  echo "Joe > To stop SSH tunnel: ssh -S ${JOE_CUR_DIR}/sshsock -O exit ubuntu@${instace_ip}"
  echo "Joe > To connect to db (password: testuser): PGPASSWORD=testuser psql -h localhost -p 10799 -U testuser %DB_NAME%"
}

if [[ ! -f "$JOE_CUR_DIR/session.id" ]] && [[ 1 -eq "$START_SESSION" ]]; then
    # prepare all for experiments
    SESSION_ID=$(date +%s)
    echo "$SESSION_ID" > $JOE_CUR_DIR/session.id
    zfs_rollback_snapshot
    create_testuser
    check_ssh_tunnel
    echo "Joe > Session started: $SESSION_ID"
fi

get_instance_params
exit 0
