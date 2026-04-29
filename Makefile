all:
	go build -o vhost ./cmd/vhost
	go build -o vrouter ./cmd/vrouter
clean:
	rm -fv vhost vrouter

doc-ex:
	util/vnet_run ./generatedNets/doc-example

l-r1h2:
	util/vnet_run ./generatedNets/linear-r1h2

l-r1h2a:
	util/vnet_run ./generatedNets/linear-r1h2a

l-r1h2c:
	util/vnet_run ./generatedNets/linear-r1h2c

l-r1h4:
	util/vnet_run ./generatedNets/linear-r1h4

l-r3h2:
	util/vnet_run ./generatedNets/linear-r3h2

lp:
	util/vnet_run ./generatedNets/loop

dabc:
	util/vnet_run ./generatedNets/d-abc

refhost-loop:
	util/vnet_run --host ./reference/arm64/vhost --router ./vrouter ./generatedNets/loop

ourhost-loop:
	util/vnet_run --host ./vhost --router ./reference/arm64/vrouter ./generatedNets/loop

ourhost:
	util/vnet_run --host ./vhost --router ./reference/arm64/vrouter ./generatedNets/linear-r1h2

ourhost-c:
	util/vnet_run --host ./vhost --router ./reference/arm64/vrouter ./generatedNets/linear-r1h2c

net:
	util/vnet_generate nets/$(NET).json $(NET)