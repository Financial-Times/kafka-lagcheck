package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Financial-Times/go-fthealth"
	"io/ioutil"
	"net/http"
	"os"
)

type Healthcheck struct {
	httpClient     *http.Client
	kafkaHost      string
	consumerGroups []string
	checkPrefix    string
	burrowFailures chan bool
}

func NewHealthcheck(httpClient *http.Client, kafkaHost string, consumerGroups []string) *Healthcheck {
	failures := make(chan bool, 3 * len(consumerGroups))
	return &Healthcheck{
		httpClient:     httpClient,
		kafkaHost:      kafkaHost,
		consumerGroups: consumerGroups,
		checkPrefix:    "http://localhost:8081/v2/kafka/local/consumer/",
		burrowFailures: failures,
	}
}

func (h *Healthcheck) checkHealth() func(w http.ResponseWriter, r *http.Request) {
	var consumerGroupChecks []fthealth.Check
	for _, consumer := range h.consumerGroups {
		consumerGroupChecks = append(consumerGroupChecks, h.consumerLags(consumer))
	}
	return fthealth.HandlerParallel("Kafka consumer groups", "Verifies all the defined consumer groups if they have lags.", consumerGroupChecks...)
}

func (h *Healthcheck) gtg(writer http.ResponseWriter, req *http.Request) {
	for _, consumer := range h.consumerGroups {
		if err := h.checkConsumerGroupForLags(consumer); err != nil {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
	}
}

func (h *Healthcheck) consumerLags(consumer string) fthealth.Check {
	return fthealth.Check{
		BusinessImpact:   "Will delay publishing on respective pipeline.",
		Name:             "Consumer group " + consumer + " is lagging.",
		PanicGuide:       "https://sites.google.com/a/ft.com/technology/systems/dynamic-semantic-publishing/extra-publishing/",
		Severity:         1,
		TechnicalSummary: "Consumer group " + consumer + " is lagging.",
		Checker:          func() error { return h.checkConsumerGroupForLags(consumer) },
	}
}

func (h *Healthcheck) checkConsumerGroupForLags(consumerGroup string) error {
	request, err := http.NewRequest("GET", h.checkPrefix+consumerGroup+"/status", nil)
	if err != nil {
		warnLogger.Printf("Could not connect to burrow: %v", err.Error())
		return err
	}
	resp, err := h.httpClient.Do(request)
	if err != nil {
		warnLogger.Printf("Could not execute request to burrow: %v", err.Error())
		h.accumulateFailure()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Burrow returned status %d", resp.StatusCode)
		return errors.New(errMsg)
	}
	h.clearFailures()
	body, err := ioutil.ReadAll(resp.Body)
	fullStatus := make(map[string]interface{})
	err = json.Unmarshal(body, &fullStatus)
	if err != nil {
		return errors.New(fmt.Sprintf("Couldn't unmarshall burrow's response: %d %d", string(body), err))
	}
	if fullStatus["error"] != false {
		return errors.New(fmt.Sprintf("Burrow's status response is an error: %d", string(body)))
	}
	status := fullStatus["status"].(map[string]interface{})
	if status["status"] != "OK" {
		return errors.New(fmt.Sprintf("%d on kafka consumer lag. Further info at: __ft-burrow/v2/kafka/local/consumer/%d/status", status["status"], consumerGroup))
	}
	return nil
}

func (h *Healthcheck) accumulateFailure() {
	select {
	case h.burrowFailures <- true:
	default:
		errorLogger.Println("Will exit app and container with failure, to promote restarting itself. Burrow will have another chance reconnecting to Kafka after.")
		os.Exit(1)
	}
}

func (h *Healthcheck) clearFailures() {
	for {
		select {
		case <- h.burrowFailures:
		default:
			return
		}
	}
}
