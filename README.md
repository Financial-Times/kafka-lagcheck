# kafka-lagcheck - Kafka Consumer Lag Checking

Just creates a healthcheck and good-to-go endpoint that's checking kafka consumer lags..

Based on [github.com/linkedin/Burrow](https://github.com/linkedin/Burrow)

There are also configurable notifiers that can send status out.

## Run locally

1. Either you open two tunnels for the two ports for Kafka and Zookeeper in one of our clusters: `ssh -L 9092:localhost:9092 core@xp-tunnel-up.ft.com` & `ssh -L 2181:localhost:2181 core@xp-tunnel-up.ft.com`
or you [download and run kafka locally](http://kafka.apache.org/documentation.html#quickstart) (this is recommended more).
2. `go get github.com/linkedin/Burrow`
3. `cd $GOPATH/src/github.com/linkedin/Burrow`
4. `git checkout v0.1.1`
4. `gpm install`
5. `go install`
6. `go get github.com/Financial-Times/kafka-lagcheck`
7. `cd $GOPATH/src/github.com/Financial-Times/kafka-lagcheck`
8. `go install`
9. `Burrow --config config/burrow.cfg`
10. `./kafka-lagcheck`

These steps run two programs. One of them is Burrow, simply got from github, but run with the configuration set up in this project's config.
Having run this already you could try querying Burrow itself [on its REST endpoints](https://github.com/linkedin/Burrow/wiki/HTTP-Endpoint) to give you information about your Kafka. e.g.

`curl localhost:8081/v2/kafka/local/consumer`

Additionally we're running a go app, that is only serving as a forwarder, it makes requests to Burrow, as linked above, and forms the result in an FT standard healthcheck format. e.g.

`curl localhost:8080/__health`

In our cluster system the container is set up having both these applications inside. Logs of Burrow are in `burrow.out`. Logs of the go app go to stdout.

See also:

* [Configuration](https://github.com/linkedin/Burrow/wiki/Configuration)
