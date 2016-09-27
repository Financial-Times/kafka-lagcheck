package main

import (
	"github.com/golang/go/src/pkg/errors"
	"testing"
)

func TestBuildHealthURL(t *testing.T) {
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
			err: errors.New("xp-notifications-push-2 consumer group is lagging behind with 31 messages."),
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
			err: errors.New("xp-notifications-push-2 consumer group is lagging behind with 9 messages."),
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
	}
	h := NewHealthcheck(nil, "", []string{"Concept"}, 30)
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

		//if tc.err == nil && actualErr != nil {
		//	t.Errorf("Expected: [<nil>]\nActual: [%v]", actualErr)
		//} else if tc.err != nil && actualErr == nil {
		//	t.Errorf("Expected: [%v]\nActual: [<nil>]", tc.err)
		//} else if actualErr.Error() != tc.err.Error() {
		//	t.Errorf("Expected: [%v]\nActual: [%v]", tc.err, actualErr)
		//
		//}
	}
}
