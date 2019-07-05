/*
Provision wrapper

2019 © Dmitry Udalov dmius@postgres.ai
2019 © Anatoly Stansler anatoly@postgres.ai
2019 © Postgres.ai
*/

package provision

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"../ec2ctrl"
	"../log"

	"github.com/docker/machine/libmachine/ssh"
	"github.com/tkanos/gonfig"
)

const (
	DOCKER_NAME      = "inside-instance-docker"
	PRICE_MULTIPLIER = 1.1
	STATE_DIR        = "joe-run"
	STATE_FILE       = "joestate.json"
)

var awsValidDurations = []int64{60, 120, 180, 240, 300, 360}

type ProvisionState struct {
	InstanceId        string
	InstanceIp        string
	DockerContainerId string
	SessionId         string
}

type ProvisionConfiguration struct {
	AwsConfiguration ec2ctrl.Ec2Configuration
	Debug            bool
	EbsVolumeId      string
	PgVersion        string
	DbUsername       string // Database user will be created with specified credentials.
	DbPassword       string
	SshTunnelPort    uint
	InitialSnapshot  string
}

type Provision struct {
	config            ProvisionConfiguration
	ec2ctrl           ec2ctrl.Ec2Ctrl
	instanceId        string
	instanceIp        string
	sshClient         ssh.Client
	dockerContainerId string
	sessionId         string
}

func NewProvision(conf ProvisionConfiguration) *Provision {
	ec2ctrl := ec2ctrl.NewEc2Ctrl(conf.AwsConfiguration)
	provision := &Provision{
		config:     conf,
		ec2ctrl:    *ec2ctrl,
		instanceId: "",
		instanceIp: "",
	}
	result, err := provision.ReadState()
	log.Dbg("Read state:", result, err)
	return provision
}

// Check validity of a configuration and show a message for each violation.
func IsValidConfig(config ProvisionConfiguration) bool {
	result := true

	if config.AwsConfiguration.AwsInstanceType == "" {
		log.Err("Wrong configuration AwsInstanceType value.")
		result = false
	}

	if ec2ctrl.RegionDetails[config.AwsConfiguration.AwsRegion] == nil {
		log.Err("Wrong configuration AwsRegion value.")
		result = false
	}

	if len(config.AwsConfiguration.AwsZone) != 1 {
		log.Err("Wrong configuration AwsZone value.")
		result = false
	}

	duration := config.AwsConfiguration.AwsBlockDurationMinutes
	isValidDuration := false
	for _, validDuration := range awsValidDurations {
		if duration == validDuration {
			isValidDuration = true
			break
		}
	}
	if !isValidDuration {
		log.Err("Wrong configuration AwsBlockDurationMinutes value.")
		result = false
	}

	if config.AwsConfiguration.AwsKeyName == "" {
		log.Err("Wrong configuration AwsKeyName value.")
		result = false
	}

	if config.AwsConfiguration.AwsKeyPath == "" {
		log.Err("Wrong configuration AwsKeyName value.")
		result = false
	}

	if _, err := os.Stat(config.AwsConfiguration.AwsKeyPath); err != nil {
		log.Err("Wrong configuration AwsKeyName value. File does not exits.")
		result = false
	}

	if config.InitialSnapshot == "" {
		log.Err("Wrong configuration InitialSnapshot value.")
		result = false
	}

	return result
}

// Start new EC2 instance
func (j *Provision) StartInstance() (bool, error) {
	price := j.ec2ctrl.GetHistoryInstancePrice()
	log.Msg("Starting instance...")
	price = price * PRICE_MULTIPLIER
	j.instanceId = ""
	var err error
	j.instanceId, err = j.ec2ctrl.CreateSpotInstance(price)
	if err != nil {
		log.Err(err)
		return false, err
	}
	j.instanceIp, err = j.ec2ctrl.GetPublicInstanceIpAddress(j.instanceId)
	if err != nil {
		log.Err(err)
		return false, fmt.Errorf("Unable to get a instance ip. May be instance not started %v.", err)
	}
	log.Msg("Instance is ready. Instance id is " + log.YELLOW + j.instanceId + log.END)
	//-o LogLevel=quiet
	log.Msg("To connect to instance use: " + log.WHITE +
		"ssh -o 'StrictHostKeyChecking no' -i " + j.config.AwsConfiguration.AwsKeyPath + " ubuntu@" + j.instanceIp +
		log.END)
	return true, nil
}

// Get pointer to Ec2Ctrl instance
func (j *Provision) GetEc2Ctrl() *ec2ctrl.Ec2Ctrl {
	return &j.ec2ctrl
}

// Get id of currently active instance
func (j *Provision) GetInstanceId() string {
	return j.instanceId
}

// Check existance of instance
func (j *Provision) InstanceExists() (bool, error) {
	if j.instanceId == "" {
		return false, fmt.Errorf("Instance Id not specified.")
	}
	return j.ec2ctrl.IsInstanceRunning(j.instanceId)
}

func (j *Provision) terminateInstance() (bool, error) {
	if j.instanceId != "" {
		res, err := j.ec2ctrl.TerminateInstance(j.instanceId)
		j.instanceId = ""
		j.instanceIp = ""
		j.dockerContainerId = ""
		return res, err
	}
	return false, nil
}

// Start SSH accecss to instance
func (j *Provision) StartInstanceSsh() (bool, error) {
	var err error
	j.sshClient, err = j.ec2ctrl.GetInstanceSshClient(j.instanceId)
	if err == nil && j.sshClient != nil {
		serr := j.ec2ctrl.WaitInstanceForSsh()
		if serr == nil {
			return true, nil
		}
	}
	j.terminateInstance()
	return false, fmt.Errorf("Cannot connect to instance via SSH")
}

// Attach EC2 drive to instance which ZFS formatted and has database snapshot
func (j *Provision) AttachZfsPancake() (bool, error) {
	log.Msg("Attaching pancake drive...")
	result := true
	out, scerr := j.ec2ctrl.RunInstanceSshCommand("sudo apt-get update", j.config.Debug)
	if scerr != nil {
		return false, fmt.Errorf("Can't execute `sudo apt-get update`")
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("sudo apt-get install -y zfsutils-linux", j.config.Debug)
	if scerr != nil {
		return false, fmt.Errorf("Can't execute `sudo apt-get install -y zfsutils-linux`")
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("sudo sh -c \"mkdir /home/storage\"", j.config.Debug)
	if scerr != nil {
		return false, fmt.Errorf("Can't execute `sudo sh -c \"mkdir /home/storage\"`")
	}
	_, verr := j.ec2ctrl.AttachInstanceVolume(j.instanceId, j.config.EbsVolumeId, "/dev/xvdc")
	if verr != nil {
		return false, fmt.Errorf("Cannot attach pancake disk to instance, %v", verr)
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("sudo zpool import -R / zpool", j.config.Debug)
	if scerr != nil {
		return false, fmt.Errorf("Can't attach ZFS pancake drive")
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("sudo df -h /home/storage", j.config.Debug)
	if scerr != nil {
		return false, fmt.Errorf("Can't execute `sudo df -h /home/storage`")
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("grep MemTotal /proc/meminfo | awk '{print $2}'", j.config.Debug)
	if scerr != nil {
		return false, fmt.Errorf("Can't execute `grep MemTotal /proc/meminfo | awk '{print $2}'`")
	}
	out = strings.Trim(out, "\n")
	memTotalKb, _ := strconv.Atoi(out)
	arcSizeB := memTotalKb / 100 * 30 * 1024
	if arcSizeB < 1073741824 {
		arcSizeB = 1073741824 // 1 GiB
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("echo "+strconv.FormatInt(int64(arcSizeB), 10)+" | sudo tee /sys/module/zfs/parameters/zfs_arc_max", j.config.Debug)
	if scerr != nil {
		return false, fmt.Errorf("Can't set up zfs_arc_max to /sys/module/zfs/parameters/zfs_arc_max")
	}
	return result, nil
}

// Start docker inside instance
func (j *Provision) StartDocker() (bool, error) {
	log.Msg("Installing docker...")
	var out string
	var scerr error
	result := true
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("sudo apt install -y docker.io", j.config.Debug)
	result = result && scerr == nil
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("docker --version", false)
	result = result && scerr == nil
	log.Msg("Installed docker version: " + log.WHITE + strings.Trim(out, "\n") + log.END)

	log.Msg("Pulling docker image...")
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("sudo docker pull \"postgresmen/postgres-nancy:"+
		j.config.PgVersion+"\" 2>&1 | grep -e 'Pulling from' -e Digest -e Status -e Error", j.config.Debug)
	result = result && scerr == nil
	if scerr != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Cannot pull docker image, %v", scerr)
	}
	log.Msg("Starting docker...")
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("sudo docker run --cap-add SYS_ADMIN "+
		"--name=\""+DOCKER_NAME+"\" -p 5432:5432 -v /home/ubuntu:/machine_home "+
		"-v /home/storage:/storage "+
		"-dit \"postgresmen/postgres-nancy:"+j.config.PgVersion+"\"", j.config.Debug)
	result = result && scerr == nil
	if scerr != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Cannot start docker, %v", scerr)
	}
	j.dockerContainerId = strings.Trim(out, "\n")

	log.Msg("Docker container hash is  " + log.YELLOW + j.dockerContainerId + log.END)
	log.Msg("To connect to docker use: " + log.WHITE + "sudo docker exec -it " + DOCKER_NAME + " bash" + log.END)
	log.Msg("or:                       " + log.WHITE + "sudo docker exec -i " + j.dockerContainerId + " bash" + log.END)
	return result, nil
}

// Execute bash command inside docker
func (j *Provision) dockerRunCommand(command string, debug bool) (string, error) {
	cmd := command
	cmd = strings.ReplaceAll(cmd, "\"", "\\\"")
	cmd = strings.ReplaceAll(cmd, "\n", " ") // for multiline sql code
	return j.ec2ctrl.RunInstanceSshCommand("sudo docker exec -i "+j.dockerContainerId+" bash -c \""+
		cmd+"\"", debug)
}

// Execute bash command inside docker
func (j *Provision) DockerRunCommand(command string) (string, error) {
	return j.dockerRunCommand(command, j.config.Debug)
}

// Stop postgres inside docker
func (j *Provision) DockerStopPostgres() (bool, error) {
	log.Dbg("Stopping Postgres...")
	var cnt int
	var out string
	var err error
	cnt = 0
	for true {
		out, err = j.DockerRunCommand("ps auxww | grep postgres | grep -v \"grep\" 2>/dev/null || echo ''")
		out = strings.Trim(out, "\n ")
		if out == "" && err == nil {
			log.Dbg("Postgres stopped")
			return true, nil
		}
		cnt++
		if cnt > 1000 && out != "" && err == nil {
			return false, fmt.Errorf("Cannot stop postgres in 15 minutes.")
		}
		if cnt > 900 { // 15 minutes = 900 seconds
			out, err = j.DockerRunCommand("sudo killall -s 9 postgres || true")
		}
		out, err = j.DockerRunCommand("sudo pg_ctlcluster " + j.config.PgVersion + " main stop -m f || true")
		time.Sleep(1 * time.Second)
	}
	return false, nil
}

// Start postgres inside docker
func (j *Provision) DockerStartPostgres() (bool, error) {
	log.Dbg("Starting Postgres...")
	var cnt int
	var out string
	var err error
	cnt = 0
	for true {
		out, err = j.DockerRunCommand("ps auxww | grep postgres | grep -v \"grep\" 2>/dev/null || echo ''")
		out = strings.Trim(out, "\n ")
		if out != "" && err == nil {
			log.Dbg("Postgres started.")
			return true, nil
		}
		cnt++
		if cnt > 900 { // 15 minutes = 900 seconds
			return false, fmt.Errorf("Can't start Postgres in 15 minutes.")
		}
		out, err = j.DockerRunCommand("sudo pg_ctlcluster " + j.config.PgVersion + " main start || true")
		time.Sleep(1 * time.Second)
	}
	return false, nil
}

// Move pointer to postgres pgdata to external drive
// attached by AttachZfsPancake
func (j *Provision) DockerMovePostgresPgData() (bool, error) {
	var result bool
	var err error
	result, err = j.DockerStopPostgres()
	if result == true && err == nil {
		j.DockerRunCommand("sudo mv /var/lib/postgresql /var/lib/postgresql_original")
		j.DockerRunCommand("ln -s /storage/postgresql /var/lib/postgresql")
		result, err = j.DockerStartPostgres()
	}
	return result, err
}

// Create ZFS snapshot on drive attached by AttachZfsPancake
func (j *Provision) CreateZfsSnapshot(name string) (bool, error) {
	log.Dbg("Create database snapshot")
	var result bool
	var err error
	result, err = j.DockerStopPostgres()
	if result == true && err == nil {
		out, cerr := j.ec2ctrl.RunInstanceSshCommand("sudo zfs snapshot -r zpool@"+name, j.config.Debug)
		if cerr != nil {
			return false, fmt.Errorf("Can't create ZFS snapshot: %s, %v", out, cerr)
		}
		result, err = j.DockerStartPostgres()
	}
	return result, err
}

// Rollback to ZFS snapshot on drive attached by AttachZfsPancake
func (j *Provision) DockerRollbackZfsSnapshot(name string) (bool, error) {
	log.Dbg("Rollback database to snapshot")
	var result bool
	var err error
	result, err = j.DockerStopPostgres()
	if result == true && err == nil {
		out, cerr := j.ec2ctrl.RunInstanceSshCommand("sudo zfs rollback -f -r zpool@"+name, j.config.Debug)
		if cerr != nil {
			return false, fmt.Errorf("Can't rollback ZFS snapshot: %s, %v", out, cerr)
		}
		result, err = j.DockerStartPostgres()
	}
	return result, err
}

func (j *Provision) getStateFilePath() string {
	bindir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	dir, _ := filepath.Abs(filepath.Dir(bindir))
	return dir + string(os.PathSeparator) + STATE_DIR + string(os.PathSeparator) + STATE_FILE
}

// Read state informatation from file
func (j *Provision) ReadState() (bool, error) {
	state := ProvisionState{}
	err := gonfig.GetConf(j.getStateFilePath(), &state)
	if err != nil {
		log.Err("ReadState: Can't read state file.", err)
		return false, fmt.Errorf("Can't read state file. %v", err)
	}

	if state.InstanceId != j.instanceId {
		if j.instanceId != "" {
			log.Err("ReadState: State read, but current instance id differ from read instance id.")
			return false, fmt.Errorf("State read, but current instance id differ from read instance id.")
		}
		res, err := j.ec2ctrl.IsInstanceRunning(state.InstanceId)
		if res == true {
			j.instanceId = state.InstanceId
			j.instanceIp, err = j.ec2ctrl.GetPublicInstanceIpAddress(state.InstanceId)
			res, err = j.StartInstanceSsh()
			if res != true && err != nil {
				j.terminateInstance()
				log.Err("ReadState:  Can't connect to instance via SSH.", err)
				return false, fmt.Errorf("Can't connect to instance via SSH. %v", err)
			}
			j.dockerContainerId = state.DockerContainerId
			out, derr := j.DockerRunCommand("echo 1")
			out = strings.Trim(out, "\n")
			if out == "1" && derr == nil {
				j.sessionId = state.SessionId
				return true, nil
			} else {
				j.terminateInstance()
				log.Err("ReadState: Can't connect to docker.", out, derr)
				return false, fmt.Errorf("Can't connect to docker. %s %v", out, derr)
			}
		}
	} else {
		log.Dbg("ReadState: saves instance id is equal current instance id", state.InstanceId, j.instanceId)
	}
	return false, err
}

// Write state to file
func (j *Provision) WriteState() (bool, error) {
	state := ProvisionState{
		InstanceId:        j.instanceId,
		InstanceIp:        j.instanceIp,
		DockerContainerId: j.dockerContainerId,
		SessionId:         j.sessionId,
	}
	f, oerr := os.Create(j.getStateFilePath())
	if oerr != nil {
		return false, oerr
	}
	b, jerr := json.Marshal(state)
	if jerr != nil {
		return false, jerr
	}
	wrote, werr := f.Write(b)
	if wrote <= 0 {
		return false, werr
	}
	f.Close()
	return true, nil
}

// Start istance for db lab tests
func (j *Provision) StartWorkingInstance() (bool, error) {
	var result bool
	var err error
	var out string
	result, err = j.StartInstance()
	log.Dbg("Creating instance:", result, err)
	if err != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Can't start instance. %v", err)
	}
	// Check instance existing
	result, err = j.StartInstanceSsh()
	log.Dbg("Start SSH:", result, err)
	if err != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Can't get SSH access to instance. %v", err)
	}
	result, err = j.AttachZfsPancake()
	log.Dbg("Attach ZFS pancake drive:", result, err)
	if err != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Can't attach pancake drive. %v", err)
	}
	result, err = j.StartDocker()
	log.Dbg("Start docker:", result, err)
	if err != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Can't start docker. %v", err)
	}
	out, err = j.DockerRunCommand("echo 1")
	out = strings.Trim(out, "\n")
	if out != "1" || err != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Can't get access to docker. %v", err)
	}
	result, err = j.DockerMovePostgresPgData()
	log.Dbg("Move PGdata pointer:", result, err)
	if err != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Can't move data to pancake drive. %v", err)
	}
	return true, nil
}

// Start test session
func (j *Provision) StartSession(options ...string) (bool, string, error) {
	snapshot := j.config.InitialSnapshot
	if len(options) > 0 && len(options[0]) > 0 {
		snapshot = options[0]
	}

	if j.sessionId != "" {
		return false, j.sessionId, fmt.Errorf("Session already started")
	}
	j.sessionId = strconv.FormatInt(time.Now().UnixNano(), 10)
	// check instance existance
	if j.instanceId != "" {
		out, derr := j.DockerRunCommand("echo 1")
		out = strings.Trim(out, "\n")
		if out != "1" || derr != nil {
			j.terminateInstance()
		}
	}
	if j.instanceId == "" {
		res, err := j.StartWorkingInstance()
		if !res || err != nil {
			return false, "", fmt.Errorf("Can't start working instance. %v", err)
		}
	}
	_, err := j.DockerRollbackZfsSnapshot(snapshot)
	if err != nil {
		return false, "", fmt.Errorf("Can't rollback database. %v", err)
	}
	err = j.DockerCreateDbUser()
	if err != nil {
		return false, "", fmt.Errorf("Can't create database user. %v", err)
	}
	res, _ := j.WriteState()
	if res == false {
		log.Err("Can't save state")
	}
	err = j.CreateSshTunnel()
	if err != nil {
		return false, "", err
	}
	return true, j.sessionId, nil
}

// Stop test session
func (j *Provision) StopSession() (bool, error) {
	j.CloseSshTunnel()
	j.sessionId = ""
	return j.WriteState()
}

func (j *Provision) ResetSession(options ...string) error {
	snapshot := j.config.InitialSnapshot
	if len(options) > 0 {
		snapshot = options[0]
	}

	_, err := j.DockerRollbackZfsSnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("Unable to rollback database. %v", err)
	}

	err = j.DockerCreateDbUser()
	if err != nil {
		return fmt.Errorf("Unable to update rolled back database. %v", err)
	}

	return nil
}

// Create test user
func (j *Provision) DockerCreateDbUser() error {
	var err error
	sql := "select 1 from pg_catalog.pg_roles where rolname = '" + j.config.DbUsername + "'"
	out, err := j.DockerRunCommand("psql -Upostgres -d postgres -t -c \"" + sql + "\"")
	out = strings.Trim(out, "\n ")

	if err != nil {
		return err
	}

	if out == "1" {
		log.Dbg("Test user already exists")
		return nil
	}

	sql = "CREATE ROLE " + j.config.DbUsername + " LOGIN password '" + j.config.DbPassword + "' superuser;"
	out, err = j.dockerRunCommand("psql -Upostgres -d postgres -t -c \""+sql+"\"", false)
	log.Dbg("Create test user", out, err)

	return err
}

// Execute local shell command
func (j *Provision) ExecuteLocalCmd(command string, wait bool) ([]byte, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", command)
	} else {
		cmd = exec.Command("bash", "-c", command)
	}
	if wait == true {
		return cmd.Output()
	} else {
		return nil, cmd.Start()
	}
}

// Open local SSH tunnel
func (j *Provision) OpenSshTunnel() error {
	var err error
	bindir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	dir, _ := filepath.Abs(filepath.Dir(bindir))
	sockFilePath := dir + string(os.PathSeparator) + STATE_DIR + string(os.PathSeparator) + "sshsock"
	cmd := "ssh -o 'StrictHostKeyChecking no' -i " +
		j.config.AwsConfiguration.AwsKeyPath +
		" -f -N -M -S " + sockFilePath + " -L " +
		strconv.FormatUint(uint64(j.config.SshTunnelPort), 10) + ":localhost:5432" +
		" ubuntu@" + j.instanceIp + " &"
	_, err = j.ExecuteLocalCmd(cmd, false)
	if err == nil {
		log.Dbg("Opening SSH tunnel " + log.OK)
	} else {
		log.Dbg("Opening SSH tunnel " + log.FAIL)
	}

	return err
}

// Close local SSH tunnel
func (j *Provision) CloseSshTunnel() error {
	var err error
	bindir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	dir, _ := filepath.Abs(filepath.Dir(bindir))
	sockFilePath := dir + string(os.PathSeparator) + STATE_DIR + string(os.PathSeparator) + "sshsock"
	cmd := "ssh -S " + sockFilePath + " -O exit ubuntu@" + j.instanceIp + " &"
	_, err = j.ExecuteLocalCmd(cmd, false)
	if err == nil {
		log.Dbg("Closing SSH tunnel " + log.OK)
	} else {
		log.Dbg("Closing SSH tunnel " + log.FAIL)
	}

	return err
}

// Check existance and readiness of local SSH tunnel
func (j *Provision) SshTunnelExists() bool {
	log.Dbg("Checking SSH tunnel...")

	cmd := "PGPASSWORD=" + j.config.DbPassword +
		" psql -t -q -h localhost -p " + strconv.FormatUint(uint64(j.config.SshTunnelPort), 10) +
		" --user=" + j.config.DbUsername +
		" postgres -c \"select '1';\" || echo 0"
	outb, err := j.ExecuteLocalCmd(cmd, true)
	out := strings.Trim(string(outb), "\n ")

	if err != nil || out != "1" {
		log.Err("Check SSH tunnel: %v %s")
		return false
	}

	return true
}

// Establish SSH tunnel
func (j *Provision) CreateSshTunnel() error {
	if !j.SshTunnelExists() {
		j.CloseSshTunnel()
		err := j.OpenSshTunnel()
		if err != nil {
			return fmt.Errorf("Can't establish SSH tunnel: %v", err)
		}
	}

	log.Dbg("SSH tunnel is " + log.GREEN + "ready" + log.END)
	return nil
}
