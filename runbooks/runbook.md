# UPP - Kafka Lagcheck

The Kafka lagcheck service is responsible for tracking lags in Kafka consumer group message consumption.

## Code

kafka-lagcheck

## Primary URL

<https://upp-prod-delivery-glb.upp.ft.com/__kafka-lagcheck/>

## Service Tier

Platinum

## Lifecycle Stage

Production

## Delivered By

content

## Supported By

content

## Known About By

- mihail.mihaylov
- hristo.georgiev
- elitsa.pavlova
- kalin.arsov
- boyko.boykov

## Host Platform

AWS

## Architecture

The Kafka lagcheck service uses the Burrow service to track lags in Kafka consumer group message consumption.
If a consumer group is lagging this service will go unhealthy and point to the failing consumer group.

## Contains Personal Data

No

## Contains Sensitive Data

No

## Dependencies

- burrow

## Failover Architecture Type

ActiveActive

## Failover Process Type

FullyAutomated

## Failback Process Type

FullyAutomated

## Failover Details

The service is deployed in all Publishing and Delivery clusters.

The failover guide for the Delivery clusters is located here:
<https://github.com/Financial-Times/upp-docs/tree/master/failover-guides/delivery-cluster>

The failover guide for the Publishing clusters is located here:
<https://github.com/Financial-Times/upp-docs/tree/master/failover-guides/publishing-cluster>

## Data Recovery Process Type

NotApplicable

## Data Recovery Details

The service does not store data and it does not require any data recovery steps.

## Release Process Type

PartiallyAutomated

## Rollback Process Type

Manual

## Release Details

It is safe to release the service without failover.

## Key Management Process Type

Manual

## Key Management Details

To access the service clients need to provide basic auth credentials. To rotate credentials you need to login to a particular cluster and update varnish-auth secrets.

## Monitoring

Service in UPP K8S Publishing clusters:
- Publishing EU service health: https://upp-prod-publish-eu.upp.ft.com/__health/__pods-health?service-name=kafka-lagcheck
- Publishing US service health: https://upp-prod-publish-us.upp.ft.com/__health/__pods-health?service-name=kafka-lagcheck

Service in UPP K8S Delivery clusters:
- Delivery EU service health: https://upp-prod-delivery-eu.upp.ft.com/__health/__pods-health?service-name=kafka-lagcheck
- Delivery US service health: https://upp-prod-delivery-us.upp.ft.com/__health/__pods-health?service-name=kafka-lagcheck

## First Line Troubleshooting

https://github.com/Financial-Times/upp-docs/tree/master/guides/ops/first-line-troubleshooting

## Second Line Troubleshooting

- If the service becomes unhealthy and lag appears on one consumer group that might not be a problem. This may be the result of a lot of publishes in a particular moment and messages should be gradually consumed. Please wait approximately 10 minutes to see if the service goes healthy again.
- If the service is still unhealthy after 10 minutes, check Burrow for the exact lag value and verify that this value is non-zero or decreasing. Example burrow URL: <https://upp-prod-delivery-eu.upp.ft.com/__burrow/v2/kafka/local/consumer/<consumer-group>/status>.
- If Burrow indicates zero lag and Kafka lagcheck is unhealthy on a specific consumer group, then the Kafka lagcheck is stuck and you should restart it.
- If Burrow indicates that the lag doesn't decrease, check that the consumers using that consumer group are healthy (you may need to restart them). As a last resort you may need to restart Kafka, Zookeeper and Kafka REST proxy according to the guide. Failover before and republish after the restart..
