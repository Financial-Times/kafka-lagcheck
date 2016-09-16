#!/bin/sh
sed -i "s ZOOKEEPER_HOST $ZOOKEEPER_HOST " /burrow.cfg
sed -i "s ZOOKEEPER_PORT $ZOOKEEPER_PORT " /burrow.cfg
sed -i "s KAFKA_HOST $KAFKA_HOST " /burrow.cfg
sed -i "s KAFKA_PORT $KAFKA_PORT " /burrow.cfg

exec ./burrow-app --config /config/burrow.cfg