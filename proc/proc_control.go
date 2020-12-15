package proc

import (
	"doveclient/config"
	"doveclient/connection"
	"doveclient/logger"
	diskStorage "doveclient/storage/leveldb"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
	"os/exec"
)

// 发送退出信号通道
var exitChan = make(chan int)

func Wait() {
	<-exitChan
}

// 系统信号处理
func ProcessSysSignal() {
	// 系统信号监控通道
	osSingalChan := make(chan os.Signal, 10)

	signal.Notify(osSingalChan)

	var sigusr1 = syscall.Signal(0xa)

	// 在此阻塞，监控并处理系统信号
	for {
		osSingal := <-osSingalChan
		go func(sig *os.Signal) {
			switch *sig {
			case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
				logger.GetLogger("INFO").Print("Quit signal received! exit...\n")
				Stop()
				exitChan <- 1
			case syscall.SIGALRM:
				logger.GetLogger("INFO").Print("Received manual GC signal, starting free OS memory!")
				gcStats := debug.GCStats{}
				debug.ReadGCStats(&gcStats)
				gcStatsOut, _ := json.MarshalIndent(gcStats, "", "")
				fmt.Fprintf(os.Stdout, "%s\r\n", gcStatsOut)
				debug.FreeOSMemory()
			case sigusr1:
				fmt.Printf("Current goroutines: %d\n", runtime.NumGoroutine())
				// case syscall.SIGUSR2:
				// 	fmt.Printf("Configs: %s\n", config.GetConfig())
			case syscall.SIGHUP:
				curDebugMode := !config.DebugEnabled()
				logger.GetLogger("INFO").Printf("Received force toggle DEBUG mode signal, setting DEBUG mode to : %v", curDebugMode)
				config.SetDebug(curDebugMode)
			case syscall.SIGUSR2:
				logger.GetLogger("INFO").Print("Restart signal received! restarting...\n")

				rpcFd, err := connection.GetLocalServerFd()
				if err != nil {
					logger.GetLogger("ERROR").Printf("Process restart failed: %s\r\n", err)
					return
				}

				// 先关闭, 防止端口冲突.
				connection.StopPProf()
				// 关闭控制台服务,防止端口冲突.
				connection.StopConsole()

				var cmd *exec.Cmd
				if len(os.Args) > 1 {
					cmd = exec.Command(os.Args[0], os.Args[1:]...)
				} else {
					cmd = exec.Command(os.Args[0])
				}

				cmd.ExtraFiles = []*os.File{rpcFd}

				envs := os.Environ()
				envs = append(envs, "RESTART=1")
				cmd.Env = envs

				err = cmd.Start()
				if err != nil {
					logger.GetLogger("ERROR").Printf("Failure to start a new process: %s\r\n", err)
					return
				}
				// 确保这个进程不是刚启动就挂掉了.
				time.Sleep(time.Second * 2)
				p, err := os.FindProcess(cmd.Process.Pid)
				if err != nil {
					logger.GetLogger("ERROR").Printf("System error: %s\r\n", err)
					return
				}

				err = p.Signal(syscall.Signal(0))
				if err != nil {
					logger.GetLogger("ERROR").Printf("Process restart failed: %s\r\n", err)
					return
				}

				SafeStop()
				exitChan <- 1
			default:
				if config.DebugEnabled() {
					logger.GetLogger("INFO").Printf("un-expected signal: %s(%d)\n", *sig, *sig)
				}
			}
		}(&osSingal)
	}
}

// 平滑重启用这个进行stop.
func SafeStop() {
	connection.StopLocalServer()
	diskStorage.Close()
}

func Stop() {
	connection.StopLocalServer()
	diskStorage.Close()
	if config.GetConfig().RunAsBi {
		os.Remove(config.GetConfig().ListenAddrTransi)
	} else {
		os.Remove(config.GetConfig().ListenAddr)
	}
	RemovePid()
}

func SavePid() error {
	return ioutil.WriteFile(config.GetConfig().PidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func RemovePid() error {
	return os.Remove(config.GetConfig().PidFile)
}

func GetPid() (int, error) {
	pidB, err := ioutil.ReadFile(config.GetConfig().PidFile)
	if err != nil {
		return -1, err
	}
	return strconv.Atoi(strings.TrimSpace(string(pidB)))
}
