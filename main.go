package main

import (
	"github.com/gorilla/mux"
	"github.com/jawher/mow.cli"
	"io"
	"log"
	"net/http"
	"os"
)

const logPattern = log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile | log.LUTC

var infoLogger *log.Logger
var warnLogger *log.Logger
var errorLogger *log.Logger

func main() {
	app := cli.App("aggregate-healthcheck", "Monitoring health of multiple services in cluster.")
	hostMachine := app.String(cli.StringOpt{
		Name:   "host-machine",
		Value:  "",
		Desc:   "Hostname of the machine this container runs on (e.g. ip-172-24-91-192.eu-west-1.compute.internal)",
		EnvVar: "HOST_MACHINE",
	})
	whitelistedTopics := app.Strings(cli.StringsOpt{
		Name:   "whitelisted-topics",
		Value:  []string{},
		Desc:   "Comma-separated list of kafka topics that we do not need to check for lags. (e.g. Concept,AnotherQ)",
		EnvVar: "WHITELISTED_TOPICS",
	})
	lagTolerance := app.Int(cli.IntOpt{
		Name:   "lag-tolerance",
		Value:  0,
		Desc:   "Number of messages that can pile up before warning. (e.g. 5)",
		EnvVar: "LAG_TOLERANCE",
	})

	app.Action = func() {
		initLogs(os.Stdout, os.Stdout, os.Stderr)
		//transport := &http.Transport{
		//	Dial: proxy.Direct.Dial,
		//	ResponseHeaderTimeout: 10 * time.Second,
		//	MaxIdleConnsPerHost:   100,
		//}
		//httpClient := &http.Client{
		//	Timeout:   5 * time.Second,
		//	Transport: transport,
		//}
		healthCheck := newHealthcheck(*hostMachine, *whitelistedTopics, *lagTolerance)
		router := mux.NewRouter()
		router.HandleFunc("/__health", healthCheck.checkHealth())
		router.HandleFunc("/__gtg", healthCheck.gtg)
		err := http.ListenAndServe(":8080", router)
		if err != nil {
			errorLogger.Printf("Can't set up HTTP listener on 8080. %v", err)
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
