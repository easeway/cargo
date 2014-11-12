package cargo

import (
	"errors"
	"fmt"
	"io"
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

func (d *docker) cmdOutput(arg ...string) ([]byte, error) {
	if out, err := d.cmdBase(arg...).Output(); err != nil {
		return nil, err
	} else {
		for _, line := range strings.Split(string(out), "\n") {
			d.logger.Debug("%s.o | %s", d.seq, line)
		}
		return out, err
	}
}

//func (d *docker) Inspect(cid, fmt string) (string, error) {
//	if output, err := d.cmd("inspect", "-f", fmt, cid).Output(); err == nil {
//		return string(output), err
//	} else {
//		return "", err
//	}
//}

func (d *docker) Pull(image string) error {
	return d.cmd("pull", image).Run()
}

func (d *docker) Create(cidfile string, args []string) (string, error) {
	cmdArgs := make([]string, len(args)+2)
	copy(cmdArgs[2:], args)
	cmdArgs[0] = "create"
	cmdArgs[1] = "--cidfile=" + cidfile

	if err := os.Remove(cidfile); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if output, err := d.cmdOutput(cmdArgs...); err == nil {
		return strings.Trim(string(output), " \n\r\t\f"), nil
	} else {
		return "", err
	}
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
			if out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", cid).Output(); err == nil {
				if strings.Trim(string(out), " \r\n\t\f") == "true" {
					return nil
				}
			}
			time.Sleep(time.Duration((i+1)*100) * time.Millisecond)
		}
		return errorStartTimeout
	}
}

//func (d *docker) RunDetached(cidfile string, args []string) (string, error) {
//	cmdArgs := make([]string, len(args)+3)
//	copy(cmdArgs[2:], args)
//	cmdArgs[0] = "run"
//	cmdArgs[1] = "-d"
//	cmdArgs[2] = "--cidfile=" + cidfile
//	if err := os.Remove(cidfile); err != nil && !os.IsNotExist(err) {
//		return "", err
//	}
//	if output, err := d.cmdOutput(cmdArgs...); err == nil {
//		return string(output), nil
//	} else {
//		return "", err
//	}
//}

//func (d *docker) Run(cidfile string, args []string, wg *sync.WaitGroup) (string, *exec.Cmd, error) {
//	cmdArgs := make([]string, len(args)+2)
//	copy(cmdArgs[2:], args)
//	cmdArgs[0] = "run"
//	cmdArgs[1] = "--cidfile=" + cidfile

//	if err := os.Remove(cidfile); err != nil && !os.IsNotExist(err) {
//		return err
//	}

//	cmd := d.cmd(cmdArgs...)
//	if err := cmd.Start(); err != nil {
//		return "", nil, err
//	}

//	for i := 0; i < 8; i++ {
//		if _, err := os.Stat(cidfile); err == nil {
//			break
//		}
//		time.Sleep(time.Duration((i+1)*100) * time.Millisecond)
//	}

//	if wg != nil {
//		wg.Add(1)
//		go func() {
//			cmd.Wait()
//			wg.Done()
//		}()
//	}

//	if cidBytes, err := ioutil.ReadFile(cidfile); err != nil {
//		return "", cmd, err
//	} else {
//		return string(cidBytes), cmd, nil
//	}
//}

func (d *docker) Stop(cid string) error {
	return d.cmd("stop", cid).Run()
}

func (d *docker) Remove(cid string) error {
	d.cmd("rm", cid).Run()
	// ignore the errors
	return nil
}

func (d *docker) Exec(cid string, args ...string) error {
	cmdArgs := make([]string, len(args)+2)
	cmdArgs[0] = "exec"
	cmdArgs[1] = cid
	copy(cmdArgs[2:], args)
	return d.cmd(cmdArgs...).Run()
}
