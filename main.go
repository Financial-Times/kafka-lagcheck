package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	status "github.com/Financial-Times/service-status-go/httphandlers"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/jawher/mow.cli"
)

const logPattern = log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile | log.LUTC

var infoLogger *log.Logger
var warnLogger *log.Logger
var errorLogger *log.Logger

func main() {
	app := cli.App("aggregate-healthcheck", "Monitoring health of multiple services in cluster.")
	port := app.String(cli.StringOpt{
		Name:   "port",
		Value:  "8080",
		Desc:   "Port to listen on",
		EnvVar: "PORT",
	})
	burrowUrl := app.String(cli.StringOpt{
		Name:   "burrow-url",
		Value:  "",
		Desc:   "Base URL at which Burrow is reachable (e.g. http://ip-172-24-91-192.eu-west-1.compute.internal:8080/__burrow)",
		EnvVar: "BURROW_URL",
	})
	whitelistedTopics := app.Strings(cli.StringsOpt{
		Name:   "whitelisted-topics",
		Value:  []string{},
		Desc:   "Comma-separated list of kafka topics that we do not need to check for lags. (e.g. Concept,AnotherQ)",
		EnvVar: "WHITELISTED_TOPICS",
	})
	whitelistedEnvironments := app.Strings(cli.StringsOpt{
		Name:   "whitelisted-environments",
		Value:  []string{},
		Desc:   "Comma-separated list of environments that contain kafka bridges that we need to check for lags. (e.g. prod-uk, prod-us)",
		EnvVar: "WHITELISTED_ENVS",
	})
	maxLagTolerance := app.Int(cli.IntOpt{
		Name:   "max-lag-tolerance",
		Value:  1000,
		Desc:   "Number of messages that can pile up before warning when Burrow reports no ERR.",
		EnvVar: "MAX_LAG_TOLERANCE",
	})
	errLagTolerance := app.Int(cli.IntOpt{
		Name:   "err-lag-tolerance",
		Value:  30,
		Desc:   "Number of messages that can pile up before warning when Burrow reports there is an ERR.",
		EnvVar: "ERR_LAG_TOLERANCE",
	})

	app.Action = func() {
		initLogs(os.Stdout, os.Stdout, os.Stderr)

		burrowAddress := *burrowUrl
		if strings.HasSuffix(burrowAddress, "/") {
			burrowAddress = burrowAddress[:len(burrowAddress)-1]
		}

		infoLogger.Printf("Non-monitored topics: %v", *whitelistedTopics)

		healthCheck := newHealthcheck(burrowAddress, *whitelistedTopics, *whitelistedEnvironments, *maxLagTolerance, *errLagTolerance)
		router := mux.NewRouter()
		router.Path("/__health").Handler(handlers.MethodHandler{"GET": http.HandlerFunc(healthCheck.Health())})
		router.Path(status.GTGPath).Handler(handlers.MethodHandler{"GET": http.HandlerFunc(status.NewGoodToGoHandler(healthCheck.GTG))})

		infoLogger.Printf("Kafka Lagcheck listening on port %v ...", *port)
		err := http.ListenAndServe(":"+*port, router)
		if err != nil {
			errorLogger.Printf("Can't set up HTTP listener on %s. %v", *port, err)
			os.Exit(1)
		}
	}
	err := app.Run(os.Args)
	if err != nil {
		errorLogger.Printf("Running app unsuccessful: %v", err)
	}
}

func initLogs(infoHandle io.Writer, warnHandle io.Writer, errorHandle io.Writer) {
	infoLogger = log.New(infoHandle, "INFO  - ", logPattern)
	warnLogger = log.New(warnHandle, "WARN  - ", logPattern)
	errorLogger = log.New(errorHandle, "ERROR - ", logPattern)
}
