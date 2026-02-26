package main

import (
	"flag"
	"fmt"
	"ip-isabelle-and-ben/pkg/ipStack"

	// "ip-isabelle-and-ben/pkg/lnxconfig"
	// "net/netip"
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
	ipStack, err := ipStack.InitIPStackFromConfig(*configPath)
	if err != nil {
		fmt.Println("Error initializing IP Stack: %s", err.Error())
		os.Exit(1)
	}

	fmt.Println("Forwarding Table")
	ipStack.PrintForwardingTable()
	fmt.Println("Interfaces")
	ipStack.PrintInterfaces()

	/* start handling IP messages */
	go ipStack.RunIPLayer()

	/* start REPL */
	ipStack.StartREPL()
}

