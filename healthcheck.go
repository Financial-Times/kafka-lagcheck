package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Financial-Times/go-fthealth"
	"github.com/jmoiron/jsonq"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
)

type healthcheck struct {
	hostMachine       string
	whitelistedTopics []string
	whitelistedEnvs   []string
	checkPrefix       string
	lagTolerance      int
}

func newHealthcheck(hostMachine string, whitelistedTopics []string, whitelistedEnvs []string, lagTolerance int) *healthcheck {
	return &healthcheck{
		hostMachine:       hostMachine,
		checkPrefix:       "http://" + hostMachine + ":8080/__burrow/v2/kafka/local/consumer/",
		whitelistedTopics: whitelistedTopics,
		whitelistedEnvs:   whitelistedEnvs,
		lagTolerance:      lagTolerance,
	}
}

func (h *healthcheck) checkHealth(w http.ResponseWriter, r *http.Request) {
	consumerGroups, err := h.fetchAndParseConsumerGroups()
	if err != nil {
		warnLogger.Println(err.Error())
		c := h.burrowUnavailableCheck(err)
		fthealth.HandlerParallel("Kafka consumer groups", "Verifies all the defined consumer groups if they have lags.", c)(w, r)
		return
	}
	var consumerGroupChecks []fthealth.Check
	for _, consumer := range consumerGroups {
		consumerGroupChecks = append(consumerGroupChecks, h.consumerLags(consumer))
	}
	if len(consumerGroups) == 0 {
		c := h.noConsumerGroupsCheck()
		fthealth.HandlerParallel("Kafka consumer groups", "Verifies all the defined consumer groups if they have lags.", c)(w, r)
		return
	}
	fthealth.HandlerParallel("Kafka consumer groups", "Verifies all the defined consumer groups if they have lags.", consumerGroupChecks...)(w, r)
}

func (h *healthcheck) gtg(writer http.ResponseWriter, req *http.Request) {
	consumerGroups, err := h.fetchAndParseConsumerGroups()
	if err != nil {
		warnLogger.Println(err.Error())
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

func (h *healthcheck) consumerLags(consumer string) fthealth.Check {
	return fthealth.Check{
		BusinessImpact:   "Will delay publishing on respective pipeline.",
		Name:             "Consumer group " + consumer + " is lagging.",
		PanicGuide:       "https://sites.google.com/a/ft.com/ft-technology-service-transition/home/run-book-library/kafka-lagcheck",
		Severity:         1,
		TechnicalSummary: "Consumer group " + consumer + " is lagging. Further info at: __burrow/v2/kafka/local/consumer/" + consumer + "/status",
		Checker:          func() error {
			return h.fetchAndCheckConsumerGroupForLags(consumer)
		},
	}
}

func (h *healthcheck) burrowUnavailableCheck(err error) fthealth.Check {
	return fthealth.Check{
		BusinessImpact:   "Will delay publishing on respective pipeline.",
		Name:             "Error retrieving consumer group list.",
		PanicGuide:       "https://sites.google.com/a/ft.com/ft-technology-service-transition/home/run-book-library/kafka-lagcheck",
		Severity:         1,
		TechnicalSummary: fmt.Sprintf("Error retrieving consumer group list. Underlying kafka analysis tool burrow@*.service is unavailable. Please restart it or have a look if kafka itself is running properly. %s", err.Error()),
		Checker:          func() error {
			return errors.New("Error retrieving consumer group list.")
		},
	}
}

func (h *healthcheck) noConsumerGroupsCheck() fthealth.Check {
	return fthealth.Check{
		BusinessImpact:   "Will delay publishing on respective pipeline.",
		Name:             "Error retrieving consumer group list.",
		PanicGuide:       "https://sites.google.com/a/ft.com/ft-technology-service-transition/home/run-book-library/kafka-lagcheck",
		Severity:         1,
		TechnicalSummary: "Can't see any consumers yet so no lags to report and could successfully connect to kafka. This usually should happen only on startup, please retry in a few moments, and if this case persists, take a more serious look at burrow and kafka.",
		Checker:          func() error {
			return nil
		},
	}
}

func (h *healthcheck) fetchAndCheckConsumerGroupForLags(consumerGroup string) error {
	resp, err := http.Get(h.checkPrefix + consumerGroup + "/status")
	if err != nil {
		warnLogger.Printf("Could not execute request to burrow: %v", err.Error())
		return err
	}
	defer properClose(resp)
	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Burrow returned status %d", resp.StatusCode)
		return errors.New(errMsg)
	}
	body, err := ioutil.ReadAll(resp.Body)
	return h.checkConsumerGroupForLags(body, consumerGroup)
}

func (h *healthcheck) checkConsumerGroupForLags(body []byte, consumerGroup string) error {
	fullStatus := map[string]interface{}{}
	dec := json.NewDecoder(bytes.NewReader(body))
	err := dec.Decode(&fullStatus)
	if err != nil {
		warnLogger.Printf("Could not decode response body to json: %v %v", string(body), err.Error())
		return errors.New("Could not decode response body to json.")
	}
	jq := jsonq.NewQuery(fullStatus)
	statusError, err := jq.Bool("error")
	if err != nil {
		warnLogger.Printf("Couldn't unmarshall consumer status: %v %v", string(body), err.Error())
		return errors.New("Couldn't unmarshall consumer status.")
	}
	if statusError {
		warnLogger.Printf("Consumer status response is an error: %v", string(body))
		return errors.New("Consumer status response is an error.")
	}
	totalLag, err := jq.Int("status", "totallag")
	if err != nil {
		warnLogger.Printf("Couldn't unmarshall totallag: %v %v", string(body), err.Error())
		return errors.New("Couldn't unmarshall totallag.")
	}
	if totalLag > h.lagTolerance {
		return h.ignoreWhitelistedTopics(jq, body, totalLag, consumerGroup)
	}
	return nil
}

func (h *healthcheck) ignoreWhitelistedTopics(jq *jsonq.JsonQuery, body []byte, lag int, consumerGroup string) error {
	topic1, err1 := jq.String("status", "maxlag", "topic")
	topic2, err2 := jq.String("status", "partitions", "0", "topic")
	if err1 != nil && err2 != nil {
		warnLogger.Printf("Couldn't unmarshall topic: %v %v %v", string(body), err1.Error(), err2.Error())
		return errors.New("Couldn't unmarshall topic.")
	}
	topic := topic1
	if topic == "" {
		topic = topic2
	}
	for _, whitelistedTopic := range h.whitelistedTopics {
		if topic == whitelistedTopic {
			return nil
		}
	}
	return fmt.Errorf("%s consumer group is lagging behind with %d messages", consumerGroup, lag)
}

func (h *healthcheck) fetchAndParseConsumerGroups() ([]string, error) {
	resp, err := http.Get(h.checkPrefix)
	if err != nil {
		warnLogger.Printf("Could not execute request to burrow: %v", err.Error())
		return nil, err
	}
	defer properClose(resp)
	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Burrow returned status %d", resp.StatusCode)
		return nil, errors.New(errMsg)
	}
	body, err := ioutil.ReadAll(resp.Body)
	return h.parseConsumerGroups(body)
}

func properClose(resp *http.Response) {
	io.Copy(ioutil.Discard, resp.Body)
	err := resp.Body.Close()
	if err != nil {
		warnLogger.Printf("Could not close response body: %v", err.Error())
	}
}

func (h *healthcheck) parseConsumerGroups(body []byte) ([]string, error) {
	fullConsumers := make(map[string]interface{})
	err := json.NewDecoder(bytes.NewReader(body)).Decode(&fullConsumers)
	if err != nil {
		warnLogger.Printf("Could not decode response body to json: %v %v", string(body), err.Error())
		return nil, fmt.Errorf("Could not decode response body to json: %v %v", string(body), err)
	}
	jq := jsonq.NewQuery(fullConsumers)
	statusError, err := jq.Bool("error")
	if err != nil {
		return nil, fmt.Errorf("Couldn't unmarshall consumer list response: %v %v", string(body), err)
	}
	if statusError {
		return nil, fmt.Errorf("Consumer list response is an error: %v", string(body))
	}
	consumers, err := jq.ArrayOfStrings("consumers")
	if err != nil {
		warnLogger.Printf("Couldn't unmarshall consumer list: %s %s", string(body), err.Error())
		return nil, fmt.Errorf("Couldn't unmarshall consumer list: %s %s", string(body), err)
	}
	return h.filterOutNonRelatedKafkaBridges(consumers), nil
}

func (h *healthcheck) filterOutNonRelatedKafkaBridges(consumers []string) []string {
	filteredConsumers := []string{}
	for _, consumer := range consumers {
		if strings.Contains(consumer, "kafka-bridge") && !h.isBridgeFromWhitelistedEnvs(consumer) {
			continue
		}

		filteredConsumers = append(filteredConsumers, consumer)
	}

	return filteredConsumers
}

func (h *healthcheck) isBridgeFromWhitelistedEnvs(bridgeName string) bool {
	//Do not filter out any Kafka bridge by default
	if len(h.whitelistedEnvs) == 0 {
		return true
	}

	for _, whitelistedEnv := range h.whitelistedEnvs {
		if strings.HasPrefix(bridgeName, whitelistedEnv) {
			return true
		}
	}

	return false
}
