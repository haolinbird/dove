// 自定义日志处理包.需选运行config.Setup.
package logger

import (
	"doveclient/config"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"runtime"
	"strings"
	"time"
)

var loggers = map[string]*CoSafeLogger{}
var logAsSingleFile bool

type Logger struct {
	log.Logger
	out  io.Writer
	flag int
}

var loggersWRLock = make(chan int, 1)

var validLevels = []string{"INFO", "ERROR", "WARN", "DEFAULT"}

type DatetimeLogFileWriter struct {
	logDir               string
	fileNameSuffixFormat string //暂时无效
	level                string //INFO ERROR WARN DEFAULT
}

// 屏蔽输出
type NullWriter struct {
}

func (w *NullWriter) Write(p []byte) (n int, err error) {
	return 0, nil
}

type CoSafeLogger struct {
	rl      *log.Logger
	wlocker chan int
	flag    int
}

func (cl *CoSafeLogger) lock() {
	cl.wlocker <- 1
}

func (cl *CoSafeLogger) unlock() {
	<-cl.wlocker
}

func (cl *CoSafeLogger) getFormatHeader() string {
	if cl.flag != log.Lshortfile {
		return ""
	}
	_, file, lineno, ok := runtime.Caller(2)
	if !ok {
		file = "???"
		lineno = 0
	}
	return fmt.Sprintf("%v:%v:", path.Base(file), lineno)
}

func (cl *CoSafeLogger) Printf(format string, v ...interface{}) {
	cl.lock()
	cl.rl.Printf(cl.getFormatHeader()+format, v...)
	cl.unlock()
}

func (cl *CoSafeLogger) Print(v ...interface{}) {
	cl.lock()
	s := append([]interface{}{}, cl.getFormatHeader())
	s = append(s, v...)
	cl.rl.Print(s...)
	cl.unlock()
}

func (cl *CoSafeLogger) Println(v ...interface{}) {
	cl.lock()
	s := append([]interface{}{}, cl.getFormatHeader())
	s = append(s, v...)
	cl.rl.Println(s...)
	cl.unlock()
}

func getLoggerByLevel(level string) *CoSafeLogger {
	loggersWRLock <- 1
	if l, exists := loggers[level]; exists {
		<-loggersWRLock
		return l
	} else {
		<-loggersWRLock
		return nil
	}
}

func addToLoggers(level string, logger *CoSafeLogger) {
	loggersWRLock <- 1
	loggers[level] = logger
	<-loggersWRLock
}

// 初始化默认logger, 可屏蔽其它组件的日志直接输出
func InitDefaultLogger() {
	//默认writer
	if config.DebugEnabled() {
		defaultOut := DatetimeLogFileWriter{logDir: config.GetConfig().LOG_DIR, level: "DEFAULT"}
		log.SetOutput(&defaultOut)
		log.SetFlags(log.LstdFlags)
	} else {
		log.SetOutput(&NullWriter{})
	}
}

// 设置将各种类型的日志文件合并成一个
func SetSingleFile() {
	logAsSingleFile = true
}

func (w *DatetimeLogFileWriter) Write(p []byte) (n int, err error) {
	suffix := time.Now().Format("2006-01-02")
	pathSep := string(os.PathSeparator)
	var logFilename string
	if logAsSingleFile {
		logFilename = fmt.Sprintf("%s%sall.%s.log", strings.TrimRight(w.logDir, pathSep), pathSep, suffix)
		p = append([]byte("["+w.level+"] "), p...)
	} else {
		logFilename = fmt.Sprintf("%s%s%s.%s.log", strings.TrimRight(w.logDir, pathSep), pathSep, strings.ToLower(w.level), suffix)
	}
	fd, err := os.OpenFile(logFilename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return 0, errors.New(fmt.Sprintf("Failed to open log file: %s with error: ", logFilename, err))
	}

	defer fd.Close()
	return fd.Write(p)
}

func CreateLogger(level string) *CoSafeLogger {
	out := DatetimeLogFileWriter{logDir: config.GetConfig().LOG_DIR, level: level}
	return &CoSafeLogger{log.New(&out, "", log.LstdFlags), make(chan int, 1), log.Lshortfile}
}

func GetLogger(level string) *CoSafeLogger {
	if !func(n string) bool {
		for _, dLevel := range validLevels {
			if level == dLevel {
				return true
			}
		}
		return false
	}(level) {
		panic(fmt.Sprintf("Invalid logger level(%s), please refer %v", level, validLevels))
	}
	var logger *CoSafeLogger
	if logger = getLoggerByLevel(level); logger == nil {
		logger = CreateLogger(level)
		addToLoggers(level, logger)
	}
	return logger
}
