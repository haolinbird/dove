package zk

import (
	"doveclient/config"
	"errors"
	"fmt"
	"reflect"
	"time"

	//"github.com/samuel/go-zookeeper/zk"
	"github.com/coreos/etcd/clientv3"
	"context"
	"math/rand"
	"sync"
	"doveclient/logger"
)

var storage *Storage

type Storage struct {
	Conn *Conn
}

type Conn struct {
	// zkConn *zk.Conn
	zkConn *clientv3.Client
	// the underground connection to storage server has been re-created.
	DriverReset chan bool
	drLock *sync.RWMutex
}

func (conn *Conn) CreateDriverResetChan(){
	conn.drLock.Lock()
	conn.DriverReset = make(chan bool)
	conn.drLock.Unlock()
}

func (conn *Conn)GetDriverResetChan()(<-chan bool){
	conn.drLock.RLock()
	defer conn.drLock.RUnlock()
	return conn.DriverReset
}

func (conn *Conn) KeepAliveOnce(ctx context.Context, lease clientv3.LeaseID) (*clientv3.LeaseKeepAliveResponse, error) {
	return conn.zkConn.KeepAliveOnce(ctx, lease)
}

func (conn *Conn) KeepAlive(ctx context.Context, lease clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	return conn.zkConn.KeepAlive(ctx, lease)
}

/*func (conn *Conn) GetW(path string) ([]byte, *zk.Stat, <-chan zk.Event, error) {
	return conn.zkConn.GetW(path)
}*/

func (conn *Conn) Get(path string) ([]byte, error) {
	// return conn.zkConn.Get(path)
	resp, err := conn.zkConn.Get(context.TODO(), P2CN(path))
	if err != nil {
		return []byte{}, err
	}

	if len(resp.Kvs) == 0 {
		return []byte{}, nil
	}

	return resp.Kvs[0].Value, nil
}

func (conn *Conn) Grant(ttl int64) (clientv3.LeaseID, error) {
	resp, err := conn.zkConn.Grant(context.TODO(), ttl)
	if err != nil {
		return 0, err
	}

	return resp.ID, err
}

func (conn *Conn) Create(path string, data []byte, lease clientv3.LeaseID) (string, error) {
	// return conn.zkConn.Create(path, data, flags, acl)
	_, err := conn.zkConn.Put(context.TODO(), P2CN(path), string(data), clientv3.WithLease(lease))
	return path, err
}

func (conn *Conn) Set(path string, data []byte, lease ...clientv3.LeaseID) (string, error) {
	// return conn.zkConn.Set(path, data, version)
	var err error
	if len(lease) > 0 {
		_, err = conn.zkConn.Put(context.TODO(), P2CN(path), string(data), clientv3.WithLease(lease[0]))
	} else {
		_, err = conn.zkConn.Put(context.TODO(), P2CN(path), string(data))
	}
	return path, err
}

func (conn *Conn) ExistsW(path string) clientv3.WatchChan {
	// return conn.zkConn.ExistsW(path)
	// ctx, _ := context.WithTimeout(context.Background(), 10 * time.Second)
	rch := conn.zkConn.Watch(context.TODO(), P2CN(path))
	return rch
}

func (conn *Conn) Exists(path string) (bool, error) {
	// return conn.zkConn.Exists(path)
	resp, err := conn.zkConn.Get(context.TODO(), P2CN(path))
	if err != nil {
		return false, err
	}

	var result bool = len(resp.Kvs) > 0
	return result, err
}

func (conn *Conn) Close() {
	// 因为整个进程使用同一个etcd链接， 所以禁止连接被关闭
	return
	conn.zkConn.Close()
}

// 获取一个连接单例
func GetConn(callback func(string, bool)) (*Storage, error) {
	if storage != nil {
		return storage, nil
	}

	var err error
	storage, err = CreateConn(callback)
	return storage, err
}

var once sync.Once
func CreateConn(callback func(string, bool)) (*Storage, error) {
	defEnv := config.GetConfig().ENV
	zkHosts := config.GetZkConfig()["hosts"].(map[string][]string)
	if zkHosts[defEnv] == nil {
		return nil, errors.New(fmt.Sprintf("Defined ENV(%s) is not valid, it must one of %v", defEnv, reflect.ValueOf(zkHosts).MapKeys()))
	}
	
	zkConn, err := clientv3.New(clientv3.Config{
		Endpoints: zkHosts[defEnv],
		DialTimeout: time.Second * time.Duration(config.GetConfig().ETCD_CONNECTION_TIMEOUT),
		DialKeepAliveTime: time.Second * time.Duration(config.GetConfig().ETCD_DIAL_KEEP_ALIVE_TIME),
		DialKeepAliveTimeout: time.Second * time.Duration(config.GetConfig().ETCD_DIAL_KEEP_ALIVE_TIMEOUT),
		AutoSyncInterval: time.Second * time.Duration(config.GetConfig().ETCD_AUTO_SYNC_INTERVAL),
	})
	var storage Storage
	if err == nil {
		storage = Storage{Conn: &Conn{zkConn: zkConn,drLock: new(sync.RWMutex)}}
		storage.Conn.CreateDriverResetChan()
		// etcd连接失效检测 与 恢复
		once.Do(func() {
			go func() {
				for {
					logger.GetLogger("INFO").Printf("The ETCD server is being tested...")
					ctx, _ := context.WithTimeout(context.Background(), time.Second * time.Duration(config.GetConfig().ETCD_REQUEST_TIMEOUT))
					_, err := zkConn.Get(ctx, config.GetConfig().ETCD_HEARTBEAT_KEY)
					if err == nil {
						r := rand.New(rand.NewSource(time.Now().UnixNano()))
						checkIntervalRange := r.Int63n(config.GetConfig().ETCD_RETRY_INTERVAL_RANGE) + 1
						logger.GetLogger("INFO").Printf("ETCD server test normal, %d seconds after the re-test", checkIntervalRange + config.GetConfig().ETCD_MIN_RETRY_INTERVAL)
						time.Sleep(time.Second * time.Duration(checkIntervalRange + config.GetConfig().ETCD_MIN_RETRY_INTERVAL))
					} else {
						logger.GetLogger("WARN").Printf("ETCD server exception, try to re-establish the connection...")
						newConn, err := clientv3.New(clientv3.Config{
							Endpoints: zkHosts[defEnv],
							DialTimeout: time.Second * time.Duration(config.GetConfig().ETCD_CONNECTION_TIMEOUT),
							DialKeepAliveTime: time.Second * time.Duration(config.GetConfig().ETCD_DIAL_KEEP_ALIVE_TIME),
							DialKeepAliveTimeout: time.Second * time.Duration(config.GetConfig().ETCD_DIAL_KEEP_ALIVE_TIMEOUT),
							AutoSyncInterval: time.Second * time.Duration(config.GetConfig().ETCD_AUTO_SYNC_INTERVAL),
						})
						if err == nil {
							logger.GetLogger("WARN").Printf("The ETCD server has been reconnected")
							zkConn.Close()
							*zkConn = *newConn
							close(storage.Conn.DriverReset)
							storage.Conn.CreateDriverResetChan()
							// 连接断开期间可能有配置更新，需要强制刷新所有配置
							if callback != nil {
								logger.GetLogger("WARN").Printf("Because the connection is broken, forced to refresh all configurations..")
								callback("*", true)
							}
						}
						
						// 防止意外情况，没有经过超时直接返回错误，造成循环快速执行，影响业务机器
						time.Sleep(time.Second * 1)
					}
				}
			}()
		})
	}

	// return &storage, eCh, err
	return &storage, err
}

func GetOneHost() string {
	defEnv := config.GetConfig().ENV
	zkHosts := config.GetZkConfig()["hosts"].(map[string][]string)
	return zkHosts[defEnv][0]
}
