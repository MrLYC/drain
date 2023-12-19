# Drain

> This project is an golang port of the original [Drain3](https://github.com/IBM/Drain3) project.

Drain is an online log template miner that can extract templates (clusters) from a stream of log messages in a timely manner. It employs a parse tree with fixed depth to guide the log group search process, which effectively avoids constructing a very deep and unbalanced tree.

## Example

```go
package main

import (
	"fmt"

	"github.com/mrlyc/drain"
)

func main() {
	logger := drain.New(drain.NewConfig(drain.SpaceTokenizer, map[string]string{
		"{ip}":   `^([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})$`,
		"{hex}":  `^0x([0-9a-fA-F]{1,8})$`,
		"{name}": `^[a-zA-Z]+$`,
	}))

	for _, line := range []string{
		"connected to 10.0.0.1",
		"connected to 10.0.0.2",
		"connected to 10.0.0.3",
		"Hex number 0xDEADBEAF",
		"Hex number 0x10000",
		"user davidoh logged in",
		"user eranr logged in",
	} {
		logger.Train(line)
	}

	for _, cluster := range logger.Clusters() {
		println(cluster.String())
	}

	cluster := logger.Match("user faceair logged in")
	if cluster == nil {
		println("no match")
	} else {
		fmt.Printf("cluster matched: %s\n", cluster.String())
	}
}
```

Output:
```
id={1} : size={3} : connected to {ip}
id={2} : size={2} : Hex number {hex}
id={3} : size={2} : user {name} logged in
cluster matched: id={3} : size={2} : user {name} logged in
```

## LICENSE

[MIT](LICENSE)