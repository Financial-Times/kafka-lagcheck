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

	"github.com/jmoiron/jsonq"

	fthealth "github.com/Financial-Times/go-fthealth/v1_1"
	"github.com/Financial-Times/service-status-go/gtg"
)

const healthPath = "/__health"

type HealthService struct {
	config       *HealthConfig
	healthChecks []fthealth.Check
	gtgChecks    []gtg.StatusChecker
}

type HealthConfig struct {
	appSystemCode     string
	appName           string
	appDescription    string
	burrowURL         string
	whitelistedTopics []string
	whitelistedEnvs   []string
	maxLagTolerance   int
	errLagTolerance   int
}

func newHealthService(appSystemCode string, appName string, appDescription string, burrowURL string,
	whitelistedTopics []string, whitelistedEnvs []string, maxLagTolerance int, errLagTolerance int) *HealthService {
	hc := &HealthService{
		config: &HealthConfig{
			appSystemCode:     appSystemCode,
			appName:           appName,
			appDescription:    appDescription,
			burrowURL:         burrowURL,
			whitelistedTopics: whitelistedTopics,
			whitelistedEnvs:   whitelistedEnvs,
			maxLagTolerance:   maxLagTolerance,
			errLagTolerance:   errLagTolerance,
		},
	}
	hc.healthChecks = []fthealth.Check{hc.burrowCheck(), hc.lagCheck()}
	burrowCheck := func() gtg.Status {
		return gtgCheck(hc.burrowChecker)
	}
	lagCheck := func() gtg.Status {
		return gtgCheck(hc.lagChecker)
	}
	var gtgChecks []gtg.StatusChecker
	gtgChecks = append(hc.gtgChecks, burrowCheck, lagCheck)
	hc.gtgChecks = gtgChecks
	return hc
}

func (service *HealthService) Health() fthealth.HC {
	return &fthealth.TimedHealthCheck{
		HealthCheck: fthealth.HealthCheck{
			SystemCode:  service.config.appSystemCode,
			Name:        service.config.appName,
			Description: service.config.appDescription,
			Checks:      service.healthChecks,
		},
		Timeout: 10 * time.Second,
	}
}

func (service *HealthService) burrowCheck() fthealth.Check {
	return fthealth.Check{
		BusinessImpact:   "Cannot monitor publishing pipeline lag.",
		Name:             "Burrow availability",
		PanicGuide:       "https://dewey.in.ft.com/view/system/kafka-lagcheck",
		Severity:         1,
		TechnicalSummary: "Error retrieving consumer group list. Underlying kafka analysis tool burrow@*.service is unavailable. Please restart it or have a look if kafka itself is running properly.",
		Checker:          service.burrowChecker,
	}
}

func (service *HealthService) burrowChecker() (string, error) {
	resp, err := http.Get(service.config.burrowURL)
	if err != nil {
		return "Could not execute request to burrow", err
	}
	defer properClose(resp)
	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Burrow returned status %d for call %s", resp.StatusCode, service.config.burrowURL)
		return errMsg, errors.New(errMsg)
	}
	return "Burrow is available", nil
}

func (service *HealthService) lagCheck() fthealth.Check {
	return fthealth.Check{
		BusinessImpact:   "Will delay publishing on respective pipeline.",
		Name:             "One of more consumer groups are lagging.",
		PanicGuide:       "https://dewey.in.ft.com/view/system/kafka-lagcheck",
		Severity:         1,
		TechnicalSummary: "One of more consumer grous are lagging. Further info at: __burrow/v2/kafka/local/consumer/{consumerName}/status",
		Checker:          service.lagChecker,
	}
}

func (service *HealthService) lagChecker() (string, error) {
	consumerGroups, err := service.fetchAndParseConsumerGroups()
	if err != nil {
		return "Could not execute request to burrow", err
	}

	lags := make(map[string]error)

	if len(consumerGroups) == 0 {
		errMsg := "Burrow didn't return any consumer groups"
		return errMsg, errors.New(errMsg)
	}

	for _, consumer := range consumerGroups {
		lags[consumer] = service.fetchAndCheckConsumerGroupForLags(consumer)
	}

	if len(lags) > 0 {
		errMsg := fmt.Sprintf("Lagging consumer groups: %v", lags)
		return errMsg, errors.New(errMsg)
	}
	return "", nil
}

func (service *HealthService) fetchAndCheckConsumerGroupForLags(consumerGroup string) error {
	resp, err := http.Get(service.config.burrowURL + consumerGroup + "/status")
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
	lagErr := service.checkConsumerGroupForLags(body, consumerGroup)
	if lagErr != nil {
		warnLogger.Printf("Lagging consumers: [%s]", lagErr.Error())
	}
	return lagErr
}

func (service *HealthService) checkConsumerGroupForLags(body []byte, consumerGroup string) error {
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

	if totalLag > service.config.maxLagTolerance {
		return service.ignoreWhitelistedTopics(jq, body, status, totalLag, consumerGroup)
	}

	if status != "OK" && totalLag > service.config.errLagTolerance { // this prevents old / unused consumer groups from causing lags
		return service.ignoreWhitelistedTopics(jq, body, status, totalLag, consumerGroup)
	}

	return nil
}

func (service *HealthService) ignoreWhitelistedTopics(jq *jsonq.JsonQuery, body []byte, status string, lag int, consumerGroup string) error {
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
	for _, whitelistedTopic := range service.config.whitelistedTopics {
		if topic == whitelistedTopic {
			return nil
		}
	}
	return fmt.Errorf("%s consumer group is lagging behind with %d messages. Status of the consumer group is %s", consumerGroup, lag, status)
}

func (service *HealthService) fetchAndParseConsumerGroups() ([]string, error) {
	resp, err := http.Get(service.config.burrowURL)
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
	return service.parseConsumerGroups(body)
}

func (service *HealthService) parseConsumerGroups(body []byte) ([]string, error) {
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
	return service.filterOutNonRelatedKafkaBridges(consumers), nil
}

func (service *HealthService) filterOutNonRelatedKafkaBridges(consumers []string) []string {
	filteredConsumers := []string{}
	for _, consumer := range consumers {
		if strings.Contains(consumer, "kafka-bridge") && !service.isBridgeFromWhitelistedEnvs(consumer) {
			continue
		}

		filteredConsumers = append(filteredConsumers, consumer)
	}

	return filteredConsumers
}

func (service *HealthService) isBridgeFromWhitelistedEnvs(bridgeName string) bool {
	//Do not filter out any Kafka bridge by default
	if len(service.config.whitelistedEnvs) == 0 {
		return true
	}

	for _, whitelistedEnv := range service.config.whitelistedEnvs {
		if strings.HasPrefix(bridgeName, whitelistedEnv) {
			return true
		}
	}

	return false
}

func gtgCheck(handler func() (string, error)) gtg.Status {
	if _, err := handler(); err != nil {
		return gtg.Status{GoodToGo: false, Message: err.Error()}
	}
	return gtg.Status{GoodToGo: true}
}

func (service *HealthService) GTG() gtg.Status {
	return gtg.FailFastParallelCheck(service.gtgChecks)()
}

func properClose(resp *http.Response) {
	io.Copy(ioutil.Discard, resp.Body)
	err := resp.Body.Close()
	if err != nil {
		warnLogger.Printf("Could not close response body: %v", err.Error())
	}
}
