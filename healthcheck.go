package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Financial-Times/go-fthealth"
	"github.com/jmoiron/jsonq"
	"io/ioutil"
	"net/http"
	"os"
)

type Healthcheck struct {
	httpClient          *http.Client
	kafkaHost           string
	whitelistedTopics   []string
	checkPrefix         string
	burrowFailuresCount chan bool
	lagTolerance        int
}

func NewHealthcheck(httpClient *http.Client, kafkaHost string, whitelistedTopics []string, lagTolerance int) *Healthcheck {
	failuresCount := make(chan bool, 3)
	return &Healthcheck{
		httpClient:          httpClient,
		kafkaHost:           kafkaHost,
		checkPrefix:         "http://localhost:8081/v2/kafka/local/consumer/",
		whitelistedTopics:   whitelistedTopics,
		burrowFailuresCount: failuresCount,
		lagTolerance:        lagTolerance,
	}
}

func (h *Healthcheck) checkHealth() func(w http.ResponseWriter, r *http.Request) {
	consumerGroups, err := h.fetchAndParseConsumerGroups()
	if err != nil {
		fc := h.falseCheck()
		return fthealth.HandlerParallel("Kafka consumer groups", "Verifies all the defined consumer groups if they have lags.", fc)
	}
	var consumerGroupChecks []fthealth.Check
	for _, consumer := range consumerGroups {
		consumerGroupChecks = append(consumerGroupChecks, h.consumerLags(consumer))
	}
	return fthealth.HandlerParallel("Kafka consumer groups", "Verifies all the defined consumer groups if they have lags.", consumerGroupChecks...)
}

func (h *Healthcheck) gtg(writer http.ResponseWriter, req *http.Request) {
	consumerGroups, err := h.fetchAndParseConsumerGroups()
	if err != nil {
		writer.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	for _, consumer := range consumerGroups {
		if err := h.fetchAndCheckConsumerGroupForLags(consumer); err != nil {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
	}
}

func (h *Healthcheck) consumerLags(consumer string) fthealth.Check {
	return fthealth.Check{
		BusinessImpact:   "Will delay publishing on respective pipeline.",
		Name:             "Consumer group " + consumer + " is lagging.",
		PanicGuide:       "https://sites.google.com/a/ft.com/ft-technology-service-transition/home/run-book-library/kafka-lagcheck",
		Severity:         1,
		TechnicalSummary: "Consumer group " + consumer + " is lagging. Further info at: __burrow/v2/kafka/local/consumer/" + consumer + "/status",
		Checker:          func() error { return h.fetchAndCheckConsumerGroupForLags(consumer) },
	}
}

func (h *Healthcheck) falseCheck() fthealth.Check {
	return fthealth.Check{
		BusinessImpact:   "Will delay publishing on respective pipeline.",
		Name:             "Error retrieving consumer group list.",
		PanicGuide:       "https://sites.google.com/a/ft.com/ft-technology-service-transition/home/run-book-library/kafka-lagcheck",
		Severity:         1,
		TechnicalSummary: "Error retrieving consumer group list. The healthcheck may not be functioning properly, please try again or restart kafka-lagcheck if this doesn't change.",
		Checker:          func() error { return errors.New("Error retrieving consumer group list.") },
	}
}

func (h *Healthcheck) fetchAndCheckConsumerGroupForLags(consumerGroup string) error {
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
	return h.checkConsumerGroupForLags(body, consumerGroup)
}

func (h *Healthcheck) checkConsumerGroupForLags(body []byte, consumerGroup string) error {
	fullStatus := map[string]interface{}{}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.Decode(&fullStatus)
	jq := jsonq.NewQuery(fullStatus)
	statusError, err := jq.Bool("error")
	if err != nil {
		return errors.New(fmt.Sprintf("Couldn't unmarshall consumer status: %v %v", string(body), err))
	}
	if statusError {
		return errors.New(fmt.Sprintf("Consumer status response is an error: %v", string(body)))
	}
	status, err := jq.String("status", "status")
	if err != nil {
		warnLogger.Printf("Couldn't unmarshall consumer status: %v %v", string(body), err.Error())
		return errors.New(fmt.Sprintf("Couldn't unmarshall consumer status: %v %v", string(body), err))
	}
	if status == "OK" {
		totalLag, err := jq.Int("status", "totallag")
		if err != nil {
			warnLogger.Printf("Couldn't unmarshall totallag: %v %v", string(body), err.Error())
			return errors.New(fmt.Sprintf("Couldn't unmarshall totallag: %v %v", string(body), err))
		}
		if totalLag > h.lagTolerance {
			return h.igonreWhitelistedTopics(jq, body, totalLag, consumerGroup)
		}
		return nil
	}
	totalLag, err := jq.Int("status", "totallag")
	if err != nil {
		warnLogger.Printf("Couldn't unmarshall totallag: %v %v", string(body), err.Error())
		return errors.New(fmt.Sprintf("Couldn't unmarshall totallag: %v %v", string(body), err))
	}
	return h.igonreWhitelistedTopics(jq, body, totalLag, consumerGroup)
}

func (h *Healthcheck) igonreWhitelistedTopics(jq *jsonq.JsonQuery, body []byte, lag int, consumerGroup string) error {
	topic, err := jq.String("status", "maxlag", "topic")
	if err != nil {
		warnLogger.Printf("Couldn't unmarshall consumer topic: %v %v", string(body), err.Error())
		return errors.New(fmt.Sprintf("Couldn't unmarshall consumer topic: %v %v", string(body), err))
	}
	for _, whitelistedTopic := range h.whitelistedTopics {
		if topic == whitelistedTopic {
			return nil
		}
	}
	return errors.New(fmt.Sprintf("%s consumer group is lagging behind with %d messages.", consumerGroup, lag))
}

func (h *Healthcheck) fetchAndParseConsumerGroups() ([]string, error) {
	request, err := http.NewRequest("GET", h.checkPrefix, nil)
	if err != nil {
		warnLogger.Printf("Could not connect to burrow: %v", err.Error())
		return nil, err
	}
	resp, err := h.httpClient.Do(request)
	if err != nil {
		warnLogger.Printf("Could not execute request to burrow: %v", err.Error())
		h.accumulateFailure()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Burrow returned status %d", resp.StatusCode)
		return nil, errors.New(errMsg)
	}
	h.clearFailures()
	body, err := ioutil.ReadAll(resp.Body)
	return h.parseConsumerGroups(body)
}

func (h *Healthcheck) parseConsumerGroups(body []byte) ([]string, error) {
	fullConsumers := make(map[string]interface{})
	json.NewDecoder(bytes.NewReader(body)).Decode(&fullConsumers)
	jq := jsonq.NewQuery(fullConsumers)
	statusError, err := jq.Bool("error")
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Couldn't unmarshall consumer list response: %v %v", string(body), err))
	}
	if statusError {
		return nil, errors.New(fmt.Sprintf("Consumer list response is an error: %v", string(body)))
	}
	consumers, err := jq.ArrayOfStrings("consumers")
	if err != nil {
		warnLogger.Printf("Couldn't unmarshall consumer list: %s %s", string(body), err.Error())
		return nil, errors.New(fmt.Sprintf("Couldn't unmarshall consumer list: %s %s", string(body), err))
	}
	return consumers, nil
}

func (h *Healthcheck) accumulateFailure() {
	select {
	case h.burrowFailuresCount <- true:
	default:
		errorLogger.Println("Will exit app and container with failure, to promote restarting itself. Burrow will have another chance reconnecting to Kafka after.")
		os.Exit(1)
	}
}

func (h *Healthcheck) clearFailures() {
	for {
		select {
		case <-h.burrowFailuresCount:
		default:
			return
		}
	}
}
