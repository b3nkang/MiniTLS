package main

import (
	"flag"
	"fmt"
	"ip-isabelle-and-ben/pkg/ipStack"
	"ip-isabelle-and-ben/pkg/protocol"
	"ip-isabelle-and-ben/pkg/tcpStack"
	"ip-isabelle-and-ben/pkg/tlsStack"

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

	ipStack.PrintInterfacesForDebugging()

	/* register test protocol receive handler */
	ipStack.RegisterRecvHandler(0, protocol.HandleTestMessage)

	/* start handling IP messages */
	go ipStack.RunIPLayer()

	/* init TCP stuff */
	tcpStack := tcpStack.InitTCPStack(ipStack)
	/* register handler to deal with TCP messages */
	ipStack.RegisterRecvHandler(6, tcpStack.HandleTCP)

	// init TLS
	tlsStack := tlsStack.InitVTLSStack(tcpStack, ipStack.TlsReplChan)

	go tcpStack.HandleTcpReplCommands()
	go tlsStack.HandleTlsReplCommands()

	/* start REPL -- runs forever so needs to be last*/
	ipStack.StartREPL()
}

