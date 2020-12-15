// 此文件包含安全信息，编译时请根据实际部署修改其中的内容, 并将文件拷贝为settings.pack.toml.go
package config

var secretSettingsRaw = `
Ver = "0.10.3"

[Zookeeper]
#root 必须以"/"开头. 所有配置存放在此节点下.
root = "/jumeiconf"

[Zookeeper.host]
DEVCD="192.168.69.6:2181,192.168.69.6:2182"
DEVBJ="192.168.49.49:2181,192.168.49.49:2182"
QACD="192.168.69.7:2181,192.168.69.7:2182"
QABJ="192.168.49.65:2181,192.168.49.65:2182"
STAG="10.1.2.50:2181"
PUB="10.1.2.50:2182"
PROD="10.1.2.57:2181,10.1.2.58:2181,10.1.2.59:2181,10.1.2.60:2181,10.1.2.61:2181"
#压测环境配置服务器
BENCH="10.1.2.50:2183"
PROD-MJQ = "10.8.20.21:2181,10.8.20.22:2181,10.8.20.23:2181"
PUB-MJQ = "10.8.20.21:2182"
PROD-YZ = "10.17.48.4:2181,10.17.48.5:2181"
PUB-YZ = "10.17.48.4:2182"
[Encrypt]
# 加密相关设置的版本号,如果配置有改动，此版本号也应进行对应的修改。
Ver=1.0
# iv必须为8字节
[Encrypt.DEVCD]
key = "example-encrypt-key"
iv = "11111111"
[Encrypt.DEVBJ]
key = "example-encrypt-key"
iv = "11111111"
[Encrypt.QACD]
key = "example-encrypt-key"
iv = "11111111"
[Encrypt.QABJ]
key = "example-encrypt-key"
iv = "11111111"
[Encrypt.BENCH]
key = "example-encrypt-key"
iv = "11111111"
[Encrypt.PUB]
key = "example-encrypt-key"
iv = "11111111"
[Encrypt.STAG]
key = "example-encrypt-key"
iv = "11111111"
[Encrypt.PROD]
key = "example-encrypt-key"
iv = "11111111"
`
