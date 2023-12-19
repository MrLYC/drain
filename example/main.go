package main

import (
	"fmt"

	"github.com/mrlyc/drain"
)

func main() {
	logger := drain.New(drain.NewConfig(drain.SpaceTokenizer, map[string]string{
		"{ip}":   `^([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})$`,
		"{hex}":  `^0x([0-9a-fA-F]{1,8})$`,
		"{name}": `^\w+$`,
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
