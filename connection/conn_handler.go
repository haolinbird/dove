package connection

import (
	"bytes"
	"doveclient/config"
	"doveclient/handler"
	"doveclient/logger"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"time"
	"net/http"
	_ "net/http/pprof"
)

const (
	HEAD_TAG_LENGTH   = 8
	STATUS_TAG_LENGTH = 8
)

var current_connctions = 0

var byte0 = byte(0)

var tagClose = []byte("CLOSE")

var counterCh = make(chan int32, 100)
var masterCommCh = make(chan int, 1)
var readConnCountCh = make(chan int, 1)
var stoppingLocalServer = false
var localServerStoppingStateQueryLock = make(chan int, 1)
var sso *StoppingSigalOp

var errMsgOutOfMaxConn = fmt.Sprintf("Too many connections, out of %d.\n", config.MAX_CONNECTIONS)

var pprofListener *net.Listener
// rpc listen
var server net.Listener

type ProcessResult struct {
	Result []byte
	Error  error
}

type Request struct {
	Closed bool
	Body   []byte
	Err    error
}

type StoppingSigalOp struct {
	lockChan      *chan bool
	userCounter   uint64
	stoppingSigal chan bool
}

func init() {
	ssoLockChan := make(chan bool, 1)
	sso = &StoppingSigalOp{lockChan: &ssoLockChan, stoppingSigal: make(chan bool, 100)}
}

func (sso *StoppingSigalOp) lock() {
	*sso.lockChan <- true
}

func (sso *StoppingSigalOp) unlock() {
	<-*sso.lockChan
}

func (sso *StoppingSigalOp) Use() {
	sso.lock()
	defer sso.unlock()
	sso.userCounter += 1
}

func (sso *StoppingSigalOp) Unuse() {
	sso.lock()
	defer sso.unlock()
	sso.userCounter -= 1
}

func (sso *StoppingSigalOp) SendSignal() {
	sso.lock()
	defer sso.unlock()
	for c := sso.userCounter; c > 0; c-- {
		 sso.stoppingSigal <- true
	}
}

// status: 不超过8字节
// data: 一般为json序列化后的数据
func response(conn *net.Conn, status string, data []byte) error {
	statusTag := []byte(status)
	statusTag = append(statusTag, make([]byte, STATUS_TAG_LENGTH-len(statusTag))...)

	dataLenBtye := strconv.Itoa(len(data))
	padLen := 8 - len(dataLenBtye)
	headTag := append([]byte(dataLenBtye), make([]byte, padLen)...)
	_, err := (*conn).Write(headTag)
	if err != nil {
		return err
	}
	_, err = (*conn).Write(statusTag)
	if err != nil {
		return err
	}
	_, err = (*conn).Write(data)
	return err
}

func isTagClose(tag []byte) bool {
	return bytes.Compare(tagClose, tag) == 0
}

func parseHeadTagLength(tag []byte) []byte {
	var headerLength []byte
	for i := 0; i < HEAD_TAG_LENGTH && tag[i] != byte0; i++ {
		headerLength = append(headerLength, tag[i])
	}
	return headerLength
}

func readRequest(conn *net.Conn, dataChan chan *Request) {
	var count int
	var err error
	var headComplete, closed bool
	headerTag := make([]byte, HEAD_TAG_LENGTH)
	result := &Request{}
	defer func() {
		result.Err = err
		result.Closed = closed
		dataChan <- result
	}()
	start := 0

	for start < HEAD_TAG_LENGTH {
		if start == 0 {
			// 设置客户端空闲超时
			(*conn).SetDeadline(time.Now().Add(config.CLIENT_MAX_IDLE))
		} else {
			// 设置客户端返回超时时间.
			(*conn).SetReadDeadline(time.Now().Add(config.CLIENT_TIMEOUT))
		}
		count, err = (*conn).Read(headerTag[start:])
		if err != nil {
			headComplete = bytes.Compare(tagClose, headerTag[0:5]) == 0
			// 如果客户端发送了CLOSE标识，或者直接关闭连接都认为是正常关闭链接，不再进一步进行确认。
			if start > 0 && !headComplete {
				// 数据未读完, 且不为关闭连接命令
				err = errors.New(fmt.Sprintf("client data length read error: %s", err))
			} else {
				err = nil
				closed = true
			}
			return
		}
		start += count
	}

	headerTagLength := parseHeadTagLength(headerTag)
	closed = isTagClose(headerTagLength)
	if closed {
		return
	}

	var bodyLength int
	bodyLength, err = strconv.Atoi(string(headerTagLength))

	if err != nil {
		err = errors.New(fmt.Sprintf("Request length parse error: %s", err))
		return
	}

	if bodyLength < 1 {
		err = errors.New("数据长度小于1.")
		return
	}
	result.Body = make([]byte, bodyLength)
	start = 0
	for start < bodyLength {
		(*conn).SetReadDeadline(time.Now().Add(config.CLIENT_TIMEOUT))
		count, err = (*conn).Read(result.Body[start:])
		if err != nil {
			if err != io.EOF && start < bodyLength-1 {
				err = errors.New(fmt.Sprint("client data read premature"))
			} else {
				err = errors.New(fmt.Sprintf("read client data error: %s", err))
			}
			return
		}
		start += count
	}
	return
}

func connCounter(ch *chan int32) {
	var m int32
	for {
		m = <-counterCh
		readConnCountCh <- 1
		switch m {
		case 1:
			current_connctions += 1
		case -1:
			current_connctions -= 1
		}
		<-readConnCountCh
	}
}

func GetCurrentConnectionCount() int {
	readConnCountCh <- 1
	c := current_connctions
	<-readConnCountCh
	return c
}

func setLocalServerStoppingState() {
	localServerStoppingStateQueryLock <- 1
	stoppingLocalServer = true
	<-localServerStoppingStateQueryLock
}

func isServerStopping() bool {
	localServerStoppingStateQueryLock <- 1
	state := stoppingLocalServer
	<-localServerStoppingStateQueryLock
	return state
}

// 启动pprof分析服务.
func StartPProf() {
	go func() {
		if config.GetConfig().PPROF_LISTEN == "" {
			return
		}

		l, err := net.Listen("tcp", config.GetConfig().PPROF_LISTEN)
		// err := http.ListenAndServe(config.GetConfig().PPROF_LISTEN, nil)
		if err != nil {
			logger.GetLogger("ERROR").Printf("Startup Performance Analysis service failed: %s", err)
			return
		}

		pprofListener = &l

		svr := http.Server{}
		svr.SetKeepAlivesEnabled(false)
		err = svr.Serve(l)
		if err != nil {
			pprofListener = nil
		}
	}()
}

// 关闭pprof分析服务.
func StopPProf() {
	if pprofListener != nil {
		err := (*pprofListener).Close()
		if err != nil {
			logger.GetLogger("ERROR").Printf("Stopping Performance Analysis service failed: %s\r\n", err)
			return
		}

		pprofListener = nil
	}
}

func StopLocalServer() {
	// 如果有多次调用，后边的协程将被阻塞
	masterCommCh <- 1
	// 以下函数调用顺序不能错.
	setLocalServerStoppingState()
	sso.SendSignal()
	for GetCurrentConnectionCount() > 0 {
		time.Sleep(time.Millisecond * 10)
	}
}

func GetLocalServerFd() (*os.File, error) {
	v, ok := server.(*net.TCPListener)
	if ok {
		f, err := v.File()
		if err != nil {
			return nil, err
		}

		return f, nil
	}

	v1, ok := server.(*net.UnixListener)
	if ok {
		f, err := v1.File()
		if err != nil {
			return nil, err
		}

		return f, nil
	}

	return nil, errors.New("Unknown listener")
}

// 启动本地配置服务
func StartLocalServer() error {
	var addr, netType string
	if config.GetConfig().RunAsBi {
		addr = config.GetConfig().ListenAddrTransi
	} else {
		addr = config.GetConfig().ListenAddr
	}
	switch runtime.GOOS {
	case "windows":
		netType = "tcp"
	default:
		netType = "unix"
	}
	
	var err error
	// 平滑重启的时候,复用上一个进程的fd.
	if os.Getenv("RESTART") == "1" {
		f := os.NewFile(3, "")
		server, err = net.FileListener(f)
	} else {
		// 进程意外终止的时候.sock还在.
		if netType == "unix" {
			os.Remove(addr)
		}
		server, err = net.Listen(netType, addr)
	}

	if err == nil && netType == "unix" {
		err = os.Chmod(addr, 0777)
		if err != nil {
			err = errors.New(fmt.Sprintf("Failed to change unixdomain listen mode: %s", err))
		}
	}
	if err != nil {
		return err
	}
	go connCounter(&counterCh)
	go receiveStats()
	if !config.GetConfig().RunAsBi {
		go startConsole()
	}
	sso.Use()
	for {
		inConn, err := server.Accept()
		if err != nil {
			logger.GetLogger("ERROR").Printf("Accept connection error: %s. Retry in 0.1 seconds.", err)
			time.Sleep(time.Millisecond * 100)
			continue
		}
		if GetCurrentConnectionCount() > config.MAX_CONNECTIONS {
			go func(inConn *net.Conn) {
				logger.GetLogger("ERROR").Print(errMsgOutOfMaxConn)
				err := response(inConn, "failed", []byte(errMsgOutOfMaxConn))
				if err != nil {
					logger.GetLogger("ERROR").Printf("Error when response to client: %s", err)
				}
				(*inConn).Close()
			}(&inConn)
			time.Sleep(time.Millisecond * 10)
			continue
		}
		go HandleConn(&inConn)
		counterCh <- int32(1)
		var exit bool
		select {
		case exit = <-sso.stoppingSigal:
		default:
		}
		if exit {
			break
		}
	}
	return nil
}

// 处理获取配置的连接请求
func HandleConn(conn *net.Conn) {
	defer func() {
		(*conn).Write([]byte("CLOSE"))
		time.Sleep(time.Millisecond * 20)
		err := (*conn).Close()
		if err != nil {
			//	logger.Printf("连接关闭出错: %s", err)
		}
		counterCh <- int32(-1)
	}()

	if isServerStopping() {
		return
	}
	sso.Use()
	requestDataChan := make(chan *Request)
	go readRequest(conn, requestDataChan)
	for {
		select {
		case <-sso.stoppingSigal:
			return
		case request := <-requestDataChan:
			if request.Closed {
				sso.Unuse()
				return
			}
			if request.Err != nil {
				logger.GetLogger("ERROR").Printf("Client data read error: %s", request.Err)
				sso.Unuse()
				return
			}

			re, err := processRequest(request.Body)
			var status string
			if err == nil {
				status = "ok"
			} else {
				status = "failed"
				re, _ = json.Marshal(err.Error())
			}
			err = response(conn, status, re)
			if err != nil {
				logger.GetLogger("ERROR").Printf("Response write error: %s\n", err)
				sso.Unuse()
				return
			} else {
				go readRequest(conn, requestDataChan)
			}
		}
	}
}

// requestdata
// map[sring]interface{}
//request["cmd"]
//request["args"]interface...
func processRequest(requestData []byte) (re []byte, err error) {
	var clientData map[string]interface{}
	err = json.Unmarshal(requestData, &clientData)
	if err != nil {
		err = errors.New(fmt.Sprintf("Failed to decode client data: %s\nRaw data: %s", err, requestData))
		return
	}
	pqCh := make(chan ProcessResult)
	go runRequestHandler(pqCh, clientData["cmd"].(string), clientData["args"].(map[string]interface{}))
	pTimeout := time.After(time.Second * time.Duration(config.PROCESS_TIMEOUT))
	select {
	case result := <-pqCh:
		re = result.Result
		err = result.Error
	case <-pTimeout:
		err = errors.New("Process timeout.")
		logger.GetLogger("WARN").Printf("Cmd: %s: %v", clientData["cmd"], err)
	}
	return
}

func runRequestHandler(reCh chan ProcessResult, cmd string, args map[string]interface{}) {
	go sendStats(StatsMessage{cmd, CONNACTION_ACCEPT, CONNACTION_STATUS_OK})
	re, err := handler.Run(cmd, args)
	reCh <- ProcessResult{re, err}
	var finStatus ConnActionStatus
	if err != nil {
		finStatus = CONNACTION_STATUS_FAILED
	} else {
		finStatus = CONNACTION_STATUS_OK
	}
	go sendStats(StatsMessage{cmd, CONNACTION_PROCESS_FIN, finStatus})
}

// 向数据统计器发送统计信息
func sendStats(msg StatsMessage) {
	getStatsRWLock()
	statsInChan <- msg
	releaseStatsRWLock()
}
