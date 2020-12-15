package main

import (
	//"encoding/json"
	"doveclient/util"
	"fmt"
)

func main() {
	ts := `db = 1
user = "a"
arra_test=[["a","b"],["c","d"]]
[addr]
detail="abcd"
short="cfg"
[[mm.a]]
a=1
b=2
[[mm.a]]
a=11
b=22
[nodes.0]
master = "10.0.5.43:27301"
slave = "10.0.5.43:27301"

[nodes.1]
master = "10.0.5.43:27302"
slave = "10.0.5.43:27302"

[nodes.2]
master = "10.0.5.43:27303"
slave = "10.0.5.43:27303"

[nodes.3]
master = "10.0.5.43:27304"
slave = "10.0.5.43:27304"

[nodes.4]
master = "10.0.5.43:27305"
slave = "10.0.5.43:27305"

[nodes.5]
master = "10.0.60.26:27306"
slave = "10.0.60.26:27306"

[nodes.6]
master = "10.0.60.26:27307"
slave = "10.0.60.26:27307"

[nodes.7]
master = "10.0.60.26:27308"
slave = "10.0.60.26:27308"

[nodes.8]
master = "10.0.60.26:27309"
slave = "10.0.60.26:27309"

[nodes.9]
master = "10.0.60.26:27310"
slave = "10.0.60.26:27310"

[nodes.10]
master = "10.0.60.28:27311"
slave = "10.0.60.28:27311"

[nodes.11]
master = "10.0.60.28:27312"
slave = "10.0.60.28:27312"

[nodes.12]
master = "10.0.60.28:27313"
slave = "10.0.60.28:27313"

[nodes.13]
master = "10.0.60.28:27314"
slave = "10.0.60.28:27314"

[nodes.14]
master = "10.0.60.28:27315"
slave = "10.0.60.28:27315"

[ip.1.2]
a=1
b=2
[ip.2.3]
a=2
b=33

`
	ts2 := `[ab]
a1=23
a2=31
a3=["a","b"]
`

	mM, e := util.TomlToJson(ts)
	mM2, e := util.TomlConfigValueToJson(ts2)
	//jv, _ := json.MarshalIndent(mM, "", "    ")
	fmt.Printf("\n%s\n--------\n%s\n%s\n", mM, e, mM2)
}
