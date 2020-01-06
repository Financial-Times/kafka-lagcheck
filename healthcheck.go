package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	fthealth "github.com/Financial-Times/go-fthealth/v1_1"
	"github.com/Financial-Times/service-status-go/gtg"
	"github.com/jmoiron/jsonq"
)

const (
	systemCode = "kafka-lagcheck"
)

type healthcheck struct {
	whitelistedTopics []string
	whitelistedEnvs   []string
	checkPrefix       string
	maxLagTolerance   int
	errLagTolerance   int
}

func newHealthcheck(burrowUrl string, whitelistedTopics []string, whitelistedEnvs []string, maxLagTolerance int, errLagTolerance int) *healthcheck {
	return &healthcheck{
		checkPrefix:       burrowUrl + "/v2/kafka/local/consumer/",
		whitelistedTopics: whitelistedTopics,
		whitelistedEnvs:   whitelistedEnvs,
		maxLagTolerance:   maxLagTolerance,
		errLagTolerance:   errLagTolerance,
	}
}

func (h *healthcheck) Health() func(w http.ResponseWriter, r *http.Request) {
	consumerGroups, err := h.fetchAndParseConsumerGroups()
	if err != nil {
		warnLogger.Println(err.Error())
		c := fthealth.TimedHealthCheck{
			HealthCheck: fthealth.HealthCheck{
				SystemCode:  systemCode,
				Name:        "Kafka consumer groups",
				Description: "Verifies all the defined consumer groups if they have lags.",
				Checks:      []fthealth.Check{h.burrowUnavailableCheck(err)},
			},
			Timeout: 10 * time.Second,
		}
		return fthealth.Handler(c)
	}

	var consumerGroupChecks []fthealth.Check
	for _, consumer := range consumerGroups {
		consumerGroupChecks = append(consumerGroupChecks, h.consumerLags(consumer))
	}
	if len(consumerGroups) == 0 {
		c := fthealth.TimedHealthCheck{
			HealthCheck: fthealth.HealthCheck{
				SystemCode:  systemCode,
				Name:        "Kafka consumer groups",
				Description: "Verifies all the defined consumer groups if they have lags.",
				Checks:      []fthealth.Check{h.noConsumerGroupsCheck()},
			},
			Timeout: 10 * time.Second,
		}
		return fthealth.Handler(c)
	}

	c := fthealth.TimedHealthCheck{
		HealthCheck: fthealth.HealthCheck{
			SystemCode:  systemCode,
			Name:        "Kafka consumer groups",
			Description: "Verifies all the defined consumer groups if they have lags.",
			Checks:      consumerGroupChecks,
		},
		Timeout: 10 * time.Second,
	}
	return fthealth.Handler(c)
}

func (h *healthcheck) GTG() gtg.Status {
	consumerGroups, err := h.fetchAndParseConsumerGroups()
	if err != nil {
		warnLogger.Println(err.Error())
		f := func() gtg.Status {
			return gtgCheck(func() (string, error) {
				_, err := h.fetchAndParseConsumerGroups()
				if err != nil {
					return "", err
				}
				return "", nil
			})
		}
		return gtg.FailFastParallelCheck([]gtg.StatusChecker{f})()
	}

	gtgs := []gtg.StatusChecker{}
	for _, consumer := range consumerGroups {
		consumerCopy := consumer
		gtgF := func() gtg.Status {
			return gtgCheck(
				func() (string, error) {
					return h.fetchAndCheckConsumerGroupForLags(consumerCopy)
				},
			)
		}
		gtgs = append(gtgs, gtgF)
	}

	return gtg.FailFastParallelCheck(gtgs)()
}

func gtgCheck(handler func() (string, error)) gtg.Status {
	if _, err := handler(); err != nil {
		return gtg.Status{GoodToGo: false, Message: err.Error()}
	}
	return gtg.Status{GoodToGo: true}
}

func (h *healthcheck) consumerLags(consumer string) fthealth.Check {
	return fthealth.Check{
		BusinessImpact:   "Will delay publishing on respective pipeline.",
		Name:             "Consumer group " + consumer + " is lagging.",
		PanicGuide:       "https://runbooks.in.ft.com/kafka-lagcheck",
		Severity:         1,
		TechnicalSummary: "Consumer group " + consumer + " is lagging. Further info at: __burrow/v2/kafka/local/consumer/" + consumer + "/status",
		Checker: func() (string, error) {
			return h.fetchAndCheckConsumerGroupForLags(consumer)
		},
	}
}

func (h *healthcheck) burrowUnavailableCheck(err error) fthealth.Check {
	return fthealth.Check{
		BusinessImpact:   "Will delay publishing on respective pipeline.",
		Name:             "Error retrieving consumer group list.",
		PanicGuide:       "https://runbooks.in.ft.com/kafka-lagcheck",
		Severity:         1,
		TechnicalSummary: fmt.Sprintf("Error retrieving consumer group list. Underlying kafka analysis tool burrow@*.service is unavailable. Please restart it or have a look if kafka itself is running properly. %s", err.Error()),
		Checker: func() (string, error) {
			return "", errors.New("Error retrieving consumer group list.")
		},
	}
}

func (h *healthcheck) noConsumerGroupsCheck() fthealth.Check {
	return fthealth.Check{
		BusinessImpact:   "Will delay publishing on respective pipeline.",
		Name:             "Error retrieving consumer group list.",
		PanicGuide:       "https://runbooks.in.ft.com/kafka-lagcheck",
		Severity:         1,
		TechnicalSummary: "Can't see any consumers yet so no lags to report and could successfully connect to kafka. This usually should happen only on startup, please retry in a few moments, and if this case persists, take a more serious look at burrow and kafka.",
		Checker: func() (string, error) {
			return "", nil
		},
	}
}

func (h *healthcheck) fetchAndCheckConsumerGroupForLags(consumerGroup string) (string, error) {
	resp, err := http.Get(h.checkPrefix + consumerGroup + "/status")
	if err != nil {
		warnLogger.Printf("Could not execute request to burrow: %v", err.Error())
		return "", err
	}
	defer properClose(resp)
	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Burrow returned status %d", resp.StatusCode)
		return "", errors.New(errMsg)
	}
	body, err := ioutil.ReadAll(resp.Body)
	lagErr := h.checkConsumerGroupForLags(body, consumerGroup)
	if lagErr != nil {
		warnLogger.Printf("Lagging consumers: [%s]", lagErr.Error())
	}
	return "", lagErr
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

	status, err := jq.String("status", "status")
	if err != nil {
		warnLogger.Printf("Couldn't unmarshall status>status: %v %v", string(body), err.Error())
		return errors.New("Couldn't unmarshall status > status")
	}

	totalLag, err := jq.Int("status", "totallag")
	if err != nil {
		warnLogger.Printf("Couldn't unmarshall totallag: %v %v", string(body), err.Error())
		return errors.New("Couldn't unmarshall totallag.")
	}

	if totalLag > h.maxLagTolerance {
		return h.ignoreWhitelistedTopics(jq, body, status, totalLag, consumerGroup)
	}

	if status != "OK" && totalLag > h.errLagTolerance { // this prevents old / unused consumer groups from causing lags
		return h.ignoreWhitelistedTopics(jq, body, status, totalLag, consumerGroup)
	}

	return nil
}

func (h *healthcheck) ignoreWhitelistedTopics(jq *jsonq.JsonQuery, body []byte, status string, lag int, consumerGroup string) error {
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
	return fmt.Errorf("%s consumer group is lagging behind with %d messages. Status of the consumer group is %s", consumerGroup, lag, status)
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
