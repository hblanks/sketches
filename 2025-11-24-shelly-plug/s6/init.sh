#!/bin/sh
# Run on target machine as part of `make deploy`
mkdir -p /tmp/log/shelly-listen
chown nobody /tmp/log/shelly-listen
rc-service -N s6 start
s6-svscanctl -a /run/service
s6-svc -u -wu /run/service/shelly-listen
