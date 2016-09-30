package main

import (
	"github.com/golang/go/src/pkg/errors"
	"strings"
	"testing"
	"io/ioutil"
)

func TestConsumerStatus(t *testing.T) {
	var testCases = []struct {
		body []byte
		err  error
	}{
		{
			body: []byte(`{}`),
			err:  errors.New("Couldn't unmarshall consumer status: {} Object map[] does not contain field error\n"),
		},
		{
			body: []byte(`{
				"error": true
			}`),
			err: errors.New(`Consumer status response is an error: {
				"error": true
			}`),
		},
		{
			/*
				Lag is not 0 but status is OK, meaning lag was 0 at least once over the observation window.
				Lag is not extremely high either.
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
			/*
				Lag is not 0 but status is OK, meaning lag was 0 at least once over the observation window.
				Lag is however over our tolerance.
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
			err: errors.New("xp-notifications-push-2 consumer group is lagging behind with 31 messages"),
		},
		{
			/*
				Lag is not 0 but that is not the problem.
				If offsets are committed, but lag keeps increasing over the observation window, burrow will warn us. That's a problem.
				Statuses can be STALLING or ERROR as well, neither of them are not ok.
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
			err: errors.New("xp-notifications-push-2 consumer group is lagging behind with 9 messages"),
		},
		{
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
				Lag is not 0 but status is OK, meaning lag was 0 at least once over the observation window.
				Lag is our tolerance.
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
			err: errors.New("xp-notifications-push-2 consumer group is lagging behind with 0 messages"),
		},
		{
			body: []byte(`{
				"error": false,
				"message": "consumer group status returned",
				"status": {
					"cluster": "local",
					"group": "xp-notifications-push-2",
					"status": "ERR",
					"complete": true,
					"partitions": null,
					"partition_count": 1,
					"maxlag": null,
					"totallag": 0
				}
			}
			`),
			err: errors.New("Couldn't unmarshall topic for consumer=xp-notifications-push-2"),
		},
	}
	initLogs(ioutil.Discard, ioutil.Discard, ioutil.Discard)
	h := newHealthcheck(nil, "", []string{"Concept"}, 30)
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
				"consumers": []
			}
			`),
			err:       nil,
			consumers: []string{},
		},
	}
	initLogs(ioutil.Discard, ioutil.Discard, ioutil.Discard)
	h := newHealthcheck(nil, "", []string{"Concept"}, 30)
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
