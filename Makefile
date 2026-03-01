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

# # TODO: update to work with vnet_run once flushed out more

# Here are our favorite vnet_run commands (replace <net dir> with a directory that has lnx files):

# Run your vhost and vrouter on some network: util/vnet_run <net dir>
# Run the reference version: util/vnet_run --bin-dir reference <net dir>
# Run specific binaries for vhost and vrouter (here, use reference for vhost, your vrouter): util/vnet_run --host ./reference/host --router ./vrouter <net dir>
# Pass extra arguments to each node (here, running the reference in debug mode):
# util/vnet_run --bin-dir reference <net dir> -- --log-level debug
