/*
2019 © Postgres.ai
*/

package provision

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"../log"
	"../util"
)

const LOGS_PREFIX = "joe_postgres_"

// We use pg_stop -D ... -m immediate stop because we need to shut down
// Postgres faster and completely get rid of this instance. So we don't care
// about its state.
const MODE_IMMEDIATE = "immediate"

type PgConfig struct {
	Version string
	Bindir  string

	// PGDATA
	Datadir string

	Host string
	Port uint
	Name string

	// The specified user must exist. The user will not be created automatically.
	Username string
	Password string
}

func (c PgConfig) getBindir() string {
	if len(c.Bindir) > 0 {
		return strings.TrimRight(c.Bindir, "/") + ""
	}

	// By default, we assume that we are working on Ubuntu/Debian.
	return fmt.Sprintf("/usr/lib/postgresql/%s/bin", c.Version)
}

func (c PgConfig) getPortStr() string {
	return strconv.FormatUint(uint64(c.Port), 10)
}

func (c PgConfig) getUsername() string {
	if len(c.Username) > 0 {
		return c.Username
	}

	return "postgres"
}

func (c PgConfig) getPassword() string {
	if len(c.Password) > 0 {
		return c.Password
	}

	return "postgres"
}

func (c PgConfig) getDbName() string {
	if len(c.Name) > 0 {
		return c.Name
	}

	return "postgres"
}

func PostgresStart(r Runner, c *PgConfig) error {
	log.Dbg("Starting Postgres...")

	portStr := c.getPortStr()
	logdir := "/var/log/" + LOGS_PREFIX + portStr + ".log"

	createLogsCmd := "sudo touch " + logdir + " && " +
		"sudo chown postgres " + logdir
	out, err := r.Run(createLogsCmd, true)
	if err != nil {
		return fmt.Errorf("Postgres start: log touch %v %v", err, out)
	}

	// TODO(anatoly): pgdata = pgdata + config.PgdataSubpath.

	// pg_ctl status mode checks whether a server is running in the specified data directory.
	_, err = pgctlStatus(r, c)
	if err != nil {
		if rerr, ok := err.(RunnerError); ok {
			switch rerr.ExitStatus {
			// If an accessible data directory is not specified, the process returns an exit status of 4.
			case 4:
				return fmt.Errorf("The specified pgBindir is unaccesible. %v", rerr)

			// If the server is not running, the process returns an exit status of 3.
			case 3:
				_, err = pgctlStart(r, logdir, c)
				if err != nil {
					return err
				}

			default:
				return rerr
			}
		}
	}
	// No errors – assume that the server is running.

	// Waiting for server to become ready and promoting if needed.
	first := true
	cnt := 0
	for true {
		out, err = runPsql(r, "select pg_is_in_recovery()", c)

		if err == nil {
			// Server does not need promotion if it is not in recovery.
			if out == "f" {
				break
			}

			// Run promotion if needed only first time.
			if out == "t" && first {
				log.Dbg("Postgres instance needs promotion.")

				first = false

				_, err = pgctlPromote(r, c)
				if err != nil {
					rerr := PostgresStop(r, c)
					if rerr != nil {
						log.Err(err)
					}

					return err
				}
			}
		}

		cnt++
		if cnt > 360 { // 3 minutes
			rerr := PostgresStop(r, c)
			if rerr != nil {
				log.Err(err)
			}

			return fmt.Errorf("Postgres could not be promoted within 3 minutes.")
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

func PostgresStop(r Runner, c *PgConfig) error {
	log.Dbg("Stopping Postgres...")

	var err error

	first := true
	cnt := 0
	for true {
		// pg_ctl status mode checks whether a server is running in the specified data directory.
		_, err = pgctlStatus(r, c)
		if err != nil {
			if rerr, ok := err.(RunnerError); ok {
				switch rerr.ExitStatus {
				// If an accessible data directory is not specified, the process returns an exit status of 4.
				case 4:
					return fmt.Errorf("The specified pgBindir is unaccesible. %v", rerr)

				// If the server is not running, the process returns an exit status of 3.
				case 3:
					// Postgres stopped.
					return nil

				default:
					return rerr
				}
			}
		}
		// No errors – assume that the server is running.

		if first {
			first = false

			_, err = pgctlStop(r, MODE_IMMEDIATE, c)
			if err != nil {
				return err
			}
		}

		cnt++
		if cnt > 360 { // 3 minutes
			return fmt.Errorf("Postgres could not be stopped within 3 minutes.")
		}
		time.Sleep(500 * time.Millisecond)
	}

	panic(fmt.Errorf("Postgres stop: Unreachable code."))
}

func PostgresList(r Runner, prefix string) ([]string, error) {
	listProcsCmd := fmt.Sprintf(`ps ax`)

	out, err := r.Run(listProcsCmd, false)
	if err != nil {
		return []string{}, err
	}

	re := regexp.MustCompile(fmt.Sprintf(`(%s[0-9]+)`, prefix))

	return util.Unique(re.FindAllString(out, -1)), nil
}

func pgctlStart(r Runner, logdir string, c *PgConfig) (string, error) {
	startCmd := `sudo --user postgres ` +
		c.getBindir() + `/pg_ctl ` +
		`--pgdata /` + c.Datadir + ` ` +
		`--log ` + logdir + ` ` +
		`-o "-p ` + c.getPortStr() + `" ` +
		`start`

	return r.Run(startCmd, true)
}

func pgctlStop(r Runner, mode string, c *PgConfig) (string, error) {
	stopCmd := "sudo --user postgres " +
		c.getBindir() + "/pg_ctl " +
		"--pgdata /" + c.Datadir + " " +
		"--mode " + mode + " " +
		"stop"

	return r.Run(stopCmd, true)
}

func pgctlStatus(r Runner, c *PgConfig) (string, error) {
	statusCmd := `sudo --user postgres ` +
		c.getBindir() + `/pg_ctl ` +
		`--pgdata /` + c.Datadir + ` ` +
		`status`

	return r.Run(statusCmd, true)
}

func pgctlPromote(r Runner, c *PgConfig) (string, error) {
	startCmd := `sudo --user postgres ` +
		c.getBindir() + `/pg_ctl ` +
		`--pgdata /` + c.Datadir + ` ` +
		`promote`

	return r.Run(startCmd, true)
}

// TODO(anatoly): Use SQL runner.
func runPsql(r Runner, command string, c *PgConfig) (string, error) {
	host := ""
	if len(c.Host) > 0 {
		host = "--host " + c.Host + " "
	}

	psqlCmd := `PGPASSWORD=` + c.getPassword() + ` ` +
		`sudo --user postgres ` +
		c.getBindir() + `/psql ` +
		host +
		`--dbname ` + c.getDbName() + ` ` +
		`--port ` + c.getPortStr() + ` ` +
		`--username ` + c.getUsername() + ` ` +
		`-XAtc "` + command + `"`

	return r.Run(psqlCmd)
}
