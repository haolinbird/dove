// Package etcd based on etcd V3.
package etcd

import(
	etcdClient "github.com/coreos/etcd/clientv3"
	"time"
	"epg/jmconf/etcd"
)

const CONNECT_TIMEOUT_FOR_RETRY = time.Millisecond * 5000
// GetClient get a etced client.
// willTimedout if false, the function will block the routine until a client is successfully created.
func GetClient(willTimedout bool)(*etcdClient.Client, err){
	var(
		err error
		client *etcdClient.Client
	)
	etcdClient.New(etcdClient.Config{

	})
	return err, client
}