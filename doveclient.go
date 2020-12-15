package main

import (
	"bytes"
	"doveclient/config"
	"doveclient/configwatcher"
	"doveclient/connection"
	"doveclient/logger"
	"doveclient/proc"
	"doveclient/util"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var clientStopped bool

type doveclient struct {
}

var serverErrorChan = make(chan error, 1)

// 初始化
func (c *doveclient) Setup() error {
	return config.Setup()
}

func (c *doveclient) WaitForExit() (err error) {
	proc.Wait()
	clientStopped = true
	select {
	case err = <-serverErrorChan:
	default:
	}
	return
}

func (c *doveclient) GetConfig() config.Config {
	return config.GetConfig()
}

func (c *doveclient) ProcessSysSignal() {
	proc.ProcessSysSignal()
}

func (c *doveclient) StopClient() {
	var pid int
	var err error
	if config.GetConfig().RunAsBi {
		pid = os.Getpid()
	} else {
		pid, err = proc.GetPid()
		if err != nil {
			logger.GetLogger("ERROR").Printf("Failed to get doveclient process ID: %s", err)
			return
		}
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to find doveclient process: %s", err)
		return
	}
	err = p.Signal(syscall.SIGQUIT)
	if err != nil {
		logger.GetLogger("WARN").Printf("Error in stopping doveclient: %v", err)
	}
}

func (c *doveclient) StartPProf() {
	connection.StartPProf()
}

func (c *doveclient) StartWatcher() error {
	return configwatcher.Start()
}

func (c *doveclient) StartLocalServer() (err error) {
	err = connection.StartLocalServer()
	serverErrorChan <- err
	if err != nil {
		c.StopClient()
	}
	return
}

func (c *doveclient) Stopped() bool {
	return clientStopped
}

func getTransiInstanceExecBin() string {
	return os.Args[0] + "-bi"
}

func startTransiIntance() (success bool, p *os.Process) {
	logger.GetLogger("INFO").Print("Starting doveclient backup instance...")
	bin := os.Args[0]
	binBi := getTransiInstanceExecBin()
	out, err := util.ExtCall("cp", bin, binBi)
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to create backup instance: %s : %s", err, out)
		return
	}
	biOut := bytes.Buffer{}
	cmd := exec.Command("bash", "-c", binBi+" -bi -rbi")
	cmd.Stdout = &biOut
	cmd.Stderr = &biOut
	err = cmd.Start()
	go func() {
		cmd.Wait()
	}()
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to start backup instance: %s : %s", err, biOut.String())
		return
	} else {
		var biPidB []byte
		var biPid int
		waitTimeOut := time.Now().Add(time.Millisecond * 5000)
		for waitTimeOut.After(time.Now()) {
			biPidB, err = ioutil.ReadFile(config.GetBiPidFile())
			if err == nil {
				biPid, err = strconv.Atoi(string(biPidB))
				if err != nil {
					err = errors.New(fmt.Sprintf("Failed to convert bi pid: %v", err))
					break
				} else {
					p, err = os.FindProcess(biPid)
				}
				if err != nil {
					err = errors.New(fmt.Sprintf("Failed to find bi process: %v", err))
				}
				break
			}
			time.Sleep(time.Millisecond * 100)
		}
		if p != nil {
			success = true
			logger.GetLogger("INFO").Print("DoveClient backup instance started successfully.")
		} else {
			logger.CreateLogger("ERROR").Printf("Failed to start DoveClient backup instance: %v: %v", err, biOut.String())
		}
	}
	return
}

func (c *doveclient) Restart() bool {
	logger.GetLogger("INFO").Print("Restarting doveclient...")
	
	pid, err := proc.GetPid()
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to get previous instance PID, is it running? abort restarting...: %s", err)
		return false
	}

	p, err := os.FindProcess(pid)
	if err != nil {
		logger.GetLogger("ERROR").Printf("System error: %s\r\n", err)
		return false;
	}

	err = p.Signal(syscall.SIGUSR2)
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failure to restart signal: %s\r\n", err)
		return false;
	}

	time.Sleep(time.Second * 3)
	// 检查进程有没有重启成功.
	pid, err = proc.GetPid()
	if err != nil {
		logger.GetLogger("ERROR").Printf("Process startup failure(PID files do not exist): %s\r\n", err)
		return false
	}

	p, err = os.FindProcess(pid)
	if err != nil {
		logger.GetLogger("ERROR").Printf("System error: %s\r\n", err)
		return false
	}

	err = p.Signal(syscall.Signal(0))
	if err != nil {
		logger.GetLogger("ERROR").Printf("Process restart failed: %s\r\n", err)
		return false
	}

	return true;


	/*logger.GetLogger("INFO").Print("Restarting doveclient...")
	pid, err := proc.GetPid()
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to get previous instance PID, is it running? abort restarting...: %s", err)
		return false
	}
	success, biProc := startTransiIntance()
	if success {
		for maxTestCount := 14; maxTestCount > 0; maxTestCount-- {
			_, err = os.Stat(config.GetConfig().ListenAddrTransi)
			if err != nil {
				time.Sleep(time.Millisecond * 800)
			} else {
				break
			}
		}
	} else {
		logger.GetLogger("ERROR").Print("Abort restarting...")
		return false
	}

	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to get status for backup instance. abort restarting.: %s", err)
		return false
	}

	logger.GetLogger("INFO").Print("Stopping previous instance...")
	preProc, err := os.FindProcess(pid)
	if err != nil || preProc.Signal(syscall.SIGQUIT) != nil {
		logger.GetLogger("ERROR").Printf("Failed to find previous instance process: %s, ignored, continue restarting...", err)
	}

	var tErr error
	preProcExited := false
	for maxTestCount := 14; maxTestCount > 0; maxTestCount-- {
		_, tErr = os.Stat(config.GetConfig().ListenAddr)
		if tErr == nil || !os.IsNotExist(tErr) {
			time.Sleep(time.Millisecond * 800)
		} else {
			preProcExited = true
			break
		}
	}

	if !preProcExited {
		logger.GetLogger("ERROR").Printf("Failed to stop previous instance, abort! Please check the logs and fix them ASAP!%v", tErr)
		return false
	} else {
		logger.GetLogger("INFO").Print("Previous instance exited. Now starting new instance...")
	}

	err = c.StartDaemonInstance()
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to start new instance: %s", err)
		return false
	}

	for maxTestCount := 14; maxTestCount > 0; maxTestCount-- {
		_, err = os.Stat(config.GetConfig().ListenAddr)
		if err != nil {
			time.Sleep(time.Millisecond * 800)
		} else {
			break
		}
	}
	if err != nil {
		logger.GetLogger("ERROR").Printf("Startning new instance not complete!! please check logs and fix errors ASAP!!!:%s", err)
		return false
	}

	logger.GetLogger("INFO").Print("New instance started...stopping temporary backup instance...")

	err = biProc.Signal(syscall.SIGQUIT)
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to stop temporary backup instance: %s, please check logs and fix errors ASAP!!", err)
		return false
	}

	biProcExited := false

	for maxTestCount := 14; maxTestCount > 0; maxTestCount-- {
		_, tErr = os.Stat(config.GetConfig().ListenAddrTransi)
		if tErr == nil || !os.IsNotExist(tErr) {
			time.Sleep(time.Millisecond * 800)
		} else {
			biProcExited = true
			break
		}
	}

	if !biProcExited {
		logger.GetLogger("ERROR").Printf("Failed to exit backup instance, please check logs and fix errors ASAP!!%v", tErr)
		return false
	} else {
		os.Remove(getTransiInstanceExecBin())
	}
	logger.GetLogger("INFO").Print("Backup instance stopped successfully!")
	logger.GetLogger("INFO").Print("DoveClient restared successfully!")
	return true*/
}

func (c *doveclient) Status() (string, error) {
	pid, err := proc.GetPid()
	if err != nil {
		return "failed", errors.New(fmt.Sprintf("Failed to get pid, not running?(%s)", err))
	}
	clientProc, err := os.FindProcess(pid)
	if err != nil {
		return "failed", errors.New(fmt.Sprintf("Failed to lookup client process: %s", err))
	}
	err = clientProc.Signal(syscall.Signal(0))
	if err != nil {
		return "stopped", nil
	}
	return "running", nil
}

func (_ *doveclient) StartDaemonInstance() error {
	var orginArgs []string
	argLen := len(os.Args)
	for i := 1; i < argLen; i++ {
		switch os.Args[i] {
		case "-d", "--d", "-d=true", "-detach", "--detach", "-detach=true", "restart", "start", "-bi", "--bi":
		default:
			orginArgs = append(orginArgs, os.Args[i])
		}
	}

	shellArgs := []string{fmt.Sprintf("sudo -u %v %v", config.GetConfig().DAEMON_USER, os.Args[0])}
	shellArgs = append(shellArgs, orginArgs...)
	cmd := exec.Command("bash", "-c", strings.Join(shellArgs, " "))
	out := bytes.Buffer{}
	cmd.Stdout = &out
	cmd.Stderr = &out

	var err, cmdErr, sErr error
	go func(cmd *exec.Cmd) {
		cmdErr = cmd.Run()
	}(cmd)
	waitTimeOut := time.Now().Add(time.Millisecond * 15000)
	var cProc *os.Process
	var pid int
	for waitTimeOut.After(time.Now()) {
		time.Sleep(time.Millisecond * 200)
		if cProc == nil {
			pid, err = proc.GetPid()
			if err == nil {
				cProc, err = os.FindProcess(pid)
			}
		}

		if err == nil {
			// 检查客户端进程是否已经启动
			err = cProc.Signal(syscall.Signal(0))
			if err == nil {
				// 强行detach 执行sudo 的进程
				sErr = cmd.Process.Signal(syscall.Signal(syscall.SIGKILL))
				break
			}
		}
	}
	if err != nil || cmdErr != nil || sErr != nil {
		return errors.New(fmt.Sprintf("cmd: %v, errors: %v : %v : %v", out.String(), sErr, cmdErr, err))
	}
	return nil
}
