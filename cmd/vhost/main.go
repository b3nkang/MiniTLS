package main

import (
	"flag"
	"fmt"
	"ip-isabelle-and-ben/pkg/ipStack"
	"ip-isabelle-and-ben/pkg/lnxconfig"
	"net/netip"
	"os"
)

func main() {
	configPath := flag.String("config", "", "path to config file")

	flag.Parse()

	/* enforce command */
	if *configPath == "" {
		fmt.Println("Usage: vhost --config <file>")
		os.Exit(1)
	}

	/* initialize IP stack */
	ipStack, err := InitIPStackFromConfig(*configPath)
	if err != nil {
		fmt.Println("Error initializing IP Stack: %s", err.Error())
		os.Exit(1)
	}

	ipStack.PrintForwardingTable()
	ipStack.PrintInterfaces()

}

func InitIPStackFromConfig(fileName string)(*ipStack.IPStack, error) {
	/* parse lnx file */
	lnxConfig, err := lnxconfig.ParseConfig(fileName)
	if err != nil {
		return nil, err
	}
	
	ipStack := &ipStack.IPStack{
		Interfaces:      make(map[string]*ipStack.Interface),
		ForwardingTable: make(map[netip.Prefix]ipStack.FwdEntry, 0),
	}

	/* initialize structs within this IPStack */
	err = ipStack.Init(lnxConfig)
	if err != nil {
		return nil, err
	}

	return ipStack, nil;
}