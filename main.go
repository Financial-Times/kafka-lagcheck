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
	kafkaHost := app.String(cli.StringOpt{
		Name:   "kafka-host",
		Value:  "",
		Desc:   "Hostname of the machine kafka and burrow (same) runs on (e.g. ip-172-24-91-192.eu-west-1.compute.internal)",
		EnvVar: "KAFKA_HOST",
	})
	consumerGroups := app.Strings(cli.StringsOpt{
		Name:   "consumer-groups",
		Value:  []string{},
		Desc:   "Comma-separated list of kafka consumer group names that we need to check for lags. (e.g. nativeIngesterCms,nativeIngesterMetadata)",
		EnvVar: "CONSUMER_GROUPS",
	})

	app.Action = func() {
		initLogs(os.Stdout, os.Stdout, os.Stderr)
		httpClient := &http.Client{}
		healthCheck := NewHealthcheck(httpClient, *kafkaHost, *consumerGroups)
		//httpClient.Get("http://example.com")
		router := mux.NewRouter()
		router.HandleFunc("/__health", healthCheck.checkHealth())
		router.HandleFunc("/__gtg", healthCheck.gtg)
		err := http.ListenAndServe(":8080", router)
		if err != nil {
			errorLogger.Printf("Can't set up HTTP listener on 8080. %v", err)
			os.Exit(1)
		}
	}
	app.Run(os.Args)
}

func initLogs(infoHandle io.Writer, warnHandle io.Writer, errorHandle io.Writer) {
	infoLogger = log.New(infoHandle, "INFO  - ", logPattern)
	warnLogger = log.New(warnHandle, "WARN  - ", logPattern)
	errorLogger = log.New(errorHandle, "ERROR - ", logPattern)
}
