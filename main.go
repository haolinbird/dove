package main

import (
	"doveclient/callbacks"
	"doveclient/config"
	"doveclient/logger"
	"doveclient/proc"
	"flag"
	"fmt"
	"os"
	"path"
	"runtime"
	"doveclient/util"
)

var validCliCmds = []string{"start", "restart", "stop", "status", "reload"}

var etcdclientVer string

func main() {
	var runBi, runAsBi, runAsDaemon, runAsDaemonL, showVersion bool
	var cmd, configFile string
	if !flag.Parsed() {
		args := flag.NewFlagSet("doveclient", flag.ExitOnError)
		var showHelp, debug, logSingleFile bool
		args.StringVar(&configFile, "c", "", "配置文件地址,默认:(*nix) /etc/DoveClient.conf, (win) "+path.Dir(os.Args[0])+string(os.PathSeparator)+"config.ini")
		args.BoolVar(&showHelp, "h", false, "显示此帮助信息")
		args.BoolVar(&showHelp, "help", false, "显示此帮助信息")
		args.BoolVar(&debug, "debug", false, "是否开启调试模式")
		args.BoolVar(&logSingleFile, "ls", false, "是否将所有类型的日志合并为同一个")
		args.BoolVar(&runBi, "bi", false, "运行备份实例")
		args.BoolVar(&runAsBi, "rbi", false, "是否作为备份实例运行")
		args.BoolVar(&runAsDaemon, "d", false, "是否作为后台程序运行")
		args.BoolVar(&runAsDaemonL, "detach", false, "是否作为后台程序运行")
		args.BoolVar(&showVersion, "V", false, "显示程序版本.")
		args.Usage = func() {
			showCliHelp(args)
		}
		args.Parse(os.Args[1:])
		if showVersion{
			println(getVersionDesc())
			os.Exit(0)
		}
		if showHelp {
			showCliHelp(args)
			os.Exit(0)
		}
		cmds := args.Args()
		if len(cmds) > 1 {
			fmt.Printf("Too many commands. Please see help with -h.\r\n")
			os.Exit(1)
		} else if len(cmds) == 1 {
			cmd = cmds[0]
		}

		if runAsDaemonL || cmd == "start" {
			runAsDaemon = true
		}
		config.SetConfigFile(configFile)
		config.SetDebug(debug)
		if logSingleFile {
			logger.SetSingleFile()
		}
	}

	dc := new(doveclient)
	if runAsBi {
		config.SetConfig("RunAsBi", true)
	}
	iErr := dc.Setup()
	if iErr == nil {
		logger.InitDefaultLogger()
		iErr = callbacks.InitCallbacks()
	}

	if iErr != nil {
		errMsg := fmt.Sprintf("Failed to setup client: %s\r\n", iErr)
		fmt.Fprint(os.Stdout, errMsg)
		logger.GetLogger("ERROR").Print(errMsg)
		os.Exit(2)
	}

	if runAsDaemon || runBi {
		dErr := dc.StartDaemonInstance()
		if dErr == nil {
			os.Exit(0)
		}
		errMsg := fmt.Sprintf("Failed to start client as daemon: %s", dErr)
		fmt.Fprint(os.Stdout, errMsg+"\r\n")
		logger.GetLogger("ERROR").Print(errMsg)
		os.Exit(1)
	}

	maxProcs := runtime.NumCPU()
	if maxProcs > 1 {
		maxProcs -= 1
	}
	runtime.GOMAXPROCS(maxProcs)

	switch cmd {
	case "restart":
		if dc.Restart() {
			fmt.Println("Success.")
			os.Exit(0)
		} else {
			fmt.Println("Failed")
			os.Exit(1)
		}
	case "stop":
		logger.GetLogger("INFO").Print("Stopping doveclient...")
		dc.StopClient()
		os.Exit(0)
	case "status":
		status, statusErr := dc.Status()
		if statusErr != nil {
			fmt.Printf("%s: %s\r\n", status, statusErr)
		} else {
			fmt.Print(status)
		}
		os.Exit(0)
	case "reload":
		fmt.Print("\"reload\" command not currently supported, it will be implemented in future version" + "\r\n")
		os.Exit(0)
	}

	if status, _ := dc.Status(); status == "running" {
		// 在重启的时候, 可以允许同时存在两个DoveClient进程.
		if os.Getenv("RESTART") == "" {
			fmt.Printf("There's already a doveclient instance running. Aborting.")
			os.Exit(1)
		}
	}

	iErr = proc.SavePid()

	if iErr != nil {
		fmt.Printf("Failed save proces ID: %s\r\n", iErr)
		os.Exit(1)
	}

	defer func() {
		if !dc.Stopped() {
			dc.StopClient()
		}
		err := recover()
		if err != nil {
			errMsg := fmt.Sprintf("doveclient encountered a fatal error: %s\n", err)
			logger.GetLogger("ERROR").Print(errMsg)
			panic(errMsg)
		}
	}()
	// 系统信号处理
	go dc.ProcessSysSignal()

	var err error
	// 过度性的备份实例不启动配置监听,只运行本地配置获取服务，以减少两个实例之间的资源竞争.
	if !config.GetConfig().RunAsBi {
		dc.StartPProf()
		err = dc.StartWatcher()
	}

	if err == nil {
		go dc.StartLocalServer()
		err = dc.WaitForExit()
	}

	if err != nil {
		errMsg := fmt.Sprintf("doveclient exited with err : %s.\r\n", err)
		fmt.Print(errMsg)
		logger.GetLogger("ERROR").Print(errMsg)
	} else {
		logger.GetLogger("INFO").Print("doveclient exited!")
	}
}

func showCliHelp(args *flag.FlagSet) {
	args.VisitAll(func(flag *flag.Flag) {
		format := "  -%s,--%s: 默认值：%s  %s\n"
		//        if _, ok := flag.Value.(*string); ok {
		//            // put quotes on the value
		//            format = "  -%s,--%s: 默认值：%q  %q\n"
		//        }
		fmt.Printf(format, flag.Name, flag.Name, flag.DefValue, flag.Usage)
	})
	fmt.Printf("  可用的命令为：%v\r\n", validCliCmds)
}

func getVersionDesc()string{
	sc, _ := config.ParseSecretSettings()
	md5s, _ := util.Md5File(os.Args[0])
	return fmt.Sprintf("DoveClient-%v(%v) with %v, with etcdclient %v", sc.Get("Ver"),md5s, runtime.Version(), etcdclientVer)
}
