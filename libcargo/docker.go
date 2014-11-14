package cargo

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	executable = "docker"
)

var (
	cmdSeq            int32 = 0
	errorStartTimeout       = errors.New("Start timeout")
)

type docker struct {
	seq    string
	logger Logger
	stdout io.Writer
	stderr io.Writer
}

func Docker(env *CloudEnv, logger Logger) *docker {
	seq := fmt.Sprintf("%d", atomic.AddInt32(&cmdSeq, 1))
	return &docker{
		seq:    seq,
		logger: logger,
		stdout: LoggerWriter(logger, seq+".&1| "),
		stderr: LoggerWriter(logger, seq+".&2| "),
	}
}

func (d *docker) cmdBase(arg ...string) *exec.Cmd {
	d.logger.Debug("DOCKER.%s %v", d.seq, arg)
	cmd := exec.Command(executable, arg...)
	return cmd
}

func (d *docker) cmd(arg ...string) *exec.Cmd {
	cmd := d.cmdBase(arg...)
	cmd.Stdout = d.stdout
	cmd.Stderr = d.stderr
	return cmd
}

func (d *docker) cmdOutput(arg ...string) (string, error) {
	if out, err := d.cmdBase(arg...).Output(); err != nil {
		return "", err
	} else {
		output := strings.Trim(string(out), " \n\r\t\f")
		for _, line := range strings.Split(output, "\n") {
			d.logger.Debug("%s.o | %s", d.seq, line)
		}
		return output, err
	}
}

func (d *docker) isRunning(cid string) (bool, error) {
	running, err := d.Inspect(cid, "{{.State.Running}}")
	return running == "true", err
}

func (d *docker) Inspect(cid, fmt string) (string, error) {
	return d.cmdOutput("inspect", "-f", fmt, cid)
}

func (d *docker) Pull(image string) error {
	return d.cmd("pull", image).Run()
}

func (d *docker) Create(cidfile string, args []string) (string, error) {
	cmdArgs := make([]string, len(args)+2)
	copy(cmdArgs[2:], args)
	cmdArgs[0] = "create"
	cmdArgs[1] = "--cidfile=" + cidfile

	cid := ""
	if cidBytes, err := ioutil.ReadFile(cidfile); err == nil {
		cid = string(cidBytes)
	}

	if cid != "" {
		if running, err := d.isRunning(cid); err == nil {
			if running {
				d.Stop(cid)
			}
			cmdArgs = append(cmdArgs, "--volumes-from="+cid)
		} else {
			cid = ""
		}
	}

	if err := os.Remove(cidfile); err != nil && !os.IsNotExist(err) {
		return "", err
	}

	newCid, err := d.cmdOutput(cmdArgs...)

	if cid != "" {
		if err == nil {
			d.RmForce(cid)
		} else {
			ioutil.WriteFile(cidfile, []byte(cid), 0777)
		}
	}

	return newCid, err
}

func (d *docker) Start(cid string, wg *sync.WaitGroup) error {
	args := make([]string, 1)
	args[0] = "start"
	if wg != nil {
		args = append(args, "-a")
	}
	args = append(args, cid)
	if wg == nil {
		return d.cmd(args...).Run()
	} else {
		cmd := d.cmd(args...)
		if err := cmd.Start(); err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			cmd.Wait()
			wg.Done()
		}()

		for i := 0; i < 8; i++ {
			if running, err := d.isRunning(cid); err == nil && running {
				return nil
			}
			time.Sleep(time.Duration((i+1)*100) * time.Millisecond)
		}
		return errorStartTimeout
	}
}

func (d *docker) Stop(cid string) error {
	return d.cmd("stop", cid).Run()
}

func (d *docker) Remove(cid string) error {
	return d.cmd("rm", cid).Run()
}

func (d *docker) RmForce(cid string) error {
	return d.cmd("rm", "--force", cid).Run()
}

func (d *docker) Exec(cid string, args ...string) error {
	cmdArgs := make([]string, len(args)+2)
	cmdArgs[0] = "exec"
	cmdArgs[1] = cid
	copy(cmdArgs[2:], args)
	return d.cmd(cmdArgs...).Run()
}
