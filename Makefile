all:
	go build -o vhost ./cmd/vhost
	go build -o vrouter ./cmd/vrouter
clean:
	rm -fv vhost vrouter

de-h1:
	go run ./cmd/vhost --config ./doc-example/h1.lnx

de-r1:
	go run ./cmd/vrouter --config ./doc-example/r1.lnx

doc-ex:
	util/vnet_run doc-example

l-r1h2:
	util/vnet_run linear-r1h2

l-r1h4:
	util/vnet_run linear-r1h4

l-r3h2:
	util/vnet_run linear-r3h2

lp:
	util/vnet_run loop

dabc:
	util/vnet_run d-abc

refhost-loop:
	util/vnet_run --host ./reference/arm64/vhost --router ./vrouter loop

ourhost-loop:
	util/vnet_run --host ./vhost --router ./reference/arm64/vrouter loop