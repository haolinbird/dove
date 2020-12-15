package main

import (
	"doveclient/config"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"runtime"
	rDebug "runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var tc int64
var st time.Time
var chs []*chan int64
var ppo int

const (
	NET_READ_TIMEOUT = time.Second * 10
)

func processSysSignal() {
	// 系统信号监控通道
	osSingalChan := make(chan os.Signal, 1)

	signal.Notify(osSingalChan)
	rDebug.SetMaxThreads(20000)
	// 在此阻塞，监控并处理系统信号
	for {
		osSingal := <-osSingalChan
		go func(sig *os.Signal) {
			switch *sig {
			case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
				fmt.Print("Quit signal received! collecting statistics...please wait...exiting...\n")
				tp := len(chs)
				const collect_step = 10
				chsCount := int(math.Ceil(float64(tp) / collect_step))
				resChs := make([]*chan int64, chsCount)
				var tc int64
				for i := 0; i < chsCount; i++ {
					sCh := make(chan int64)
					resChs[i] = &sCh
					go func(ch *chan int64, i int) {
						uper := (i+1)*collect_step - 1
						if uper > tp {
							uper = tp
						}
						var cTc int64
						for _, cC := range chs[i*collect_step : uper] {
							*cC <- int64(1)
							cTc += <-*cC
							*cC <- int64(2)
						}
						*ch <- cTc
					}(resChs[i], i)
				}

				for _, ch := range resChs {
					tc += <-*ch
					// cp := float64(i*collect_step) / float64(tp)
					// if (i > 0) || i >= chsCount {
					// 	fmt.Printf("collected : %v%%\n", cp*100)
					// }
				}
				dd := time.Now().Sub(st)
				dt := dd.Seconds()
				fmt.Printf("\n并发数：%d,总耗时:%f, 完成请求次数:%d,  每次请求耗时：%f毫秒, 每秒完成次数:%f\n", ppo, dt, tc, (dt/float64(tc))*1000, float64(tc)/dt)
				os.Exit(0)
			case syscall.SIGUSR1:
				fmt.Printf("Current goroutines: %d\n", runtime.NumGoroutine())
			default:
				fmt.Printf("un-expected signal: %s(%d)\n", *sig, *sig)
			}
		}(&osSingal)
	}
}

func genReqData(data map[string]interface{}) []byte {
	bStr, _ := json.Marshal(data)
	headTag := []byte(strconv.Itoa(len(bStr)))
	headTag = append(headTag, make([]byte, 8-len(headTag))...)
	return append(headTag, bStr...)
}

func main() {
	var pp int
	args := flag.NewFlagSet("client_test", flag.ContinueOnError)
	args.IntVar(&pp, "p", 100, "并发数")
	var output bool
	var testConfigName string
	args.BoolVar(&output, "o", false, "是否打印服务端返回的信息")
	var showHelp bool
	args.BoolVar(&showHelp, "h", false, "显示此帮助信息")
	args.BoolVar(&showHelp, "help", false, "显示此帮助信息")
	args.StringVar(&testConfigName, "cn", "", "测试的配置key名称")
	args.Parse(os.Args[1:])
	if showHelp {
		args.PrintDefaults()
		return
	}
	cErr := config.Setup()
	if cErr != nil{
		panic(cErr)
	}
	config := config.GetConfig()
	runtime.GOMAXPROCS(runtime.NumCPU() - 1)
	go processSysSignal()

	ppo = pp
	st = time.Now()
	for ; pp > 0; pp-- {
		ch := make(chan int64)
		chs = append(chs, &ch)
		go func(ch *chan int64) {
			conn, err := net.Dial("unix", config.ListenAddr)
			if err != nil {
				panic(fmt.Sprintf("连接失败：%s: %+v\n", err, config))
				return
			}

			var loopCount int64
			for {
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
					reqData := make(map[string]interface{})
					reqData["cmd"] = "GetProjectConfig"
					if testConfigName == "" {
						testConfigName = "Koubei.salesPromotionConfig.StartTime"
					}
					reqData["args"] = map[string]interface{}{"name": testConfigName}
					wn, werr := conn.Write(genReqData(reqData))
					if werr != nil {
						fmt.Printf("Failed to send data to server(%d): %s\n now close connection.", wn, err)
						conn.Close()
						return
					}
					// fmt.Printf("发送数据长度: %s\n", append(headTag, str...))
					resHeadTag := make([]byte, 16)
					start := 0
					for start < 16 {
						conn.SetReadDeadline(time.Now().Add(NET_READ_TIMEOUT))
						count, err := conn.Read(resHeadTag[start:])
						if err == nil {
							start += count
						} else {
							panic(fmt.Sprintf("Data length Read error, connection may closed unexpectedly:%s\nAlread read(%d): %v\n", err, start, resHeadTag))
							break
						}

					}

					bodyLen, _ := strconv.Atoi(strings.Trim(string(resHeadTag[0:7]), "\000"))
					data := make([]byte, bodyLen)
					var dData interface{}
					start = 0
					for {
						if start == bodyLen {
							if output {
								err = json.Unmarshal(data, &dData)
								if err != nil {
									fmt.Printf("数据解析错误: %s\n", err)
								}
							}
							break
						}
						conn.SetReadDeadline(time.Now().Add(NET_READ_TIMEOUT))
						count, err := conn.Read(data[start:])
						if err == nil {
							start += count
						} else {
							panic(fmt.Sprintf("Read error:%s\nAready read: %s", err, data))
						}
					}
					loopCount += 1
					if output {
						fmt.Printf("服务端返回数据长度：%v, 状态: %s\n数据: %s\n", bodyLen, resHeadTag[8:], data)
					}
				}
			}
		}(&ch)
	}

	for {
		time.Sleep(time.Millisecond * 100)
	}

}
