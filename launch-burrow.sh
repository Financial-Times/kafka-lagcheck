#!/bin/sh
sed -i "s ZOOKEEPER_HOST $ZOOKEEPER_HOST " /config/burrow.cfg
sed -i "s ZOOKEEPER_PORT $ZOOKEEPER_PORT " /config/burrow.cfg
sed -i "s KAFKA_HOST $KAFKA_HOST " /config/burrow.cfg
sed -i "s KAFKA_PORT $KAFKA_PORT " /config/burrow.cfg

cat /config/burrow.cfg

exec ./kafka-lagcheck && ./burrow-app --config /config/burrow.cfg