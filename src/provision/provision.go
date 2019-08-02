/*
Provision wrapper

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
	PG_PROCESS_CHECK = "ps ax | grep postgres | grep -v \"grep\" | " +
		"awk '{print $5}' | grep \"postgres:\" 2>/dev/null || echo ''"
)

var awsValidDurations = []int64{60, 120, 180, 240, 300, 360}

type ProvisionState struct {
	InstanceId        string
	InstanceIp        string
	DockerContainerId string
	SessionId         string
}

type LocalConfiguration struct {
	PgStartCommand string `yaml:"pgStartCommand"`
	PgStopCommand  string `yaml:"pgStopCommand"`
}

type ProvisionConfiguration struct {
	AwsConfiguration   ec2ctrl.Ec2Configuration
	LocalConfiguration LocalConfiguration
	Debug              bool
	EbsVolumeId        string
	PgVersion          string
	DbUsername         string // Database user will be created with the specified credentials.
	DbPassword         string
	SshTunnelPort      uint
	InitialSnapshot    string
	Local              bool
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

	if config.Local {
		return true
	}

	if config.AwsConfiguration.AwsInstanceType == "" {
		log.Err("AwsInstanceType cannot be empty.")
		result = false
	}

	if ec2ctrl.RegionDetails[config.AwsConfiguration.AwsRegion] == nil {
		log.Err("Wrong configuration AwsRegion value.")
		result = false
	}

	if len(config.AwsConfiguration.AwsZone) != 1 {
		log.Err("Wrong configuration AwsZone value (must be exactly 1 letter).")
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
		log.Err("AwsKeyName cannot be empty.")
		result = false
	}

	if config.AwsConfiguration.AwsKeyPath == "" {
		log.Err("AwsKeyPath cannot be empty.")
		result = false
	}

	if _, err := os.Stat(config.AwsConfiguration.AwsKeyPath); err != nil {
		log.Err("Wrong configuration AwsKeyPath value. File does not exits.")
		result = false
	}

	if config.InitialSnapshot == "" {
		log.Err("InitialSnapshot cannot be empty.")
		result = false
	}

	return result
}

// Start new EC2 instance
func (j *Provision) StartInstance() (bool, error) {
	price := j.ec2ctrl.GetHistoryInstancePrice()
	log.Msg("Starting an instance...")
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
		return false, fmt.Errorf("Unable to get the IP of the instance. Check that the instance has started %v.", err)
	}
	log.Msg("The instance is ready. Instance id is " + log.YELLOW + j.instanceId + log.END)
	//-o LogLevel=quiet
	log.Msg("To connect to the instance use: " + log.WHITE +
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
		return false, fmt.Errorf("Instance id not specified.")
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
	return false, fmt.Errorf("Cannot connect to the instance using SSH.")
}

// Attach EC2 drive to instance which ZFS formatted and has database snapshot
func (j *Provision) AttachZfsPancake() (bool, error) {
	log.Msg("Attaching pancake drive...")
	result := true
	out, scerr := j.ec2ctrl.RunInstanceSshCommand("sudo apt-get update", j.config.Debug)
	if scerr != nil {
		return false, scerr
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("sudo apt-get install -y zfsutils-linux", j.config.Debug)
	if scerr != nil {
		return false, scerr
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("sudo sh -c \"mkdir /home/storage\"", j.config.Debug)
	if scerr != nil {
		return false, scerr
	}
	_, verr := j.ec2ctrl.AttachInstanceVolume(j.instanceId, j.config.EbsVolumeId, "/dev/xvdc")
	if verr != nil {
		return false, fmt.Errorf("Cannot attach the persistent disk to the instance, %v.", verr)
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("sudo zpool import -R / zpool", j.config.Debug)
	if scerr != nil {
		return false, scerr
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("sudo df -h /home/storage", j.config.Debug)
	if scerr != nil {
		return false, scerr
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("grep MemTotal /proc/meminfo | awk '{print $2}'", j.config.Debug)
	if scerr != nil {
		return false, scerr
	}
	out = strings.Trim(out, "\n")
	memTotalKb, _ := strconv.Atoi(out)
	arcSizeB := memTotalKb / 100 * 30 * 1024
	if arcSizeB < 1073741824 {
		arcSizeB = 1073741824 // 1 GiB
	}
	out, scerr = j.ec2ctrl.RunInstanceSshCommand("echo "+strconv.FormatInt(int64(arcSizeB), 10)+" | sudo tee /sys/module/zfs/parameters/zfs_arc_max", j.config.Debug)
	if scerr != nil {
		return false, scerr
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
		return false, fmt.Errorf("Cannot start Docker, %v.", scerr)
	}
	j.dockerContainerId = strings.Trim(out, "\n")

	log.Msg("Docker container hash is  " + log.YELLOW + j.dockerContainerId + log.END)
	log.Msg("To connect to Docker use: " + log.WHITE + "sudo docker exec -it " + DOCKER_NAME + " bash" + log.END)
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

// Execute bash command inside Docker
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
		out, err = j.DockerRunCommand(PG_PROCESS_CHECK)
		out = strings.Trim(out, "\n ")
		if out == "" && err == nil {
			log.Dbg("Postgres has been stopped.")
			return true, nil
		}
		cnt++
		if cnt > 1000 && out != "" && err == nil {
			return false, fmt.Errorf("Postgres could not be stopped in 15 minutes.")
		}
		if cnt > 900 { // 15 minutes = 900 seconds
			out, err = j.DockerRunCommand("sudo killall -s 9 postgres || true")
		}
		out, err = j.DockerRunCommand("sudo pg_ctlcluster " + j.config.PgVersion + " main stop -m f || true")
		time.Sleep(1 * time.Second)
	}
	return false, nil
}

// Start Postgres inside Docker
func (j *Provision) DockerStartPostgres() (bool, error) {
	log.Dbg("Starting Postgres...")
	var cnt int
	var out string
	var err error
	cnt = 0
	for true {
		out, err = j.DockerRunCommand(PG_PROCESS_CHECK)
		out = strings.Trim(out, "\n ")
		if out != "" && err == nil {
			log.Dbg("Postgres has been started.")
			return true, nil
		}
		cnt++
		if cnt > 900 { // 15 minutes = 900 seconds
			return false, fmt.Errorf("Postgres could not be started in 15 minutes.")
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
	log.Dbg("Create a database snapshot.")
	var result bool
	var err error
	result, err = j.DockerStopPostgres()
	if result == true && err == nil {
		out, cerr := j.ec2ctrl.RunInstanceSshCommand("sudo zfs snapshot -r zpool@"+name, j.config.Debug)
		if cerr != nil {
			return false, fmt.Errorf("Cannot create a ZFS snapshot: %s, %v.", out, cerr)
		}
		result, err = j.DockerStartPostgres()
	}
	return result, err
}

// Rollback to ZFS snapshot on drive attached by AttachZfsPancake
// TODO @Nikolay add comments for all function signatures (desc, params, returning type)
func (j *Provision) DockerRollbackZfsSnapshot(name string) (bool, error) {
	log.Dbg("Rollback the state of the database to the specified snapshot.")
	var result bool
	var err error
	result, err = j.DockerStopPostgres()
	if result == true && err == nil {
		out, cerr := j.ec2ctrl.RunInstanceSshCommand("sudo zfs rollback -f -r zpool@"+name, j.config.Debug)
		if cerr != nil {
			return false, fmt.Errorf("Cannot rollback to the ZFS snapshot: %s, %v.", out, cerr)
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
		log.Err("ReadState: Cannot read state file.", err)
		return false, fmt.Errorf("Cannot read state file. %v", err)
	}

	if state.InstanceId != j.instanceId {
		if j.instanceId != "" {
			log.Err("ReadState: State read, but current instance id differs from the read instance id.")
			return false, fmt.Errorf("State read, but current instance id differs from the read instance id.")
		}
		res, err := j.ec2ctrl.IsInstanceRunning(state.InstanceId)
		if res == true {
			j.instanceId = state.InstanceId
			j.instanceIp, err = j.ec2ctrl.GetPublicInstanceIpAddress(state.InstanceId)
			res, err = j.StartInstanceSsh()
			if res != true && err != nil {
				j.terminateInstance()
				log.Err("ReadState:  Cannot connect to the instance using SSH.", err)
				return false, fmt.Errorf("Cannot connect to the instance using SSH. %v", err)
			}
			j.dockerContainerId = state.DockerContainerId
			out, derr := j.DockerRunCommand("echo 1")
			out = strings.Trim(out, "\n")
			if out == "1" && derr == nil {
				j.sessionId = state.SessionId
				return true, nil
			} else {
				j.terminateInstance()
				log.Err("ReadState: Cannot connect to Docker.", out, derr)
				return false, fmt.Errorf("Cannot connect to Docker. %s %v", out, derr)
			}
		}
	} else {
		log.Dbg("ReadState: saved instance id is equal to the current instance id", state.InstanceId, j.instanceId)
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

	log.Dbg("Provision state:", string(b))

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
		return false, fmt.Errorf("Cannot start instance. %v", err)
	}
	// Check instance existing
	result, err = j.StartInstanceSsh()
	log.Dbg("Start SSH:", result, err)
	if err != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Cannot get an SSH access to the instance. %v", err)
	}
	result, err = j.AttachZfsPancake()
	log.Dbg("Attach ZFS pancake drive:", result, err)
	if err != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Cannot attach the disk. %v", err)
	}
	result, err = j.StartDocker()
	log.Dbg("Start docker:", result, err)
	if err != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Cannot start Docker. %v", err)
	}
	out, err = j.DockerRunCommand("echo 1")
	out = strings.Trim(out, "\n")
	if out != "1" || err != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Cannot get an access to Docker. %v", err)
	}
	result, err = j.DockerMovePostgresPgData()
	log.Dbg("Move PGdata pointer:", result, err)
	if err != nil {
		j.terminateInstance()
		return false, fmt.Errorf("Cannot move data to the disk. %v", err)
	}
	return true, nil
}

// Start test session
func (j *Provision) StartSession(options ...string) (bool, string, error) {
	// TODO(anatoly): Remove temporary hack.
	if j.config.Local {
		err := j.LocalResetSession()
		if err != nil {
			return false, "", err
		}
		return true, j.sessionId, nil
	}

	snapshot := j.config.InitialSnapshot
	if len(options) > 0 && len(options[0]) > 0 {
		snapshot = options[0]
	}

	if j.sessionId != "" {
		return false, j.sessionId, fmt.Errorf("Session has been started already.")
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
			return false, "", fmt.Errorf("Cannot start the working instance. %v", err)
		}
	}
	_, err := j.DockerRollbackZfsSnapshot(snapshot)
	if err != nil {
		return false, "", fmt.Errorf("Cannot rollback the state of the database. %v", err)
	}
	err = j.DockerCreateDbUser()
	if err != nil {
		return false, "", fmt.Errorf("Cannot create a database user. %v", err)
	}
	res, _ := j.WriteState()
	if res == false {
		log.Err("Cannot save the state.")
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

	// TODO(anatoly): Remove temporary hack.
	if j.config.Local {
		return j.LocalResetSession(snapshot)
	}

	_, err := j.DockerRollbackZfsSnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("Unable to rollback database. %v", err) // TODO(NikolayS): Why "unable" here but "cannot" above?
	}

	err = j.DockerCreateDbUser()
	if err != nil {
		return fmt.Errorf("Unable to update rolled back database. %v", err) // TODO(NikolayS): Improve wording here.
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
			return fmt.Errorf("Cannot establish an SSH tunnel: %v", err)
		}
	}

	log.Dbg("SSH tunnel is " + log.GREEN + "ready" + log.END)
	return nil
}

// TODO(anatoly): Refactor to proper implementation. Remove copypasted code.
func (j *Provision) LocalResetSession(options ...string) error {
	snapshot := j.config.InitialSnapshot
	if len(options) > 0 {
		snapshot = options[0]
	}

	_, err := j.LocalRollbackZfsSnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("Unable to rollback database to the initial state. %v", err)
	}

	return nil
}

func (j *Provision) LocalRollbackZfsSnapshot(name string) (bool, error) {
	log.Dbg("Rollback the state of the database to the specified snapshot.")
	var result bool
	var err error
	result, err = j.LocalStopPostgres()
	if result == true && err == nil {
		out, cerr := LocalRunCommand("sudo zfs rollback -f -r zpool@" + name)
		if cerr != nil {
			return false, fmt.Errorf("Cannot perform \"zfs rollback\" to the specified snapshot: %s, %v.", out, cerr)
		}
		result, err = j.LocalStartPostgres()
	}
	return result, err
}

func (j *Provision) LocalStopPostgres() (bool, error) {
	log.Dbg("Stopping Postgres (local version)...")

	stopCommand := "sudo systemctl stop postgresql || true"
	if len(j.config.LocalConfiguration.PgStopCommand) > 0 {
		stopCommand = j.config.LocalConfiguration.PgStopCommand
	}
	log.Dbg("Command to be used: " + stopCommand)

	var cnt int
	var out string
	var err error
	cnt = 0
	for true {
		out, err = LocalRunCommand(PG_PROCESS_CHECK)
		out = strings.Trim(out, "\n ")
		if out == "" && err == nil {
			log.Dbg("Postgres has been stopped.")
			return true, nil
		}
		cnt++
		if cnt > 1000 && out != "" && err == nil {
			return false, fmt.Errorf("Postgres could not be stopped within 15 minutes.")
		}
		if cnt > 900 { // 15 minutes = 900 seconds
			out, err = LocalRunCommand("sudo killall -s 9 postgres || true")
		}
		out, err = LocalRunCommand(stopCommand)
		time.Sleep(1 * time.Second)
	}
	return false, nil
}

// Start Postgres inside Docker
func (j *Provision) LocalStartPostgres() (bool, error) {
	log.Dbg("Starting Postgres...")

	startCommand := "sudo systemctl stop postgresql || true"
	if len(j.config.LocalConfiguration.PgStartCommand) > 0 {
		startCommand = j.config.LocalConfiguration.PgStartCommand
	}

	var cnt int
	var out string
	var err error
	cnt = 0
	for true {
		out, err = LocalRunCommand(PG_PROCESS_CHECK)
		out = strings.Trim(out, "\n ")
		if out != "" && err == nil {
			log.Dbg("Postgres has been started.")
			return true, nil
		}
		cnt++
		if cnt > 900 { // 15 minutes = 900 seconds
			return false, fmt.Errorf("Postgres could not be started within 15 minutes.")
		}
		out, err = LocalRunCommand(startCommand)
		time.Sleep(1 * time.Second)
	}
	return false, nil
}

func LocalRunCommand(cmd string) (string, error) {
	log.Dbg(fmt.Sprintf("> exec: %s", cmd))
	out, err := exec.Command("/bin/bash", "-c", cmd).Output()
	if err != nil {
		log.Dbg(fmt.Sprintf(">> Error: %v Output: %s", err, out))
		return "", fmt.Errorf("RunCommand \"%s\" error: %v", cmd, err)
	}
	log.Dbg(fmt.Sprintf(">> Output: %s", out))
	return string(out), nil
}

func (j *Provision) IsLocal() bool {
	return j.config.Local
}
