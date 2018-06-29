package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"testing"

	status "github.com/Financial-Times/service-status-go/httphandlers"
	"github.com/stretchr/testify/assert"
	"gopkg.in/jarcoal/httpmock.v1"
)

func TestConsumerStatus(t *testing.T) {
	var testCases = []struct {
		body []byte
		err  error
	}{
		{
			body: []byte(`{}`),
			err:  errors.New("Couldn't unmarshall consumer status."),
		},
		{
			body: []byte(`{
				"error": true
			}`),
			err: errors.New("Consumer status response is an error."),
		},
		{
			// Lag is not 0 but below threshold.
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned",
				"status": {
					"cluster": "local",
					"group": "xp-notifications-push-2",
					"status": "OK",
					"complete": true,
					"partitions": [ ],
					"partition_count": 1,
					"maxlag": {
						"topic": "CmsPublicationEvents",
						"partition": 0,
						"status": "OK",
						"start": {
							"offset": 2779051,
							"timestamp": 1474992081559,
							"lag": 8
						},
						"end": {
							"offset": 2779316,
							"timestamp": 1474992621559,
							"lag": 19
						}
					},
					"totallag": 19
				}
			}
			`),
			err: nil,
		},
		{
			// Lag is however over our tolerance.
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned",
				"status": {
					"cluster": "local",
					"group": "xp-notifications-push-2",
					"status": "OK",
					"complete": true,
					"partitions": [ ],
					"partition_count": 1,
					"maxlag": {
						"topic": "CmsPublicationEvents",
						"partition": 0,
						"status": "OK",
						"start": {
							"offset": 2779051,
							"timestamp": 1474992081559,
							"lag": 8
						},
						"end": {
							"offset": 2779316,
							"timestamp": 1474992621559,
							"lag": 31
						}
					},
					"totallag": 31
				}
			}
			`),
			err: errors.New("xp-notifications-push-2 consumer group is lagging behind with 31 messages. Status of the consumer group is OK"),
		},
		{
			/*
				Lag is more than 5 and burrow is not returning an OK status.
				https://github.com/linkedin/Burrow/wiki/Consumer-Lag-Evaluation-Rules
			*/
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned",
				"status": {
					"cluster": "local",
					"group": "xp-notifications-push-2",
					"status": "WARNING",
					"complete": true,
					"partitions": [ ],
					"partition_count": 1,
					"maxlag": {
						"topic": "CmsPublicationEvents",
						"partition": 0,
						"status": "WARNING",
						"start": {
							"offset": 2779051,
							"timestamp": 1474992081559,
							"lag": 1
						},
						"end": {
							"offset": 2779316,
							"timestamp": 1474992621559,
							"lag": 9
						}
					},
					"totallag": 9
				}
			}
			`),
			err: errors.New("xp-notifications-push-2 consumer group is lagging behind with 9 messages. Status of the consumer group is WARNING"),
		},
		{
			/*
				Lag is less than 5 and burrow is not returning an OK status.
				https://github.com/linkedin/Burrow/wiki/Consumer-Lag-Evaluation-Rules
			*/
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned",
				"status": {
					"cluster": "local",
					"group": "xp-notifications-push-2",
					"status": "WARNING",
					"complete": true,
					"partitions": [ ],
					"partition_count": 1,
					"maxlag": {
						"topic": "CmsPublicationEvents",
						"partition": 0,
						"status": "WARNING",
						"start": {
							"offset": 2779051,
							"timestamp": 1474992081559,
							"lag": 1
						},
						"end": {
							"offset": 2779316,
							"timestamp": 1474992621559,
							"lag": 9
						}
					},
					"totallag": 4
				}
			}
			`),
			err: nil,
		},
		{
			// No problems at all
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned",
				"status": {
					"cluster": "local",
					"group": "xp-notifications-push-2",
					"status": "OK",
					"complete": true,
					"partitions": [],
					"partition_count": 1,
					"maxlag": null,
					"totallag": 0
				}
			}
			`),
			err: nil,
		},
		{
			/*
				Lag is over our tolerance.
				Topic is in our white-list.
			*/
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned",
				"status": {
					"cluster": "local",
					"group": "xp-notifications-push-2",
					"status": "OK",
					"complete": true,
					"partitions": [ ],
					"partition_count": 1,
					"maxlag": {
						"topic": "Concept",
						"partition": 0,
						"status": "OK",
						"start": {
							"offset": 2779051,
							"timestamp": 1474992081559,
							"lag": 8
						},
						"end": {
							"offset": 2779316,
							"timestamp": 1474992621559,
							"lag": 31
						}
					},
					"totallag": 31
				}
			}
			`),
			err: nil,
		},
		{
			// Consumer is stopped, burrow is not showing an OK status.
			// Lag is however zero, means all messages are consumed, and group not used for a long time.
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned",
				"status": {
					"cluster": "local",
					"group": "xp-notifications-push-2",
					"status": "ERR",
					"complete": true,
					"partitions": [
						{
							"topic": "NativeCmsMetadataPublicationEvents",
							"partition": 0,
							"status": "STOP",
							"start": {
								"offset": 1854,
								"timestamp": 1475255783092,
								"lag": 0
							},
							"end": {
								"offset": 1860,
								"timestamp": 1475256143092,
								"lag": 0
							}
						}
					],
					"partition_count": 1,
					"maxlag": null,
					"totallag": 0
				}
			}
			`),
			err: nil,
		},
	}
	initLogs(ioutil.Discard, ioutil.Discard, ioutil.Discard)
	h := newHealthcheck("", []string{"Concept"}, []string{}, 30, 5)
	for _, tc := range testCases {
		actualErr := h.checkConsumerGroupForLags(tc.body, "xp-notifications-push-2")
		actualMsg := "<nil>"
		if actualErr != nil {
			actualMsg = actualErr.Error()
		}
		expectedMsg := "<nil>"
		if tc.err != nil {
			expectedMsg = tc.err.Error()
		}
		if expectedMsg != actualMsg {
			t.Errorf("Expected: [%s]\nActual: [%s]", expectedMsg, actualMsg)
		}
	}
}

func TestUnmarshalBurrowResponseFails(t *testing.T) {
	testCases := []struct {
		body  []byte
		err   string
		fails bool
	}{
		{
			body:  []byte(``),
			err:   "Could not decode response body to json.",
			fails: true,
		},
		{
			body:  []byte(`{"error": null}`),
			err:   "Couldn't unmarshall consumer status.",
			fails: true,
		},
		{
			body:  []byte(`{"error":false, "status": {}}`),
			err:   "Couldn't unmarshall status > status",
			fails: true,
		},
		{
			body:  []byte(`{"error":false, "status": {"status":1,"totallag": "hi"}}`),
			err:   "Couldn't unmarshall status > status",
			fails: true,
		},
		{
			body:  []byte(`{"error":false, "status": {"status":"any string","totallag": "hi"}}`),
			err:   "Couldn't unmarshall totallag.",
			fails: true,
		},
	}

	h := newHealthcheck("", []string{"Concept"}, []string{}, 30, 10)
	for _, tc := range testCases {
		err := h.checkConsumerGroupForLags(tc.body, "xp-notifications-push-2")
		if tc.fails {
			assert.EqualError(t, err, tc.err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestConsumerList(t *testing.T) {
	var testCases = []struct {
		body      []byte
		err       error
		consumers []string
	}{
		{
			body:      []byte("{}"),
			err:       errors.New("Couldn't unmarshall consumer list response"),
			consumers: nil,
		},
		{
			body: []byte(`{
				"error": true
			}`),
			err:       errors.New("Consumer list response is an error"),
			consumers: nil,
		},
		{
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned"
			}`),
			err:       errors.New("Couldn't unmarshall consumer list"),
			consumers: nil,
		},
		{
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned",
				"consumers": [
					"xp-notifications-push-2",
					"xp-v2-annotator-red",
					"xp-v2-annotator-blue",
					"console-consumer-2324",
					"console-consumer-98135"
				]
			}
			`),
			err:       nil,
			consumers: []string{"xp-notifications-push-2", "xp-v2-annotator-red", "xp-v2-annotator-blue", "console-consumer-2324", "console-consumer-98135"},
		},
		{
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned",
				"consumers": [
					"console-consumer-2324",
					"lower-env1-kafka-bridge-2324",
					"lower-env2-kafka-bridge-2324",
					"console-consumer-98135"
				]
			}
			`),
			err:       nil,
			consumers: []string{"console-consumer-2324", "lower-env1-kafka-bridge-2324", "console-consumer-98135"},
		},
		{
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned",
				"consumers": []
			}
			`),
			err:       nil,
			consumers: []string{},
		},
	}
	initLogs(ioutil.Discard, ioutil.Discard, ioutil.Discard)
	h := newHealthcheck("", []string{"Concept"}, []string{"lower-env1"}, 30, 10)
	for _, tc := range testCases {
		consumers, actualErr := h.parseConsumerGroups(tc.body)
		actualMsg := "<nil>"
		if actualErr != nil {
			actualMsg = actualErr.Error()
		}
		expectedMsg := "<nil>"
		if tc.err != nil {
			expectedMsg = tc.err.Error()
		}
		if !strings.HasPrefix(actualMsg, expectedMsg) {
			t.Errorf("Expected to start with: [%s]\nActual: [%s]", expectedMsg, actualMsg)
		}
		for i, c := range consumers {
			if c != tc.consumers[i] {
				t.Errorf("Consumers do not match. Expected: [%s]\nActual: [%s]", tc.consumers, consumers)
			}
		}
	}
}

func TestFilterOutNonRelatedKafkaBridges(t *testing.T) {
	var testCases = []struct {
		whitelistedEnvs []string
		consumers       []string
		expected        []string
	}{
		{
			whitelistedEnvs: []string{},
			consumers:       []string{"console-consumer", "prod-env-kafka-bridge", "lower-env-kafka-bridge"},
			expected:        []string{"console-consumer", "prod-env-kafka-bridge", "lower-env-kafka-bridge"},
		},
		{
			whitelistedEnvs: []string{"prod-env"},
			consumers:       []string{"console-consumer", "prod-env-kafka-bridge", "lower-env-kafka-bridge"},
			expected:        []string{"console-consumer", "prod-env-kafka-bridge"},
		},
		{
			whitelistedEnvs: []string{"prod-env", "pub-prod-env"},
			consumers:       []string{"console-consumer", "prod-env-kafka-bridge", "lower-env-kafka-bridge", "pre-prod-env-kafka-bridge", "pub-prod-env-kafka-bridge"},
			expected:        []string{"console-consumer", "prod-env-kafka-bridge", "pub-prod-env-kafka-bridge"},
		},
	}

	for _, tc := range testCases {
		h := newHealthcheck("", []string{""}, tc.whitelistedEnvs, 30, 10)
		filteredConsumers := h.filterOutNonRelatedKafkaBridges(tc.consumers)
		for i, c := range filteredConsumers {
			if c != tc.expected[i] {
				t.Errorf("Consumers do not match. Expected: [%s]\nActual: [%s]", tc.expected, filteredConsumers)
			}
		}
	}
}

func TestGTG(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	burrowUrl := "http://burrow.example.com"

	consumers := map[string]interface{}{
		"error":     false,
		"message":   "consumer list returned",
		"consumers": []string{"consumer1", "consumer2"},
	}
	consumersResponse, _ := httpmock.NewJsonResponder(200, consumers)
	httpmock.RegisterResponder("GET", burrowUrl+"/v3/kafka/local/consumer/", consumersResponse)

	consumerStatus := []map[string]interface{}{
		map[string]interface{}{
			"error":   false,
			"message": "consumer group status returned",
			"status": map[string]interface{}{
				"status":   "OK",
				"complete": true,
				"partitions": []map[string]interface{}{
					map[string]interface{}{"topic": "TestTopic"},
				},
				"partition_count": 1,
				"maxlag":          nil,
				"totallag":        0,
			},
		},
		map[string]interface{}{
			"error":   false,
			"message": "consumer group status returned",
			"status": map[string]interface{}{
				"status":   "OK",
				"complete": true,
				"partitions": []map[string]interface{}{
					map[string]interface{}{"topic": "TestTopic"},
				},
				"partition_count": 1,
				"maxlag":          nil,
				"totallag":        0,
			},
		},
	}
	for i, status := range consumerStatus {
		statusResponse, _ := httpmock.NewJsonResponder(200, status)
		httpmock.RegisterResponder("GET", fmt.Sprintf("%s/v3/kafka/local/consumer/consumer%d/status", burrowUrl, i+1), statusResponse)
	}

	h := newHealthcheck(burrowUrl, []string{}, []string{}, 0, 0)

	req, _ := http.NewRequest("GET", "http://localhost/__gtg", nil)
	w := httptest.NewRecorder()
	http.HandlerFunc(status.NewGoodToGoHandler(h.GTG))(w, req)

	actual := *w.Result()
	assert.Equal(t, actual.StatusCode, http.StatusOK, "GTG HTTP status")
}

type syncWriter struct {
	sync.Mutex
	buf bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (n int, err error) {
	w.Lock()
	defer w.Unlock()

	return w.buf.Write(p)
}

func (w *syncWriter) Bytes() []byte {
	w.Lock()
	defer w.Unlock()

	return w.buf.Bytes()
}

func TestGTGLaggingBeyondLimit(t *testing.T) {
	buf := &syncWriter{}
	initLogs(buf, buf, buf)

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	burrowUrl := "http://burrow.example.com"

	consumers := map[string]interface{}{
		"error":     false,
		"message":   "consumer list returned",
		"consumers": []string{"consumer1", "consumer2"},
	}
	consumersResponse, _ := httpmock.NewJsonResponder(200, consumers)
	httpmock.RegisterResponder("GET", burrowUrl+"/v3/kafka/local/consumer/", consumersResponse)

	consumerStatus := []map[string]interface{}{
		map[string]interface{}{
			"error":   false,
			"message": "consumer group status returned",
			"status": map[string]interface{}{
				"status":   "OK",
				"complete": true,
				"partitions": []map[string]interface{}{
					map[string]interface{}{"topic": "TestTopic"},
				},
				"partition_count": 1,
				"maxlag":          nil,
				"totallag":        10,
			},
		},
		map[string]interface{}{
			"error":   false,
			"message": "consumer group status returned",
			"status": map[string]interface{}{
				"status":   "OK",
				"complete": true,
				"partitions": []map[string]interface{}{
					map[string]interface{}{"topic": "TestTopic"},
				},
				"partition_count": 1,
				"maxlag":          nil,
				"totallag":        6,
			},
		},
	}
	for i, status := range consumerStatus {
		statusResponse, _ := httpmock.NewJsonResponder(200, status)
		httpmock.RegisterResponder("GET", fmt.Sprintf("%s/v3/kafka/local/consumer/consumer%d/status", burrowUrl, i+1), statusResponse)
	}

	h := newHealthcheck(burrowUrl, []string{}, []string{}, 5, 1)

	req, _ := http.NewRequest("GET", "http://localhost/__gtg", nil)
	w := httptest.NewRecorder()
	http.HandlerFunc(status.NewGoodToGoHandler(h.GTG))(w, req)
	actual := *w.Result()
	assert.Equal(t, actual.StatusCode, http.StatusServiceUnavailable, "GTG HTTP status")

	// give a little time for logging goroutine to catch up
	time.Sleep(100 * time.Millisecond)

	logs := string(buf.Bytes())
	for _, status := range consumerStatus {
		expected := fmt.Sprintf(".*Lagging consumers:.+consumer[\\d] consumer group is lagging behind with %d messages.*", status["status"].(map[string]interface{})["totallag"])
		assert.Regexp(t, expected, logs, "lagging consumer log")
	}
}

func TestGTGLaggingWithinLimit(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	burrowUrl := "http://burrow.example.com"

	consumers := map[string]interface{}{
		"error":     false,
		"message":   "consumer list returned",
		"consumers": []string{"consumer1", "consumer2"},
	}
	consumersResponse, _ := httpmock.NewJsonResponder(200, consumers)
	httpmock.RegisterResponder("GET", burrowUrl+"/v3/kafka/local/consumer/", consumersResponse)

	consumerStatus := []map[string]interface{}{
		map[string]interface{}{
			"error":   false,
			"message": "consumer group status returned",
			"status": map[string]interface{}{
				"status":   "OK",
				"complete": true,
				"partitions": []map[string]interface{}{
					map[string]interface{}{"topic": "TestTopic"},
				},
				"partition_count": 1,
				"maxlag":          nil,
				"totallag":        10,
			},
		},
		map[string]interface{}{
			"error":   false,
			"message": "consumer group status returned",
			"status": map[string]interface{}{
				"status":   "OK",
				"complete": true,
				"partitions": []map[string]interface{}{
					map[string]interface{}{"topic": "TestTopic"},
				},
				"partition_count": 1,
				"maxlag":          nil,
				"totallag":        0,
			},
		},
	}
	for i, status := range consumerStatus {
		statusResponse, _ := httpmock.NewJsonResponder(200, status)
		httpmock.RegisterResponder("GET", fmt.Sprintf("%s/v3/kafka/local/consumer/consumer%d/status", burrowUrl, i+1), statusResponse)
	}

	h := newHealthcheck(burrowUrl, []string{}, []string{}, 10, 5)

	req, _ := http.NewRequest("GET", "http://localhost/__gtg", nil)
	w := httptest.NewRecorder()
	http.HandlerFunc(status.NewGoodToGoHandler(h.GTG))(w, req)

	actual := *w.Result()
	assert.Equal(t, actual.StatusCode, http.StatusOK, "GTG HTTP status")
}
