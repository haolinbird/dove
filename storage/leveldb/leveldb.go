package leveldb

import (
	"errors"
	"fmt"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"time"
)

var dbRWLock = make(chan int, 1)

// dbObjCreateLock Db对象单例锁，只使用一次
var dbObjCreateLock = make(chan int, 1)
var db *Db

type Config struct {
	Dsn string
}

type Conn struct {
	rconn *leveldb.DB
}
type Db struct {
	Conn *Conn
}

var config Config
var initialized bool

func (conn *Conn) reConnect() error {
	rconn, err := createRealConn()
	if err != nil {
		return err
	}
	conn.rconn = rconn
	return nil
}

func (conn *Conn) Get(key []byte, ro *opt.ReadOptions) ([]byte, error) {
	if !conn.getDbRWLock() {
		return nil, NewError(ErrorDbLockFailed, "Failed to get db connection, cannot accqurie lock.")
	}
	d, e := conn.rconn.Get(key, ro)
	if e == leveldb.ErrClosed {
		if conn.reConnect() != nil {
			return nil, e
		}
		return conn.rconn.Get(key, ro)
	}
	conn.releasedbRWLock()
	return d, e
}

func (conn *Conn) Put(key, value []byte, wo *opt.WriteOptions) error {
	if !conn.getDbRWLock() {
		return NewError(ErrorDbLockFailed, "Failed to get db connection, cannot accqurie lock.")
	}
	e := conn.rconn.Put(key, value, wo)
	if e == leveldb.ErrClosed {
		if conn.reConnect() != nil {
			return e
		}
		return conn.rconn.Put(key, value, wo)
	}
	conn.releasedbRWLock()
	return e
}

// 此方法在整个程序中只调用一次。
func InitDb(c Config) error {
	config = c
	initialized = true
	return nil
}
func IsIntialized() bool {
	return initialized
}
func (conn *Conn) getDbRWLock() bool {
	lockWaitTimeout := time.Millisecond * 1500
	lockWaitStart := time.Now()
	hadLock := false
	for !hadLock {
		select {
		case dbRWLock <- 1:
			hadLock = true
		default:
			if time.Now().Sub(lockWaitStart) > lockWaitTimeout {
				return false
			}
			time.Sleep(time.Millisecond * 3)
		}
	}
	return true
}

func (conn *Conn) releasedbRWLock() {
	<-dbRWLock
}

func createRealConn() (*leveldb.DB, error) {
	lvDb, err := leveldb.OpenFile(config.Dsn, &opt.Options{WriteBuffer: 0})
	if err != nil {
		return nil, NewError(ErrorDbOpenFailed, err.Error())
	}
	return lvDb, nil
}

// leveldb 协程安全，只允许创建一个链接.
func GetDb() (*Db, error) {
	if db != nil {
		return db, nil
	}
	lockWaitTimeout := time.Second * 1
	lockWaitStart := time.Now()
	hasLock := false
	for !hasLock {
		select {
		case dbObjCreateLock <- 1:
			defer func() { <-dbObjCreateLock }()
			hasLock = true
			if db != nil {
				return db, nil
			}
		default:
			// 如果没有获得锁，则等待其他协程创建完成，或等待获得锁之后继续创建新链接
			if db != nil {
				return db, nil
			} else {
				if time.Now().Sub(lockWaitStart) > lockWaitTimeout {
					return nil, NewError(ErrorDbCreateFailed, "Failed to create db object, cannot accquire lock.")
				}
				time.Sleep(time.Millisecond * 2)
			}
		}
	}

	if !initialized {
		return nil, errors.New("Db not initialized. You should call InitDb first!")
	}

	var cErr error
	var rConn *leveldb.DB
	for maxRetryTimes := 180; maxRetryTimes > 0; maxRetryTimes-- {
		rConn, cErr = createRealConn()
		if cErr == nil {
			break
		} else {
			time.Sleep(time.Millisecond * 50)
		}
	}

	if cErr != nil {
		return nil, errors.New(fmt.Sprintf("Failed to create db connection: %v", cErr))
	}
	db = &Db{&Conn{rconn: rConn}}
	return db, nil
}

// TODO: go routine safe.
func Close() error {
	if db != nil {
		return db.Conn.rconn.Close()
	}
	return nil
}
