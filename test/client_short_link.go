package main

import (
	"doveclient/commons"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var tc int64
var st time.Time
var chs []*chan int64
var ppo int

func processSysSignal() {
	// 系统信号监控通道
	osSingalChan := make(chan os.Signal, 1)

	signal.Notify(osSingalChan)

	// 在此阻塞，监控并处理系统信号
	for {
		osSingal := <-osSingalChan
		go func(sig *os.Signal) {
			switch *sig {
			case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
				fmt.Print("Quit signal received! exit...\n")
				for _, ch := range chs {
					*ch <- int64(1)
					tc += <-*ch
					*ch <- int64(2)
				}
				dd := time.Now().Sub(st)
				dt := dd.Seconds()
				fmt.Printf("并发数：%d,总耗时:%f, 完成请求次数:%d,  每次请求耗时：%f毫秒, 每秒完成次数:%f\n", ppo, dt, tc, (dt/float64(tc))*1000, float64(tc)/dt)
				os.Exit(0)
			case syscall.SIGUSR1:
				fmt.Printf("Current goroutines: %d\n", runtime.NumGoroutine())
			default:
				fmt.Printf("un-expected signal: %s(%d)\n", *sig, *sig)
			}
		}(&osSingal)
	}
}

func main() {
	dc := commons.doveclient{}
	_ = dc.Setup()
	config := dc.GetConfig()
	runtime.GOMAXPROCS(runtime.NumCPU() - 1)
	go processSysSignal()

	var pp int
	args := flag.NewFlagSet("client_test", flag.ContinueOnError)
	args.IntVar(&pp, "p", 100, "并发数")
	var output bool
	args.BoolVar(&output, "o", false, "是否打印服务端返回的信息")
	var showHelp bool
	args.BoolVar(&showHelp, "h", false, "显示此帮助信息")
	args.BoolVar(&showHelp, "help", false, "显示此帮助信息")
	args.Parse(os.Args[1:])
	if showHelp {
		args.PrintDefaults()
		return
	}
	ppo = pp
	st = time.Now()
	for ; pp > 0; pp-- {
		ch := make(chan int64)
		chs = append(chs, &ch)
		go func(ch *chan int64) {

			str := []byte("你好， 配置管理客户端！")
			headTag := []byte(strconv.Itoa(len(str)))
			headTag = append(headTag, make([]byte, 8-len(headTag))...)
			var loopCount int64
			for {
				conn, err := net.Dial("unix", config.ListenAddr)
				if err != nil {
					panic(fmt.Sprintf("连接失败：%s\n", err))
					return
				}
				select {
				case m := <-*ch:
					if m == 1 {
						*ch <- loopCount
					} else if m == 2 {
						conn.Write([]byte("CLOSE"))
						conn.Close()
						return
					}

				default:
					conn.Write(append(headTag, str...))
					// fmt.Printf("发送数据长度: %s\n", append(headTag, str...))
					resHeadTag := make([]byte, 8)
					start := 0
					for start < 8 {
						conn.SetReadDeadline(time.Now().Add(time.Second * 1))
						count, err := conn.Read(resHeadTag[start:])
						if err == nil {
							start += count
						} else {
							panic(fmt.Sprintf("Read error:%s\n", err))
							break
						}

					}

					bodyLen, _ := strconv.Atoi(strings.Trim(string(resHeadTag), "\000"))

					data := make([]byte, bodyLen)
					start = 0
					for {
						if start == bodyLen {
							break
						}
						conn.SetReadDeadline(time.Now().Add(time.Second * 1))
						count, err := conn.Read(data[start:])
						if err == nil {
							start += count
						} else {
							panic(fmt.Sprintf("Read error:%s\n", err))
						}
					}
					loopCount += 1
					if output {
						fmt.Printf("服务端返回: %s\n", data)
					}
					conn.Write([]byte("CLOSE"))
					conn.Close()
				}
			}
		}(&ch)
	}

	for {
		time.Sleep(time.Millisecond * 100)
	}

}
