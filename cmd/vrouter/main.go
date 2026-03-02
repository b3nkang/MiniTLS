package main

import (
	"flag"
	"fmt"
	"ip-isabelle-and-ben/pkg/ipStack"

	"os"
)

func main() {
	configPath := flag.String("config", "", "path to config file")

	flag.Parse()

	/* enforce command */
	if *configPath == "" {
		fmt.Println("Usage: vrouter --config <file>")
		os.Exit(1)
	}

	/* initialize IP stack */
	ipStack, err := ipStack.InitIPStackFromConfig(*configPath)
	if err != nil {
		fmt.Printf("Error initializing IP Stack: %s\n", err.Error())
		os.Exit(1)
	}

	ipStack.PrintForwardingTable()
	ipStack.PrintInterfacesForDebugging()

	/* start handling IP messages */
	go ipStack.RunIPLayer()

	/* start RIP - uncomment when RIP messages can actually be received */
	go ipStack.UpdateLoop()
	// start listening for timeout
	go ipStack.TimeoutLoop()

	/* start REPL */
	ipStack.StartREPL()
}

