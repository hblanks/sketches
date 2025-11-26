
GO_OPTS := CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 

BUILD := build

$(BUILD)/shelly-listen: main.go
	$(GO_OPTS) go build -o $@ $<

# Assumes s6 is in place on the target, e.g. via
# `apk add s6 s6-openrc; service s6 start`
.PHONY: deploy
deploy: $(BUILD)/shelly-listen
	ssh rpi3 mkdir -p /run/service
	rsync -av s6/ rpi3:/run/service/shelly-listen/
	ssh rpi3 s6-svc -d -wd /run/service/shelly-listen || true
	scp $< rpi3:/usr/local/bin/
	ssh rpi3 /run/service/shelly-listen/init.sh

.PHONY: harvest
harvest:
	rsync -r --include=current --include="*.bz2" --exclude="*" \
		rpi3:/tmp/log/shelly-listen/ $(BUILD)/data/
	find $(BUILD)/data -name "*.bz2" -exec bzcat "{}" \; | awk '{print $$3}' > $(BUILD)/data.json
	cat $(BUILD)/data/current | awk '{print $$3}' >> $(BUILD)/data.json
