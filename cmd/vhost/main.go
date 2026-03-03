package main

import (
	"flag"
	"fmt"
	"ip-isabelle-and-ben/pkg/ipStack"
	"ip-isabelle-and-ben/pkg/protocol"

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
		fmt.Printf("Error initializing IP Stack: %s\n", err.Error())
		os.Exit(1)
	}

	fmt.Println("Forwarding Table")
	ipStack.PrintForwardingTable()
	fmt.Println("Interfaces")
	ipStack.PrintInterfacesForDebugging()

	/* register test protocol receive handler */
	ipStack.RegisterRecvHandler(0, protocol.HandleTestMessage)

	/* start handling IP messages */
	go ipStack.RunIPLayer()

	/* start REPL */
	ipStack.StartREPL()
}

